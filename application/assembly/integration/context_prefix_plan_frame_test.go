//go:build !extended

package integration

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/context/prefixprobe"
)

// plan 帧变体：断言 plan 帧**位置不漂**、前缀完整（D1 落地后的形态）。
//
// 历史（本用例原先钉的是缺陷）：plan 上下文消息由 `planContextMessageLocked`
// 每轮**现算现插**在"保留历史之后、当前输入之前"。历史每轮变长，插入点就往后挪
// 一格——上一次请求里 plan 占的那一格，这一次被新历史占住。失效因此不是"plan 那
// 一段"，而是**从 plan 那一格起直到请求末尾全部重算**（实测 turn2
// roles=[user system user] → turn3 roles=[user user system user]，
// hit=16767/17520=95.7%，UnexpectedMiss=1）。
//
// D1 形态（kernel/task/plan_frame.go）：plan 帧在**计划状态写入的那一刻**作为
// internal 事件（WireMaterial=true）落进 transcript，此后按记录位置重放；
// 内容哈希不变 ⇒ 一条不发；变了 ⇒ 尾部追加新帧、旧帧留史（后写覆盖前写）。
//
// 运行：go test ./application/assembly/integration -run ContextPrefixPlanFrame -v -count=1

func TestContextPrefixPlanFrame(t *testing.T) {
	harness := newPrefixProbeHarness(t, 1_000_000)
	harness.startStream()

	harness.beginTurn("第 1 轮：先看一眼装配顺序。")
	harness.capture(1, 1, "t1.iter1")

	// 装载 plan（模拟 plan_load 落地）。
	setActivePlan(harness, "第 1 版计划：核对前缀链路", model.NodeRunning)
	harness.beginTurn("第 2 轮：按计划推进。")
	harness.capture(2, 1, "t2.iter1")

	// plan 推进：同一张 plan 的节点状态变了（正文随之变化）。
	setActivePlan(harness, "第 2 版计划：前缀链路已核对，进入修复", model.NodeCompleted)
	harness.beginTurn("第 3 轮：继续。")
	harness.capture(3, 1, "t3.iter1")

	// 计划不变的第 4 轮：plan_hash 没变 ⇒ 一条不发（帧数不涨、位置不动）。
	framesBefore := countPlanFrames(harness)
	harness.beginTurn("第 4 轮：计划没变。")
	harness.capture(4, 1, "t4.iter1")

	probe := prefixprobe.New()
	var results []prefixprobe.Result
	for index := 1; index < len(harness.calls); index++ {
		prev, cur := harness.calls[index-1], harness.calls[index]
		segments, refs := probeSegmentsAndMessages(systemPromptFor(harness.service), cur.messages, cur.bytes)
		if cur.key.SystemDigest == "" || cur.key.ToolsDigest == "" {
			t.Fatalf("call %s 没有真 prefix_key：%+v", cur.label, cur.key)
		}
		res := probe.Observe(uint64(cur.turn), prefixprobe.Sample{
			Wire: cur.bytes, Segments: segments, Messages: refs, Key: cur.key,
		})
		results = append(results, res)
		t.Logf("  %s  (msgs=%d)", res.Summary(), len(cur.messages))
		if res.Intact() {
			continue
		}
		idx := res.Divergence.MessageIndex
		t.Logf("    prev roles=%v", probeRoles(prev.messages))
		t.Logf("    cur  roles=%v", probeRoles(cur.messages))
		t.Logf("    prev msg#%d(%s)=%q", idx, probeMessageAt(prev.messages, idx).Role,
			summarizeProbeText(probeMessageContentAt(prev.messages, idx)))
		t.Logf("    cur  msg#%d(%s)=%q", idx, probeMessageAt(cur.messages, idx).Role,
			summarizeProbeText(probeMessageContentAt(cur.messages, idx)))
	}

	cum := probe.Cumulative()
	t.Logf("== 累计: requests=%d hit=%d/%d (%.1f%%) unexpected=%d ==",
		cum.Requests, cum.HitChars, cum.TotalChars, cum.Ratio()*100, cum.UnexpectedMiss)

	// ① plan 帧首现即作为 internal 事件（WireMaterial）落进 transcript。
	frames := planFrames(harness)
	if len(frames) == 0 {
		t.Fatalf("期望 plan 帧作为 internal 事件落进 transcript，实际一条都没有")
	}
	for index, frame := range frames {
		t.Logf("  transcript plan 帧 #%d seq=%d kind=%q wire_material=%t chars=%d",
			index, frame.Seq, frame.Kind, frame.WireMaterial, len(frame.Content))
		if frame.Kind != model.TranscriptEventKindInternal {
			t.Fatalf("plan 帧 #%d 的 Kind=%q，期望 %q", index, frame.Kind, model.TranscriptEventKindInternal)
		}
		if !frame.WireMaterial {
			t.Fatalf("plan 帧 #%d 未标记 wire_material：重启后它进不了装配", index)
		}
		if !strings.HasPrefix(frame.Content, planContextMarker) {
			t.Fatalf("plan 帧 #%d 正文不以 %q 开头", index, planContextMarker)
		}
	}

	// ② 计划不变的那一轮一条不发：帧数不涨。
	if after := countPlanFrames(harness); after != framesBefore {
		t.Fatalf("计划未变化的回合追加了 plan 帧（before=%d after=%d）：同内容重复发会把前缀往后推", framesBefore, after)
	}
	t.Logf("== plan 帧去重: 计划未变化的第 4 轮帧数保持 %d ==", framesBefore)

	// ③ 前缀完整：只有"首现"那一格没有参照（results[0]），其余每次请求都必须以
	// 上一次为前缀。
	if len(results) < 2 {
		t.Fatalf("探针没有采样到跨请求比较（results=%d）", len(results))
	}
	for index := 1; index < len(results); index++ {
		if res := results[index]; !res.Intact() {
			t.Fatalf("第 %d 次跨请求比较前缀不完整: %s", index+1, res.Summary())
		}
	}
	if cum.UnexpectedMiss != 0 {
		t.Fatalf("plan 帧不该造成 UnexpectedMiss，实测 %d", cum.UnexpectedMiss)
	}
}

// planContextMarker 是 plan 上下文消息的固定前缀（kernel/task 的
// PlanContextPrefix）。用它把"这一格是 plan 帧"与"这一格是对话消息"分开。
const planContextMarker = "<!-- seelex:active-plan:v1 -->"

// planFrames 返回 transcript 里全部 plan 帧事件（按记录顺序）。
func planFrames(harness *prefixProbeHarness) []model.TranscriptEvent {
	frames := make([]model.TranscriptEvent, 0, 4)
	for _, event := range harness.service.components.tasks.TranscriptFor(harness.session) {
		if strings.HasPrefix(event.Content, planContextMarker) {
			frames = append(frames, event)
		}
	}
	return frames
}

func countPlanFrames(harness *prefixProbeHarness) int {
	return len(planFrames(harness))
}

// probeRoles 返回消息序列的 role 摘要（定位"消息流错位 vs 同位置改写"）。
func probeRoles(messages []contract.EngineMessage) []string {
	roles := make([]string, 0, len(messages))
	for _, message := range messages {
		roles = append(roles, message.Role)
	}
	return roles
}

// setActivePlan 装载一帧活跃 plan（复刻 plan_load 落地后的会话状态）。
//
// 注入链有两处：`ActivePlanProjection` 读的是**全局快照**的 `Runtime.Plan`，
// 而 `activePlanID` 来自会话状态；两者都要设，plan 帧才会真的产出正文。设置
// 动作本身就是**记录时刻**：帧在这一次调用里落进 transcript（排在下一个用户
// 提问之前），此后按记录位置重放。
func setActivePlan(harness *prefixProbeHarness, name string, node model.NodeStatus) {
	harness.t.Helper()
	plan := &model.PlanState{
		Name:        name,
		EntryNodeID: "n1",
		Status:      model.PlanRunning,
		Nodes: []model.PlanNode{{
			ID: "n1", Label: name, Kind: "task", Status: node,
		}},
	}
	harness.service.ViewMu.Lock()
	defer harness.service.ViewMu.Unlock()
	harness.service.Core.Snapshot.Runtime.Plan = plan
	harness.service.components.tasks.SetPlanStateLocked([]model.SessionPlanFrame{{
		ID: "plan-prefix", Plan: plan,
	}}, "plan-prefix")
}
