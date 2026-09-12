package main

// A/B 全链路对比（记录见 docs/test/session-transition-ab.md）：同一脚本
// 用真实链路（seelebridge Runtime + adapters.EnginePort + sessionstore
// 真数据 + 假 provider）跑两遍（变体 A/B），断言：
//   - 每次运行内热挂载"运行中"会话快速返回（无死锁）；
//   - A 的对话不掺入 B 内容、B 正常完成；
//   - 两次运行的行为一致（活跃会话/会话列表/对话文本/事件指纹）。
// 脚本：A 首轮完成 → fork 出 B 并切换 → 切回 A → A 第二轮阻塞运行中 →
// 切到 B → B 提交并完成 → 切回运行中的 A → 释放 A → 全部收敛。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
)

type chainABResult struct {
	activeID     string
	sessions     []string
	conversation []string
	fingerprint  []string
	hotAttachMS  int64
}

func runChainSwitchScenario(t *testing.T) chainABResult {
	t.Helper()
	provider := newRequestCountingProvider()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider.serve(t, w, r)
	}))
	t.Cleanup(server.Close)

	tempDir := t.TempDir()
	accountsPath := filepath.Join(tempDir, "accounts.yaml")
	accounts := "roles:\n  agent:\n    - model: test-model\n      base_url: " + server.URL +
		"\n      api_key: test-key-1\n    - model: test-model\n      base_url: " + server.URL +
		"\n      api_key: test-key-2\n"
	if err := os.WriteFile(accountsPath, []byte(accounts), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := newFullChainHarnessWithProjectBinding(t, accountsPath, tempDir, 30*time.Second, true)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	sub := harness.events.Subscribe(1024)
	defer sub.Close()

	// A 首轮完成。
	if err := harness.app.Submit(ctx, "first A"); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	sessionA := harness.app.Snapshot().Session.ID
	if sessionA == "" {
		t.Fatal("session A not materialized")
	}
	// fork 出 B 并切换。
	sessionB, err := harness.app.ForkSessionLatest(sessionA)
	if err != nil {
		t.Fatalf("fork B: %v", err)
	}
	if harness.app.Snapshot().Session.ID != sessionB {
		t.Fatalf("active after fork = %q, want B", harness.app.Snapshot().Session.ID)
	}
	// 切回 A，发起长任务（provider 第二请求阻塞 → A 运行中）。
	if err := harness.app.ResumeSession(sessionA); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.Submit(ctx, "long task A"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-provider.secondSeen:
	case <-ctx.Done():
		t.Fatalf("A second turn did not block: %v", ctx.Err())
	}
	// 切到 B（空闲）→ B 提交并完成（第三请求不阻塞）。
	if err := harness.app.ResumeSession(sessionB); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.Submit(ctx, "hello B"); err != nil {
		t.Fatal(err)
	}
	bDone := time.Now().Add(20 * time.Second)
	for {
		snapshot := harness.app.Snapshot()
		if snapshot.Session.ID == sessionB && !snapshot.Chat.Running {
			break
		}
		if time.Now().After(bDone) {
			t.Fatalf("B did not complete while A running: %+v", snapshot.Chat)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// 关键：切回运行中的 A——热挂载必须快速返回（无死锁）。
	hotStart := time.Now()
	if err := harness.app.ResumeSession(sessionA); err != nil {
		t.Fatalf("resume running A: %v", err)
	}
	hotAttachMS := time.Since(hotStart).Milliseconds()
	if harness.app.Snapshot().Session.ID != sessionA {
		t.Fatalf("active after hot attach = %q, want A", harness.app.Snapshot().Session.ID)
	}
	// 释放 A → 收敛。
	close(provider.releaseSecond)
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}

	// 收集结果。
	snapshot := harness.app.Snapshot()
	fingerprint, ordinals := drainChainFingerprint(t, sub)
	result := chainABResult{
		activeID:     snapshot.Session.ID,
		sessions:     sessionIDs(snapshot.Sessions),
		conversation: conversationTexts(snapshot.Conversation),
		fingerprint:  fingerprint,
		hotAttachMS:  hotAttachMS,
	}
	// 会话 ID 是运行期生成值，跨运行必然不同：统一归一化为事件指纹序数后对比。
	result.normalizeSessionIDs(ordinals)
	// A 对话含自身输入与 DONE_FROM_A，不含 B 输入（无串写）。
	joined := strings.Join(result.conversation, "\n")
	if !strings.Contains(joined, "long task A") || !strings.Contains(joined, "DONE_FROM_A") {
		t.Fatalf("A did not complete normally: %q", joined)
	}
	if strings.Contains(joined, "hello B") {
		t.Fatalf("A conversation polluted with B input: %q", joined)
	}
	return result
}

// normalizeSessionIDs 用事件指纹的序数映射归一化 activeID/sessions，消除
// 运行间 ID 噪音。序数按脚本事件首次出现顺序分配（跨运行稳定），不按会话
// 列表的展示顺序：列表按 UpdatedAt 倒序、相等时由随机 ID 兜底，跨运行本来
// 就不保证顺序一致，断言只关心集合与活跃会话身份。
func (result *chainABResult) normalizeSessionIDs(ordinal map[string]string) {
	for index := range result.sessions {
		if mapped, ok := ordinal[result.sessions[index]]; ok {
			result.sessions[index] = mapped
		}
	}
	sort.Strings(result.sessions)
	if mapped, ok := ordinal[result.activeID]; ok {
		result.activeID = mapped
	}
}

// TestChainSwitchABStable 是 A/B 全链路对比：两遍同脚本行为一致且无死锁。
func TestChainSwitchABStable(t *testing.T) {
	variantA := runChainSwitchScenario(t)
	variantB := runChainSwitchScenario(t)

	for name, variant := range map[string]chainABResult{"A": variantA, "B": variantB} {
		if variant.hotAttachMS > 2000 {
			t.Fatalf("variant %s hot attach to running session took %dms（死锁/阻塞）",
				name, variant.hotAttachMS)
		}
	}
	// 计时字段（hotAttachMS）是运行时噪音，不参与一致性对比；其余字段必须
	// 逐项一致。
	behaviorA := variantA
	behaviorB := variantB
	behaviorA.hotAttachMS = 0
	behaviorB.hotAttachMS = 0
	if !reflect.DeepEqual(behaviorA, behaviorB) {
		t.Fatalf("A/B 全链路行为不一致:\nA=%+v\nB=%+v", variantA, variantB)
	}
}

func sessionIDs(sessions []application.SessionInfo) []string {
	ids := make([]string, 0, len(sessions))
	for _, session := range sessions {
		ids = append(ids, session.ID)
	}
	return ids
}

// drainChainFingerprint 把订阅事件归一化为指纹（kind + session/request/
// message ID 序数），与 core 侧事件指纹同一方法。
func drainChainFingerprint(t *testing.T, sub application.Subscription) ([]string, map[string]string) {
	t.Helper()
	ids := map[string]string{}
	next := 0
	ordinal := func(value string) string {
		if value == "" {
			return "-"
		}
		if _, ok := ids[value]; !ok {
			ids[value] = string(rune('0' + next))
			next++
		}
		return ids[value]
	}
	var fingerprint []string
	quiet := time.After(100 * time.Millisecond)
	draining := true
	for draining {
		select {
		case event, ok := <-sub.Events:
			if !ok {
				draining = false
				break
			}
			if event.RequestID == "" {
				continue
			}
			key := string(event.Kind) + "|" + ordinal(event.SessionID) + "|" + ordinal(event.RequestID)
			if event.Kind == application.EventMessageAdded || event.Kind == application.EventMessageDelta {
				key += "|" + ordinal(chainMessageID(event.Payload))
			}
			fingerprint = append(fingerprint, key)
		case <-quiet:
			draining = false
		}
	}
	return fingerprint, ids
}

func chainMessageID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var probe struct {
		MessageID string `json:"message_id"`
		ID        string `json:"id"`
	}
	if json.Unmarshal(raw, &probe) != nil {
		return ""
	}
	if probe.MessageID != "" {
		return probe.MessageID
	}
	return probe.ID
}
