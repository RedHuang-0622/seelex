package task_context

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/state"
)

// planFrameCoordinator 造一台只有 plan 事实的协调器（D1 判据的单元级夹具）。
func planFrameCoordinator(sessionID string, plan *model.PlanState) *Coordinator {
	core := &state.Core{}
	core.Snapshot.Session.ID = sessionID
	core.Snapshot.Runtime.Plan = plan
	return NewCoordinator(Deps{
		Core:             core,
		CurrentSessionID: func() string { return sessionID },
	})
}

func runningPlan(label string, status model.NodeStatus) *model.PlanState {
	return &model.PlanState{
		Name: label, EntryNodeID: "n1", Status: model.PlanRunning,
		Nodes: []model.PlanNode{{ID: "n1", Label: label, Kind: "task", Status: status}},
	}
}

// TestPlanFrameRecordedOnceAndAppendedOnChange 钉住 D1 的三条硬判据：
// ① 帧首现即作为 internal 事件（wire_material）落进 transcript；
// ② plan 内容不变 ⇒ 一条不发（幂等，位置不动）；
// ③ 内容变了 ⇒ 尾部追加新帧、旧帧留史。
func TestPlanFrameRecordedOnceAndAppendedOnChange(t *testing.T) {
	sessionID := "session-plan-frame"
	plan := runningPlan("核对前缀链路", model.NodeRunning)
	coordinator := planFrameCoordinator(sessionID, plan)

	coordinator.SetPlanStateLocked([]model.SessionPlanFrame{{ID: "plan-1", Plan: plan}}, "plan-1")
	if len(coordinator.TranscriptFor(sessionID)) != 1 {
		t.Fatalf("首现必须落一条帧事件，实际 %d 条", len(coordinator.TranscriptFor(sessionID)))
	}
	first := coordinator.TranscriptFor(sessionID)[0]
	if first.Kind != model.TranscriptEventKindInternal || !first.WireMaterial {
		t.Fatalf("plan 帧必须是 internal + wire_material，实际 kind=%q wire_material=%t", first.Kind, first.WireMaterial)
	}
	if !strings.HasPrefix(first.Content, PlanContextPrefix) {
		t.Fatalf("帧正文必须以 %q 开头: %q", PlanContextPrefix, first.Content)
	}
	// wire 上是 system 角色（内部材料），不是用户提问、也不开新一轮。
	if role := providerRoleForTranscriptEvent(first); role != "system" {
		t.Fatalf("plan 帧的 provider role = %q，期望 system", role)
	}
	if isUserQuestionEvent(first) {
		t.Fatal("plan 帧是状态材料，不能算用户提问")
	}

	// ② 内容没变：一条不发（同一份内容重发会把前缀往后推）。
	coordinator.SetPlanStateLocked([]model.SessionPlanFrame{{ID: "plan-1", Plan: plan}}, "plan-1")
	coordinator.RecordPlanFrameForLocked(sessionID)
	if got := len(coordinator.TranscriptFor(sessionID)); got != 1 {
		t.Fatalf("plan 没变却追加了帧：events=%d", got)
	}

	// ③ 内容变了：尾部追加新帧，旧帧留史（后写覆盖前写）。
	next := runningPlan("核对前缀链路", model.NodeCompleted)
	coordinator.Core.Snapshot.Runtime.Plan = next
	coordinator.SetPlanStateLocked([]model.SessionPlanFrame{{ID: "plan-1", Plan: next}}, "plan-1")
	events := coordinator.TranscriptFor(sessionID)
	if len(events) != 2 {
		t.Fatalf("plan 变化必须追加一条新帧，实际 events=%d", len(events))
	}
	if events[0].Content == events[1].Content {
		t.Fatal("新帧正文必须与旧帧不同（节点状态变了）")
	}
	if events[0].Seq >= events[1].Seq {
		t.Fatalf("新帧必须追加在尾部：seq %d >= %d", events[0].Seq, events[1].Seq)
	}
}

// TestPlanFrameTombstoneOnClear 钉住"计划清除要发墓碑"：帧一旦留史就永久可见，
// 计划没了若一条不发，模型会继续把旧计划当权威。
func TestPlanFrameTombstoneOnClear(t *testing.T) {
	sessionID := "session-plan-clear"
	plan := runningPlan("收尾计划", model.NodeRunning)
	coordinator := planFrameCoordinator(sessionID, plan)
	coordinator.SetPlanStateLocked([]model.SessionPlanFrame{{ID: "plan-1", Plan: plan}}, "plan-1")

	coordinator.Core.Snapshot.Runtime.Plan = nil
	coordinator.ClearActivePlanLocked()
	events := coordinator.TranscriptFor(sessionID)
	if len(events) != 2 {
		t.Fatalf("计划清除必须追加墓碑帧，实际 events=%d", len(events))
	}
	last := events[len(events)-1]
	if !strings.HasPrefix(last.Content, PlanContextPrefix) {
		t.Fatalf("墓碑帧正文必须以 %q 开头: %q", PlanContextPrefix, last.Content)
	}
	if !strings.Contains(last.Content, `"status":"none"`) {
		t.Fatalf("墓碑帧必须写明 plan 已不活跃: %q", last.Content)
	}
	// 幂等：再清一次不追加。
	coordinator.ClearActivePlanLocked()
	if got := len(coordinator.TranscriptFor(sessionID)); got != 2 {
		t.Fatalf("重复清除追加了帧：events=%d", got)
	}
}

// TestPlanFrameSeedsHashFromTranscript 钉住冷加载后的去重基线：恢复时 transcript
// 里已有一条同内容帧 ⇒ 一条不发（否则每次装载都重发一份）。
func TestPlanFrameSeedsHashFromTranscript(t *testing.T) {
	sessionID := "session-plan-resume"
	plan := runningPlan("恢复计划", model.NodeRunning)
	recorder := planFrameCoordinator(sessionID, plan)
	recorder.SetPlanStateLocked([]model.SessionPlanFrame{{ID: "plan-1", Plan: plan}}, "plan-1")
	frame := recorder.TranscriptFor(sessionID)[0]

	// 新进程：同一个计划 + 同一个 transcript 尾部窗口（去重基线是空的，必须从
	// 事件流里最后一条帧对齐）。
	resumed := planFrameCoordinator(sessionID, runningPlan("恢复计划", model.NodeRunning))
	resumed.stateMu.Lock()
	runtime := resumed.sessionStateLocked(sessionID)
	runtime.planStack = []model.SessionPlanFrame{{ID: "plan-1", Plan: runningPlan("恢复计划", model.NodeRunning)}}
	runtime.activePlanID = "plan-1"
	runtime.planSequence = 1
	runtime.transcript = []model.TranscriptEvent{frame}
	runtime.transcriptSeq = frame.Seq
	appended := resumed.recordPlanFrameLocked(runtime)
	events := runtime.transcript
	resumed.stateMu.Unlock()
	if appended {
		t.Fatal("恢复同内容计划不该重发帧")
	}
	if len(events) != 1 {
		t.Fatalf("恢复同内容计划不该重发帧：events=%d", len(events))
	}
}

// TestPlanFrameIsStandaloneProtocolUnit 钉住单元切分：plan 帧自成一格，不把下一轮
// 的 assistant 消息卷进自己的单元（与激活技能正文同一姿势）。
func TestPlanFrameIsStandaloneProtocolUnit(t *testing.T) {
	frame := model.TranscriptEvent{
		Seq: 1, Role: "user", Kind: model.TranscriptEventKindInternal, WireMaterial: true,
		Content: PlanContextPrefix + "\n{}",
	}
	events := []model.TranscriptEvent{
		frame,
		{Role: "user", Content: "inspect the repo"},
		{Role: "assistant", Content: "done"},
	}
	units := transcriptProtocolUnits(events)
	if len(units) != 2 {
		t.Fatalf("units = %d，期望 2（plan 帧 + 真实轮）", len(units))
	}
	if len(units[0]) != 1 || units[0][0].Content != frame.Content {
		t.Fatalf("首单元必须只含 plan 帧: %#v", units[0])
	}
	if len(units[1]) != 2 {
		t.Fatalf("次单元必须是真实轮（提问 + 答复）: %#v", units[1])
	}
}
