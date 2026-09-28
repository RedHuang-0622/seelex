//go:build compactlive

// 前缀链路《压缩四区模型》落地的真·API 冒烟（opt-in，走**完整应用链路**）：
//
//	app.Submit（真实 provider）→ /compact 显式折叠 → 压缩记录 + 帧正文
//	→ 改配置再压一次 → 两次的**决策事实**必须如实反映配置差异
//
// 断言三件只有真链路才能证的事：
//
//  1. 保护区下限（limits.context_retain_floor_percent）真的抬高保留区：
//     占比窗口（window.ratio）收到 0.05 后 min(token1, token2) 本会把保留区压到
//     很小，配了下限之后 retained 抬到「比例 × 预算」，且 floor_applied 翻真；
//  2. 四区显式化在**帧正文**里可回读（分区 + 各区 token 数与来源 + 保留窗口决策）；
//  3. 打点（compaction.progress 门禁 Detail）带上保留窗口决策与四区 token 数——
//     报表面与判据面是同一份 ContextLayout。
//
// 运行：
//
//	$env:SEELEX_SMOKE_ACCOUNTS='G:\Program\go\seelex\config\accounts.yaml'
//	go test -tags compactlive . -run TestPrefixChainRetainFloorLiveSmoke -count=1 -v -timeout=15m
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/core"
	"github.com/RedHuang-0622/seelex/application/model"
)

// applyRetainFloorPercent 设置 limits.context_retain_floor_percent（进程内生效配置：
// 协调器每次装配读同一份 limits，因此改配置对下一条消息立即生效）。
func applyRetainFloorPercent(percent int) {
	applied := core.Limits()
	applied.ContextRetainFloorPercent = percent
	core.ApplyLimits(applied)
}

// prefixLiveMetadata 是帧正文 v2 元数据块里本冒烟要断言的字段。
//
// **黑盒结构**：刻意不复用 context_runtime 的未导出类型。这里测的是持久化格式
// 本身，复用会让"格式变了、测试跟着改"，失去报警能力。
//
// v1 的散文行（`retained=707`、`floor_applied=true`、`## Context zones`）已不再
// 存在；正则在正文里抓这些字面量会**静默抓空**，所以改为解析 JSON。
type prefixLiveMetadata struct {
	Schema string `json:"schema"`
	Layout struct {
		Zones []struct {
			Kind   string `json:"kind"`
			Tokens int    `json:"tokens"`
			Source string `json:"source"`
		} `json:"zones"`
		Retain struct {
			AllContextTokens int  `json:"all_context_tokens"`
			FloorTokens      int  `json:"floor_tokens"`
			Retained         int  `json:"retained"`
			FloorApplied     bool `json:"floor_applied"`
		} `json:"retain"`
	} `json:"layout"`
}

// prefixLiveParseMetadata 从帧正文抽出 ```json 元数据块并解析。抽不出/解析不了
// 直接失败：正文里没有可解析的元数据块，就等于这一帧无法被逐字段对拍。
func prefixLiveParseMetadata(t *testing.T, stage, body string) prefixLiveMetadata {
	t.Helper()
	const open = "```json\n"
	start := strings.Index(body, open)
	if start < 0 {
		t.Fatalf("%s：帧正文没有 ```json 元数据块（不是 v2 形状）：%s", stage, truncateForLog(body))
	}
	rest := body[start+len(open):]
	end := strings.Index(rest, "\n```")
	if end < 0 {
		t.Fatalf("%s：帧正文的 json 块没有闭合：%s", stage, truncateForLog(body))
	}
	var meta prefixLiveMetadata
	if err := json.Unmarshal([]byte(rest[:end]), &meta); err != nil {
		t.Fatalf("%s：元数据块不是合法 JSON（%v）：%s", stage, err, truncateForLog(rest[:end]))
	}
	if meta.Schema == "" {
		t.Fatalf("%s：元数据块缺少 schema 标识：%s", stage, truncateForLog(rest[:end]))
	}
	return meta
}

func TestPrefixChainRetainFloorLiveSmoke(t *testing.T) {
	accountsSource := strings.TrimSpace(os.Getenv("SEELEX_SMOKE_ACCOUNTS"))
	if accountsSource == "" {
		t.Skip("设置 SEELEX_SMOKE_ACCOUNTS 指向 accounts.yaml 才能运行真实 API 前缀链路冒烟")
	}

	projectRoot := t.TempDir()
	accountsPath := filepath.Join(projectRoot, "accounts.yaml")
	copyAccountsOpaqueForCompact(t, accountsSource, accountsPath)
	rewriteAccountsContextLimits(t, accountsPath, compactSmokeContextWindow, compactSmokeMaxTokens)

	harness := newFullChainHarness(t, accountsPath, projectRoot, 90*time.Second)
	defer harness.app.Shutdown()

	// 占比窗口收到 0.05：min(token1, token2) 本会把保留区压到很小 —— 这正是保护区
	// 下限要挡的坏方向（《压缩四区模型》边界判定）。保留上限沿用配置（token1 兜底 =
	// 账号窗口），因此下限不会与保留上限冲突（floor <= retain_tokens）。
	previousWindow := core.CurrentWindowConfig()
	core.ApplyWindowConfig(core.WindowConfig{Ratio: 0.05})
	defer core.ApplyWindowConfig(previousWindow)

	previousLimits := core.Limits()
	defer core.ApplyLimits(previousLimits)
	applyRetainFloorPercent(0)

	progress := subscribePrefixLiveProgress(harness.events)
	defer progress.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	submit := func(stage, prompt string) model.Snapshot {
		t.Helper()
		turnCtx, turnCancel := context.WithTimeout(ctx, 4*time.Minute)
		defer turnCancel()
		if err := harness.app.Submit(turnCtx, prompt); err != nil {
			t.Fatalf("%s：提交失败: %v", stage, err)
		}
		if err := harness.app.WaitForIdle(turnCtx); err != nil {
			snapshot := harness.app.Snapshot()
			t.Fatalf("%s：真实回合未回到 idle（%v）：running=%v error=%q 对话=%s",
				stage, err, snapshot.Chat.Running, snapshot.Chat.Error,
				describeConversationTail(snapshot.Conversation, 4))
		}
		snapshot := harness.app.Snapshot()
		if snapshot.Chat.Error != "" {
			t.Fatalf("%s：真实回合失败: %s", stage, snapshot.Chat.Error)
		}
		return snapshot
	}

	// 先攒四小片材料（单条远低于外置阈值），让全量上下文明显超过「比例 × 预算」
	// 的下限（= 预算 × 50%），从而能观察到下限真的把保留区抬起来。
	for part := 1; part <= 4; part++ {
		submit(fmt.Sprintf("前缀链路冒烟材料 %d", part), fmt.Sprintf(
			"这是上下文压力测试，禁止调用任何工具，只回复『已记录 %d』。下面是本片材料：\n%s",
			part, compactSmokePiece(1, part)))
	}

	baseline := prefixLiveCompactAndReadFrame(t, harness, submit, ctx, "下限未配置（0）")
	applyRetainFloorPercent(50)
	floored := prefixLiveCompactAndReadFrame(t, harness, submit, ctx, "下限 50% 预算")

	// ── 断言 1：保护区下限真的抬高保留区 ───────────────────────────
	baselineMeta := prefixLiveParseMetadata(t, "下限未配置（0）", baseline.Body)
	flooredMeta := prefixLiveParseMetadata(t, "下限 50% 预算", floored.Body)
	baselineRetained := baselineMeta.Layout.Retain.Retained
	flooredRetained := flooredMeta.Layout.Retain.Retained
	baselineFloor := baselineMeta.Layout.Retain.FloorTokens
	flooredFloor := flooredMeta.Layout.Retain.FloorTokens
	flooredAll := flooredMeta.Layout.Retain.AllContextTokens
	if baselineFloor != 0 {
		t.Fatalf("下限未配置时 floor 应为 0，得到 %d", baselineFloor)
	}
	if flooredFloor <= 0 {
		t.Fatalf("配置 50%% 后 floor 应大于 0，得到 %d", flooredFloor)
	}
	// 判定公式：retained = clamp(min(token1, token2), floor, token1)，再夹到全量上下文。
	wantFloored := flooredFloor
	if flooredAll < wantFloored {
		wantFloored = flooredAll
	}
	if flooredRetained != wantFloored {
		t.Fatalf("下限生效的保留区 = %d，want %d（floor=%d，全量上下文=%d）",
			flooredRetained, wantFloored, flooredFloor, flooredAll)
	}
	if flooredRetained <= baselineRetained {
		t.Fatalf("下限没有抬高保留区：baseline retained=%d floor=%d；floored retained=%d floor=%d",
			baselineRetained, baselineFloor, flooredRetained, flooredFloor)
	}
	if !flooredMeta.Layout.Retain.FloorApplied {
		t.Fatalf("配置生效的这次折叠必须报告 floor_applied=true：\n%s", truncateForLog(floored.Body))
	}
	if baselineMeta.Layout.Retain.FloorApplied {
		t.Fatalf("未配置下限的这次折叠必须报告 floor_applied=false：\n%s", truncateForLog(baseline.Body))
	}
	t.Logf("下限生效实测：baseline retained=%d floor=%d → floored retained=%d floor=%d all=%d",
		baselineRetained, baselineFloor, flooredRetained, flooredFloor, flooredAll)

	// ── 断言 2：四区显式化在帧正文里可回读 ─────────────────────────
	// v2 里四区是元数据块的 layout.zones 结构化数组，不再是 "## Context zones"
	// 散文区块。逐分区核对存在性与来源，比子串匹配更强：它同时钉住"分区在"与
	// "分区名对"，而不只是"正文里出现过这个词"。
	presentZones := make(map[string]string, len(flooredMeta.Layout.Zones))
	for _, zone := range flooredMeta.Layout.Zones {
		presentZones[zone.Kind] = zone.Source
	}
	for _, want := range []string{"stable_prefix", "folded", "protected_window", "tail", "current_input"} {
		source, ok := presentZones[want]
		if !ok {
			t.Fatalf("帧正文的元数据块缺少分区 %q（实得 %v）：\n%s",
				want, presentZones, truncateForLog(floored.Body))
		}
		if strings.TrimSpace(source) == "" {
			t.Fatalf("分区 %q 没有来源说明，读者无法判断这一段是什么：%s", want, truncateForLog(floored.Body))
		}
	}

	// ── 断言 3：打点（门禁 Detail）与判据同源 ──────────────────────
	gates := progress.Snapshot()
	if len(gates) == 0 {
		t.Fatal("没有收到 compaction.progress 事件：打点面没有接线")
	}
	var judgeDetail, assembleDetail string
	for _, gate := range gates {
		switch gate.Gate {
		case "judge":
			judgeDetail = gate.Detail
		case "assemble":
			assembleDetail = gate.Detail
		}
	}
	for _, want := range []string{"floor=", "retained=", "floor_applied=", "cap=", "ratio="} {
		if !strings.Contains(judgeDetail, want) {
			t.Fatalf("判据关门禁 Detail 缺少保留窗口事实 %q：%q", want, judgeDetail)
		}
	}
	for _, want := range []string{"stable_prefix=", "folded=", "protected_window=", "tail=", "current_input="} {
		if !strings.Contains(assembleDetail, want) {
			t.Fatalf("装配关门禁 Detail 缺少四区 token 数 %q：%q", want, assembleDetail)
		}
	}
	t.Logf("门禁打点实测：judge=%q assemble=%q", judgeDetail, assembleDetail)
}

// prefixLiveFrame 是一次折叠的压缩记录与帧正文。
type prefixLiveFrame struct {
	Record model.ContextCompaction
	Body   string
}

// prefixLiveCompactAndReadFrame 走显式 /compact 折叠一次并回读帧正文。
func prefixLiveCompactAndReadFrame(
	t *testing.T,
	harness fullChainHarness,
	submit func(stage, prompt string) model.Snapshot,
	ctx context.Context,
	stage string,
) prefixLiveFrame {
	t.Helper()
	snapshot := submit(stage+"/compact", "/compact")
	records := compactRecords(snapshot)
	if len(records) == 0 {
		t.Fatalf("%s：/compact 没有留下压缩记录（对话=%s）",
			stage, describeConversationTail(snapshot.Conversation, 4))
	}
	record := records[len(records)-1]
	if record.FrameRef == "" {
		t.Fatalf("%s：压缩记录没有 frame_ref，四区报表不可回读：%+v", stage, record)
	}
	page, err := harness.app.ReadToolResultHandler(ctx,
		fmt.Sprintf(`{"result_ref":%q,"offset":0,"limit":20000}`, record.FrameRef))
	if err != nil {
		t.Fatalf("%s：回读帧正文失败：%v", stage, err)
	}
	var decoded struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(page), &decoded); err != nil {
		t.Fatalf("%s：帧正文分页不是 JSON：%v", stage, err)
	}
	if !strings.Contains(decoded.Content, "checkpoint frame v") {
		t.Fatalf("%s：回读到的不是压缩帧正文：%s", stage, truncateForLog(decoded.Content))
	}
	return prefixLiveFrame{Record: record, Body: decoded.Content}
}

// subscribePrefixLiveProgress 订阅压缩门禁打点（compaction.progress），按会话外的
// 全局订阅取全部轮次；Close 后 Snapshot 返回已收到的门禁载荷（按到达顺序）。
func subscribePrefixLiveProgress(hub *application.EventHub) *prefixLiveProgress {
	sink := &prefixLiveProgress{subscription: hub.Subscribe(1024), done: make(chan struct{})}
	go sink.consume()
	return sink
}

type prefixLiveProgress struct {
	subscription application.Subscription
	done         chan struct{}
	mu           sync.Mutex
	gates        []application.CompactionProgress
}

func (s *prefixLiveProgress) consume() {
	defer close(s.done)
	for event := range s.subscription.Events {
		if event.Kind != application.EventCompactionProgress {
			continue
		}
		var gate application.CompactionProgress
		if json.Unmarshal(event.Payload, &gate) != nil {
			continue
		}
		s.mu.Lock()
		s.gates = append(s.gates, gate)
		s.mu.Unlock()
	}
}

// Snapshot 返回已收到的全部门禁载荷（只到 Close 为止）。
func (s *prefixLiveProgress) Snapshot() []application.CompactionProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]application.CompactionProgress(nil), s.gates...)
}

// Close 停止订阅并等待消费者退出。
func (s *prefixLiveProgress) Close() {
	s.subscription.Close()
	<-s.done
}
