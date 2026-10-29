//go:build !extended

package integration

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/context/prefixprobe"
)

// plan 帧变体：钉住"plan 正文被现算现插在历史尾部（当前输入之前）"造成的分叉。
//
// 缺陷语义（I2 只追加）：plan 上下文消息由 `planContextMessageLocked` 每轮**重新
// 渲染**，位置在保留历史之后、当前输入之前。plan 一推进（节点状态变化），该位置
// 的字节就变——而它前面是整段历史、后面是当前输入，所以从该点起直到请求尾部
// 全部重算。
//
// D1 的目标形态是"只在 plan_hash 变化时**追加**一条，旧帧留史"，本条用例记录
// 现状（重算 → 分叉），D1 落地后翻转为"前缀完整"。
//
// 运行：go test ./application/assembly -run ContextPrefixPlanFrame -v -count=1

func TestContextPrefixPlanFrame_KnownDefect(t *testing.T) {
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

	var key prefixprobe.Key
	probe := prefixprobe.New()
	var results []prefixprobe.Result
	for index := 1; index < len(harness.calls); index++ {
		prev, cur := harness.calls[index-1], harness.calls[index]
		segments, refs := probeSegmentsAndMessages(systemPromptFor(harness.service), cur.messages, cur.bytes)
		res := probe.Observe(uint64(cur.turn), prefixprobe.Sample{
			Wire: cur.bytes, Segments: segments, Messages: refs, Key: key,
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

	var planBreaks []prefixprobe.Result
	for index := range results {
		res := results[index]
		if res.Intact() || res.Divergence == nil {
			continue
		}
		// 判定"这一格在上一侧是 plan 帧"：plan 消息带固定前缀标记。
		prevMessage := probeMessageAt(harness.calls[index].messages, res.Divergence.MessageIndex)
		if strings.HasPrefix(prevMessage.Content, planContextMarker) {
			planBreaks = append(planBreaks, res)
		}
	}
	if len(planBreaks) == 0 {
		t.Logf("== plan 帧未造成分叉：若刚完成 D1，请把本条用例翻转为断言前缀完整 ==")
		t.Fatalf("期望 plan 帧导致前缀分叉，实际未发生（若夹具未真正注入 plan 消息，这会是一条假阴性，必须先修夹具）")
	}
	for _, res := range planBreaks {
		t.Logf("  plan 分叉: %s", res.Divergence.String())
	}
	if cum.UnexpectedMiss == 0 {
		t.Fatalf("键未变却断开必须计入 UnexpectedMiss")
	}
}

// planContextMarker 是 plan 上下文消息的固定前缀（kernel/context 的
// planContextPrefix）。用它把"这一格是 plan 帧"与"这一格是对话消息"分开。
const planContextMarker = "<!-- seelex:active-plan:v1 -->"

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
// 而 `activePlanID` 来自会话状态；两者都要设，`planContextMessageLocked` 才会
// 真的产出 plan 消息。
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
