//go:build draftsmoke

// 草稿（composer draft）的 headless 冒烟：真实全链路（真装配 + 真 store + 真 wire
// 组装）+ 记录型 mock provider（逐条留下 provider 请求原文），把用户口径里的三件事
// 钉成可重复的断言：
//
//  1. 前缀匹配：未发送草稿不进 provider 请求（草稿槽是本地事实源，不构成历史），
//     且一轮一轮发出去之后相邻请求仍保持"前一次请求是后一次请求的完整前缀"
//     （提示缓存/前缀缓存成立的前提）；
//  2. 历史的留存：重启（同一 store 再启动）后 ResumeSession 仍能看到既有可见
//     消息对，逐条相同；
//  3. 中断会话恢复：一轮在途被取消（用户点停止）后，已定稿的历史不被吞；重启
//     恢复后仍可继续提交并拿到回答。
//
// 只换 provider（本地记录型 mock），不换任何生产路径：装配、会话单元、草稿槽、
// rollout 落盘、wire 组装都是真的。真实 API 版本（付费、opt-in）见
// real_api_prefix_live_test.go / real_api_smoke_test.go。
//
// 运行：
//
//	go test -tags draftsmoke . -run TestComposerDraftHeadlessSmoke -count=1 -v -timeout 10m
//
// 需要 import 的 store 目录、账号文件都是本文件自己造的临时数据，不读也不打印
// 任何真实账号内容。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/model"
)

// draftSmokeSentinel 是只写在输入框（草稿槽）里的哨兵正文：它绝不该出现在
// provider 请求里，物化之后也不该留在草稿槽/目录身份上。
const draftSmokeSentinel = "草稿哨兵：这段字只活在输入框里，不该被发给 provider"

// draftSmokeProvider 是记录型 mock provider：留下每次请求的原始 body（喂
// assertFullChainPrefixInvariant），并且可以被"扣住"不回复——中断用例需要在
// 请求在途时按 CancelChat，靠扣住制造可控的中断窗口。
type draftSmokeProvider struct {
	*httptest.Server
	t        *testing.T
	mu       sync.Mutex
	bodies   [][]byte
	requests int
	hold     bool
	arrived  chan struct{}
	release  chan struct{}
}

func newDraftSmokeProvider(t *testing.T) *draftSmokeProvider {
	t.Helper()
	provider := &draftSmokeProvider{
		t:       t,
		arrived: make(chan struct{}, 32),
		release: make(chan struct{}, 32),
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		provider.serve(writer, request)
	}))
	t.Cleanup(server.Close)
	provider.Server = server
	return provider
}

func (provider *draftSmokeProvider) bodiesSnapshot() [][]byte {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	out := make([][]byte, len(provider.bodies))
	for index := range provider.bodies {
		out[index] = append([]byte(nil), provider.bodies[index]...)
	}
	return out
}

// setHold 切换"扣住 provider 不回复"：打开之后到达的请求会一直挂着，直到
// releaseOne 放行或客户端自己取消（= 被 CancelChat 打断）。打开时清掉上一轮
// 留下的到达信号，避免把"旧请求已到达"当成新窗口已打开。
func (provider *draftSmokeProvider) setHold(hold bool) {
	provider.mu.Lock()
	provider.hold = hold
	provider.mu.Unlock()
	if hold {
		provider.drainArrived()
	}
}

func (provider *draftSmokeProvider) drainArrived() {
	for {
		select {
		case <-provider.arrived:
		default:
			return
		}
	}
}

func (provider *draftSmokeProvider) releaseOne() {
	select {
	case provider.release <- struct{}{}:
	default:
	}
}

// waitForRequest 等到"下一个请求已到达 provider"（中断窗口已经打开）。
func (provider *draftSmokeProvider) waitForRequest(ctx context.Context) bool {
	select {
	case <-provider.arrived:
		return true
	case <-ctx.Done():
		return false
	}
}

func (provider *draftSmokeProvider) serve(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(writer, "read request: "+err.Error(), http.StatusBadRequest)
		return
	}
	var envelope struct {
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		http.Error(writer, "decode request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !envelope.Stream {
		http.Error(writer, "expected streaming request", http.StatusBadRequest)
		return
	}

	provider.mu.Lock()
	provider.requests++
	index := provider.requests
	provider.bodies = append(provider.bodies, append([]byte(nil), body...))
	hold := provider.hold
	provider.mu.Unlock()

	select {
	case provider.arrived <- struct{}{}:
	default:
	}
	if hold {
		select {
		case <-provider.release:
		case <-request.Context().Done():
			// 客户端（本轮）被取消：这一轮按"中断"处理，不回复。
			return
		}
	}

	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	writeSSE(provider.t, writer, flusher, map[string]any{
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         map[string]any{"content": fmt.Sprintf("answer-%d", index)},
			"finish_reason": "",
		}},
	})
	writeSSE(provider.t, writer, flusher, map[string]any{
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         map[string]any{},
			"finish_reason": "stop",
		}},
	})
	fmt.Fprint(writer, "data: [DONE]\n\n")
	flusher.Flush()
}

// draftSmokeAccounts 写一份指向本地 mock provider 的账号文件（不碰真实账号）。
func draftSmokeAccounts(t *testing.T, baseURL, root string) string {
	t.Helper()
	path := filepath.Join(root, "accounts.yaml")
	content := fmt.Sprintf("roles:\n  agent:\n    - model: test-model\n      base_url: %s\n      api_key: test-key\n", baseURL)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// draftSmokeWaitIdle 等一轮回到 idle 并把回合级失败当失败（错误信息带上对话尾巴，
// 便于红灯归因）。
func draftSmokeWaitIdle(t *testing.T, ctx context.Context, app *application.Service) {
	t.Helper()
	turnCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if err := app.WaitForIdle(turnCtx); err != nil {
		snapshot := app.Snapshot()
		t.Fatalf("回合未回到 idle（%v）：running=%v error=%q 对话=%s",
			err, snapshot.Chat.Running, snapshot.Chat.Error, draftSmokeDescribe(snapshot.Conversation, 4))
	}
	if snapshot := app.Snapshot(); snapshot.Chat.Error != "" {
		t.Fatalf("回合失败：%s（对话=%s）", snapshot.Chat.Error, draftSmokeDescribe(snapshot.Conversation, 4))
	}
}

// draftSmokeWaitIdleTolerant 是取消之后的等待：中断这一轮会把取消原因记成
// 可见的回合错误（"context canceled"），这里只等回 idle 并如实记下，不当失败。
func draftSmokeWaitIdleTolerant(t *testing.T, ctx context.Context, app *application.Service) model.Snapshot {
	t.Helper()
	turnCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if err := app.WaitForIdle(turnCtx); err != nil {
		snapshot := app.Snapshot()
		t.Fatalf("取消之后回合未回到 idle（%v）：running=%v error=%q 对话=%s",
			err, snapshot.Chat.Running, snapshot.Chat.Error, draftSmokeDescribe(snapshot.Conversation, 4))
	}
	return app.Snapshot()
}

func draftSmokeDescribe(conversation []application.Message, limit int) string {
	tail := conversation
	if len(tail) > limit {
		tail = tail[len(tail)-limit:]
	}
	parts := make([]string, 0, len(tail))
	for _, message := range tail {
		content := strings.TrimSpace(message.Content)
		if len(content) > 60 {
			content = content[:60] + "…"
		}
		parts = append(parts, message.Role+":"+content)
	}
	return "[" + strings.Join(parts, " | ") + "]"
}

// draftSmokeDirectoryRow 在会话目录（前端会话树的数据源）里找一行。
func draftSmokeDirectoryRow(snapshot application.Snapshot, sessionID string) (model.SessionInfo, bool) {
	for _, row := range snapshot.Sessions {
		if row.ID == sessionID {
			return row, true
		}
	}
	return model.SessionInfo{}, false
}

// draftSmokeSettled 取"有正文的 user/assistant"序列：中断那一轮可能留下一条空的
// assistant 占位（一个字都没产出），它不该参与"历史留存"的逐条比对——要保住的是
// 已定稿内容与已发出的 user 正文。
func draftSmokeSettled(messages []probeWireMessage) []probeWireMessage {
	out := make([]probeWireMessage, 0, len(messages))
	for _, message := range messages {
		if strings.TrimSpace(message.Content) == "" {
			continue
		}
		out = append(out, message)
	}
	return out
}

// draftSmokeHasRoleText 判断可见消息里有没有某个角色的某段正文。
func draftSmokeHasRoleText(messages []probeWireMessage, role, want string) bool {
	for _, message := range messages {
		if message.Role == role && strings.Contains(message.Content, want) {
			return true
		}
	}
	return false
}

func TestComposerDraftHeadlessSmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	t.Run("前缀匹配：未发送草稿不进 provider 请求，跨轮前缀不变量成立", func(t *testing.T) {
		provider := newDraftSmokeProvider(t)
		root := t.TempDir()
		harness := newFullChainHarness(t, draftSmokeAccounts(t, provider.URL, root), root, 10*time.Second)
		defer harness.app.Shutdown()

		// 真实前端路径：新建会话草稿 → 输入（300ms 防抖落盘的那一步）→ 草稿落盘。
		if err := harness.app.BeginNewSession(); err != nil {
			t.Fatalf("BeginNewSession: %v", err)
		}
		draft := harness.app.Snapshot()
		draftID := draft.Session.ID
		if !draft.Session.Draft || draftID == "" {
			t.Fatalf("BeginNewSession 之后应当是草稿会话：session=%+v", draft.Session)
		}
		if err := harness.app.SaveComposerDraft(draftSmokeSentinel); err != nil {
			t.Fatalf("SaveComposerDraft: %v", err)
		}
		saved := harness.app.Snapshot()
		if !saved.Session.Draft || saved.Session.Composer != draftSmokeSentinel {
			t.Fatalf("未发送正文应当落在草稿槽上：session=%+v", saved.Session)
		}
		row, ok := draftSmokeDirectoryRow(saved, draftID)
		if !ok || row.Status != model.SessionStatusDraft {
			t.Fatalf("草稿会话在会话目录里应当以 status=draft 出现（前端草稿行的数据源）：row=%+v ok=%v", row, ok)
		}

		// 从草稿发第一轮：物化（草稿身份消失、草稿槽清空）+ 真 wire。
		if err := harness.app.SubmitToSession(ctx, draftID, "round-01"); err != nil {
			t.Fatalf("SubmitToSession(草稿): %v", err)
		}
		draftSmokeWaitIdle(t, ctx, harness.app)
		materialized := harness.app.Snapshot()
		if materialized.Session.Draft {
			t.Fatalf("物化之后不应还是草稿会话：session=%+v", materialized.Session)
		}
		if strings.TrimSpace(materialized.Session.Composer) != "" {
			t.Fatalf("物化之后草稿槽应当清空（clearComposerDraft 按残留收敛）：%q", materialized.Session.Composer)
		}
		if row, ok := draftSmokeDirectoryRow(materialized, draftID); ok && row.Status == model.SessionStatusDraft {
			t.Fatalf("物化之后目录行不该还标 draft：%+v", row)
		}
		if err := harness.app.SubmitToSession(ctx, draftID, "round-02"); err != nil {
			t.Fatalf("SubmitToSession(第二轮): %v", err)
		}
		draftSmokeWaitIdle(t, ctx, harness.app)

		bodies := provider.bodiesSnapshot()
		if len(bodies) < 2 {
			t.Fatalf("应当至少两次 provider 请求，实际 %d", len(bodies))
		}
		for index, body := range bodies {
			if strings.Contains(string(body), "草稿哨兵") {
				t.Fatalf("req#%d 把未发送草稿发给了 provider（草稿不该进 wire 前缀）：%.400s", index+1, body)
			}
		}
		// 跨轮前缀不变量：相邻请求必须保持"前者是后者的完整前缀"（提示缓存前提）。
		assertFullChainPrefixInvariant(t, bodies)
		t.Logf("前缀匹配通过：%d 次 provider 请求、跨轮前缀逐条一致，草稿哨兵未出现在任何请求里", len(bodies))
	})

	t.Run("历史的留存：重启后既有消息仍在，草稿归属可观测", func(t *testing.T) {
		provider := newDraftSmokeProvider(t)
		root := t.TempDir()
		harness := newFullChainHarness(t, draftSmokeAccounts(t, provider.URL, root), root, 10*time.Second)

		const rounds = 3
		for round := 1; round <= rounds; round++ {
			if err := harness.app.Submit(ctx, fmt.Sprintf("round-%02d", round)); err != nil {
				t.Fatalf("submit round %d: %v", round, err)
			}
			draftSmokeWaitIdle(t, ctx, harness.app)
		}
		sessionID := harness.app.Snapshot().Session.ID
		before := visiblePairMessages(harness.app.Snapshot().Conversation)
		if len(before) != rounds*2 {
			t.Fatalf("重启前可见消息对 = %d, want %d（%s）", len(before), rounds*2, draftSmokeDescribe(harness.app.Snapshot().Conversation, 6))
		}
		// 另开一份未发送草稿：重启后它的归属/正文是否留存，单列一条观测。
		if err := harness.app.BeginNewSession(); err != nil {
			t.Fatalf("BeginNewSession: %v", err)
		}
		draftID := harness.app.Snapshot().Session.ID
		if err := harness.app.SaveComposerDraft(draftSmokeSentinel); err != nil {
			t.Fatalf("SaveComposerDraft: %v", err)
		}
		harness.app.Shutdown()
		assertSessionStorageEvidence(t, root)

		restarted := newFullChainHarness(t, draftSmokeAccounts(t, provider.URL, root), root, 10*time.Second)
		defer restarted.app.Shutdown()
		if err := restarted.app.ResumeSession(sessionID); err != nil {
			t.Fatalf("重启后 ResumeSession: %v", err)
		}
		resumed := restarted.app.Snapshot()
		if resumed.Session.ID != sessionID {
			t.Fatalf("重启后视图会话 = %q, want %q", resumed.Session.ID, sessionID)
		}
		after := visiblePairMessages(resumed.Conversation)
		if problems := compareWirePrefix(after, before); len(problems) > 0 {
			t.Fatalf("重启后既有消息没有留存：\n%s", strings.Join(problems, "\n"))
		}
		row, ok := draftSmokeDirectoryRow(resumed, draftID)
		t.Logf("历史留存通过：重启后 %d 条可见消息逐条相同", len(after))
		t.Logf("草稿归属观测：重启后视图会话 draft=%v composer=%q；草稿会话 %s 目录行=%+v 存在=%v",
			resumed.Session.Draft, resumed.Session.Composer, draftID, row, ok)
	})

	t.Run("中断会话恢复：取消在途一轮后历史不被吞、重启后仍可继续", func(t *testing.T) {
		provider := newDraftSmokeProvider(t)
		root := t.TempDir()
		harness := newFullChainHarness(t, draftSmokeAccounts(t, provider.URL, root), root, 10*time.Second)

		if err := harness.app.Submit(ctx, "round-01"); err != nil {
			t.Fatalf("submit round-01: %v", err)
		}
		draftSmokeWaitIdle(t, ctx, harness.app)
		sessionID := harness.app.Snapshot().Session.ID
		settled := visiblePairMessages(harness.app.Snapshot().Conversation)
		if len(settled) != 2 {
			t.Fatalf("第一轮定稿后可见消息对 = %d, want 2（%s）", len(settled), draftSmokeDescribe(harness.app.Snapshot().Conversation, 4))
		}

		// 扣住 provider：第二轮请求在途时按取消（用户点停止）。
		provider.setHold(true)
		if err := harness.app.Submit(ctx, "round-02"); err != nil {
			t.Fatalf("submit round-02: %v", err)
		}
		holdCtx, holdCancel := context.WithTimeout(ctx, 30*time.Second)
		defer holdCancel()
		if !provider.waitForRequest(holdCtx) {
			t.Fatalf("第二轮 provider 请求没有到达，无法制造中断窗口")
		}
		if !harness.app.CancelChat("") {
			t.Fatalf("CancelChat 应当取消在途回合")
		}
		interrupted := draftSmokeWaitIdleTolerant(t, ctx, harness.app)
		interruptedPairs := visiblePairMessages(interrupted.Conversation)
		t.Logf("中断后：running=%v chat.error=%q 可见消息对=%d（%s）",
			interrupted.Chat.Running, interrupted.Chat.Error, len(interruptedPairs), draftSmokeDescribe(interrupted.Conversation, 4))
		if len(interruptedPairs) < len(settled) {
			t.Fatalf("中断吞掉了已定稿的历史：%d → %d", len(settled), len(interruptedPairs))
		}
		if problems := compareWirePrefix(interruptedPairs[:len(settled)], settled); len(problems) > 0 {
			t.Fatalf("中断改写了已定稿的历史：\n%s", strings.Join(problems, "\n"))
		}
		interruptedRetained := draftSmokeHasRoleText(interruptedPairs, "user", "round-02")
		harness.app.Shutdown()

		// 重启恢复：历史仍在，且能继续提交拿到回答。
		provider.setHold(false)
		provider.drainArrived()
		restarted := newFullChainHarness(t, draftSmokeAccounts(t, provider.URL, root), root, 10*time.Second)
		defer restarted.app.Shutdown()
		if err := restarted.app.ResumeSession(sessionID); err != nil {
			t.Fatalf("重启后 ResumeSession: %v", err)
		}
		resumed := restarted.app.Snapshot()
		resumedPairs := visiblePairMessages(resumed.Conversation)
		resumedSettled := draftSmokeSettled(resumedPairs)
		if problems := compareWirePrefix(resumedSettled, draftSmokeSettled(interruptedPairs)); len(problems) > 0 {
			t.Fatalf("中断会话重启后历史没有留存：\n%s\n  interrupted=%+v\n  resumed=%+v",
				strings.Join(problems, "\n"), draftSmokeSettled(interruptedPairs), resumedSettled)
		}
		if !draftSmokeHasRoleText(resumedSettled, "user", "round-02") {
			t.Fatalf("中断那一轮的 user 正文没有留存到重启后：%+v", resumedSettled)
		}
		if err := restarted.app.Submit(ctx, "round-03"); err != nil {
			t.Fatalf("中断恢复后继续提交: %v", err)
		}
		draftSmokeWaitIdle(t, ctx, restarted.app)
		finalPairs := visiblePairMessages(restarted.app.Snapshot().Conversation)
		if !draftSmokeHasRoleText(finalPairs, "user", "round-03") {
			t.Fatalf("中断恢复后的新一轮没有进入会话：%+v", finalPairs)
		}
		if len(finalPairs) <= len(resumedPairs) {
			t.Fatalf("中断恢复后的新一轮没有产出回答：%d → %d（%+v）", len(resumedPairs), len(finalPairs), finalPairs)
		}
		t.Logf("中断会话恢复通过：中断那轮 user 正文留存=%v；重启后可见消息对 %d → 继续一轮后 %d",
			interruptedRetained, len(resumedPairs), len(finalPairs))
	})

	// 草稿正文的跨重启留存目前是**观测**而不是硬断言：它取决于"重启后的会话装配
	// 路径是否把持久草稿重新装回视图"（application/core/service_assembler.go 的
	// initialDraft 分支 + restorePersistedDraft）。把证据留在日志里，由
	// docs/devlog 的未修清单跟。
	t.Run("草稿的留存（观测）：重启后未发送草稿还能不能回来", func(t *testing.T) {
		provider := newDraftSmokeProvider(t)
		root := t.TempDir()
		harness := newFullChainHarness(t, draftSmokeAccounts(t, provider.URL, root), root, 10*time.Second)
		if err := harness.app.BeginNewSession(); err != nil {
			t.Fatalf("BeginNewSession: %v", err)
		}
		draftID := harness.app.Snapshot().Session.ID
		if err := harness.app.SaveComposerDraft(draftSmokeSentinel); err != nil {
			t.Fatalf("SaveComposerDraft: %v", err)
		}
		harness.app.Shutdown()
		assertSessionStorageEvidence(t, root)

		restarted := newFullChainHarness(t, draftSmokeAccounts(t, provider.URL, root), root, 10*time.Second)
		defer restarted.app.Shutdown()
		booted := restarted.app.Snapshot()
		row, ok := draftSmokeDirectoryRow(booted, draftID)
		restored := booted.Session.Draft && strings.TrimSpace(booted.Session.Composer) != ""
		t.Logf("重启后（未 ResumeSession）：视图会话 id=%q draft=%v composer=%q；草稿会话 %s 目录行=%+v 存在=%v",
			booted.Session.ID, booted.Session.Draft, booted.Session.Composer, draftID, row, ok)
		t.Logf("草稿正文是否随重启回来：%v（判据：视图会话 draft=true 且 composer 非空）", restored)
	})
}
