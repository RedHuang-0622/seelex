package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/model"
)

// compactTestService 构造带活跃任务执行的会话：主动压缩绑定请求纪元
// （TaskExecutionState.RequestID），没有纪元就无从压缩。
func compactTestService(t *testing.T, requestID string) (*Service, *fakeEngine, string) {
	t.Helper()
	runtime := runtimeWithContextLimits{fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192}
	engine := &fakeEngine{}
	service := newTestService(t, engine, withTestRuntime(runtime))
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: requestID}
	service.components.tasks.BeginTask(requestID, "inspect", "high", nil, TaskCheckpoint{})
	service.ViewMu.Unlock()
	sessionID := service.Snapshot().Session.ID
	if sessionID == "" {
		sessionID = service.components.tasks.SessionIDForRequest(requestID)
	}
	return service, engine, sessionID
}

// TestCompactContextHandlerFoldsTranscript：compact_context 工具（= /compact
// 的同一落点）在达到压缩阈值时主动折叠 transcript：留下压缩记录、丢掉窗口外
// 的旧轮次，并把结果以结构化 JSON 返回给模型。
func TestCompactContextHandlerFoldsTranscript(t *testing.T) {
	service, engine, sessionID := compactTestService(t, "task-compact-1")
	// 4 个已定稿轮，每轮约 4 万 tokens（16 万 ASCII 字符）→ 合计约 16 万，
	// 超过软阈值 125106，触发压缩；每轮内容带唯一前缀，便于断言"最旧轮被
	// 压出 provider 历史、最新轮保留"。
	roundOf := func(index int) string {
		return "round-" + string(rune('a'+index)) + ":" + strings.Repeat("A", 160_000)
	}
	service.ViewMu.Lock()
	for index := 0; index < 4; index++ {
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: "task-compact", Role: "user", Content: "question-" + string(rune('a'+index)),
		})
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: "task-compact", Role: "assistant", Content: roundOf(index),
		})
	}
	service.ViewMu.Unlock()

	ctx := task_context.WithSessionID(context.Background(), sessionID)
	raw, err := service.CompactContextHandler(ctx, `{"reason":"准备开始一段长任务"}`)
	if err != nil {
		t.Fatalf("compact_context: %v", err)
	}
	var result ContextCompactionResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("结果不是 JSON: %v (%s)", err, raw)
	}
	// MessagesBefore 记的是引擎历史条数（本夹具只往 transcript 追加事件，
	// 引擎历史为空 → 0 是准确值）；用版本与估算证明压缩确实发生。
	if !result.Compacted || result.Version == 0 || result.EstimatedTokens == 0 {
		t.Fatalf("压缩结果 = %+v", result)
	}
	if !strings.Contains(result.Note, "长任务") {
		t.Fatalf("模型自述的压缩原因未回带到结果：%q", result.Note)
	}
	// 窗口外的旧轮次不再进入 provider 历史（原始轮次仍在会话存储，按引用回读）；
	// 最新的轮次保留在有界窗口里，继续作为工作上下文。
	oldest, newest := roundOf(0), roundOf(3)
	foundNewest := false
	for _, message := range engine.History() {
		if strings.Contains(message.Content, oldest) {
			t.Fatal("压缩后最旧轮次仍留在 provider 历史里")
		}
		if strings.Contains(message.Content, newest) {
			foundNewest = true
		}
	}
	if !foundNewest {
		t.Fatal("压缩后最新轮次应从有界窗口保留，实际丢失")
	}
	service.ViewMu.RLock()
	compactions := service.components.tasks.CurrentTaskExecution().ContextCompactions
	service.ViewMu.RUnlock()
	if len(compactions) != 1 || compactions[0].Reason != "context_budget" {
		t.Fatalf("压缩记录 = %#v, want one context_budget record", compactions)
	}
}

// TestCompactManualFoldsBelowThreshold：显式压缩（/compact、compact_context）
// **不设阈值前提**——上下文远低于软阈值时照样折叠，并如实报告判据量。
//
// 这是「手动命令被上限挡住」的直接来源：此前显式路径仍以「超过软阈值」为前提，
// 未达阈值就回一句「未达压缩阈值」，而且句子里塞的是**装配后估算**（不是判据量），
// 于是能说出「129409 tokens，未达压缩阈值 118962」这种自相矛盾的话。
func TestCompactManualFoldsBelowThreshold(t *testing.T) {
	service, engine, sessionID := compactTestService(t, "task-compact-2")
	service.ViewMu.Lock()
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
		TaskID: "task-compact", Role: "user", Content: "small question",
	})
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
		TaskID: "task-compact", Role: "assistant", Content: "small answer",
	})
	service.ViewMu.Unlock()

	ctx := task_context.WithSessionID(context.Background(), sessionID)
	raw, err := service.CompactContextHandler(ctx, `{}`)
	if err != nil {
		t.Fatalf("compact_context: %v", err)
	}
	var result ContextCompactionResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Compacted || result.Version == 0 || !result.Recorded {
		t.Fatalf("显式压缩必须在低阈值下也折叠并留记录：%+v", result)
	}
	if result.ComparedTokens >= result.SoftThreshold {
		t.Fatalf("夹具应是低阈值场景（判据量 %d < 软阈值 %d），否则这条测试没有判别力",
			result.ComparedTokens, result.SoftThreshold)
	}
	if strings.Contains(result.Note, "未达压缩阈值") {
		t.Fatalf("低阈值下的显式压缩不应说「未达压缩阈值」：%q", result.Note)
	}
	service.ViewMu.RLock()
	compactions := service.components.tasks.CurrentTaskExecution().ContextCompactions
	service.ViewMu.RUnlock()
	if len(compactions) != 1 || compactions[0].Reason != "context_budget" {
		t.Fatalf("显式压缩应留下一条 context_budget 记录：%#v", compactions)
	}
	// 低阈值场景折叠后引擎历史仍带着两轮内容（窗口宽），不是"压没了"。
	if len(engine.History()) == 0 {
		t.Fatal("折叠后引擎历史不应为空")
	}
}

// TestCompactManualAfterTurnRecordsExplicitOrigin：回合已收尾（任务状态不再是
// Running）后用户打 /compact 或模型调 compact_context：折叠照做，**记录也照写**，
// 来源标记为 explicit_after_turn。
//
// 这正是此前必落 folded_without_record 的场景（用户 2026-09-23 实测回执：
// 「该回合的任务执行已收尾，压缩记录只在执行中产生，故本次不留记录」）：
// 记录门槛只看 Running，而"回合之间手动压缩"恰恰是最自然的用法，于是前端完全
// 看不到压缩（状态页「上下文压缩」区块为空、轨迹压缩轨整条不渲染）。
func TestCompactManualAfterTurnRecordsExplicitOrigin(t *testing.T) {
	service, _, sessionID := compactTestService(t, "task-finished")
	service.ViewMu.Lock()
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
		TaskID: "task-finished", MessageID: "message-1", Role: "assistant", Content: strings.Repeat("B", 4_000),
	})
	// 回合收尾：任务执行不再是 Running（同时按真实收尾路径写一次任务面，
	// 这样断言的是"用户会看到的那份快照"）。
	service.components.tasks.CurrentTaskExecution().Status = task_context.StatusCompleted
	service.components.tasks.SetTaskStateLocked("task-finished", model.TaskCompleted, "done")
	service.ViewMu.Unlock()

	ctx := task_context.WithSessionID(context.Background(), sessionID)
	raw, err := service.CompactContextHandler(ctx, `{}`)
	if err != nil {
		t.Fatalf("compact_context: %v", err)
	}
	var result ContextCompactionResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Compacted {
		t.Fatalf("折叠应发生在结果面报告为已压缩：%+v", result)
	}
	if !result.Recorded {
		t.Fatalf("回合收尾后的显式压缩必须留记录（否则前端看不到任何压缩）：%+v", result)
	}
	if result.Origin != model.CompactionOriginExplicitAfterTurn {
		t.Fatalf("来源应标记为 %q，实际 %q", model.CompactionOriginExplicitAfterTurn, result.Origin)
	}
	if !strings.Contains(result.Note, "已压缩上下文") || strings.Contains(result.Note, "不留记录") {
		t.Fatalf("回执应按记录成句、不得再说不留记录：%q", result.Note)
	}
	if result.FrameRef == "" || !strings.Contains(result.Note, result.FrameRef) {
		t.Fatalf("帧正文引用应随记录回带（前端按 ref 分页回读正文）：%+v", result)
	}
	service.ViewMu.RLock()
	records := service.components.tasks.CurrentTaskExecution().ContextCompactions
	snapshotTask := service.Core.Snapshot.Task
	service.ViewMu.RUnlock()
	if len(records) != 1 || records[0].Origin != model.CompactionOriginExplicitAfterTurn || records[0].FrameRef == "" {
		t.Fatalf("压缩记录 = %#v, want one explicit_after_turn record with frame ref", records)
	}
	if snapshotTask == nil || len(snapshotTask.ContextCompactions) != 1 {
		t.Fatalf("快照应带上压缩记录（前端可见面）：%#v", snapshotTask)
	}
}

// TestCompactAfterTurnSurfacesRecordWithoutTaskFace：快照里还没有任务面时
// （冷恢复后直接显式压缩），压缩记录也必须出现在快照里——否则"前端看不到
// 压缩"换个来路又回来了。
func TestCompactAfterTurnSurfacesRecordWithoutTaskFace(t *testing.T) {
	service, _, sessionID := compactTestService(t, "task-no-face")
	service.ViewMu.Lock()
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
		TaskID: "task-no-face", MessageID: "message-1", Role: "assistant", Content: strings.Repeat("F", 4_000),
	})
	service.components.tasks.CurrentTaskExecution().Status = task_context.StatusCompleted
	service.Core.Snapshot.Task = nil
	service.ViewMu.Unlock()

	ctx := task_context.WithSessionID(context.Background(), sessionID)
	if _, err := service.CompactContextNow(ctx); err != nil {
		t.Fatalf("CompactContextNow: %v", err)
	}
	service.ViewMu.RLock()
	task := service.Core.Snapshot.Task
	service.ViewMu.RUnlock()
	if task == nil || len(task.ContextCompactions) != 1 {
		t.Fatalf("无任务面时压缩记录也应进快照：%#v", task)
	}
	if task.ContextCompactions[0].Origin != model.CompactionOriginExplicitAfterTurn {
		t.Fatalf("记录来源 = %q, want %q", task.ContextCompactions[0].Origin, model.CompactionOriginExplicitAfterTurn)
	}
}

// TestCompactionFrameBodyIsReadableByRef：记录里的 frame_ref 真能读回帧正文——
// 前端"查看压缩帧"就走这条（Bridge.ToolResultContent → Service.ToolResultContent，
// 按 ref 分页）。正文只放内容存储、不进快照，所以这条通路必须成立。
func TestCompactionFrameBodyIsReadableByRef(t *testing.T) {
	service, _, sessionID := compactTestService(t, "task-frame-read")
	service.ViewMu.Lock()
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
		TaskID: "task-frame-read", MessageID: "message-1", Role: "assistant", Content: strings.Repeat("G", 4_000),
	})
	service.components.tasks.CurrentTaskExecution().Status = task_context.StatusCompleted
	service.ViewMu.Unlock()

	result, err := service.CompactContextNow(task_context.WithSessionID(context.Background(), sessionID))
	if err != nil {
		t.Fatalf("CompactContextNow: %v", err)
	}
	if result.FrameRef == "" {
		t.Fatalf("压缩记录应带帧正文引用：%+v", result)
	}
	page, err := service.ToolResultContent(context.Background(), result.FrameRef, 0, 0)
	if err != nil {
		t.Fatalf("按 ref 读帧正文: %v", err)
	}
	body := page.Content
	if !strings.Contains(body, "Context checkpoint frame v") {
		t.Fatalf("读回的正文不是帧正文：%q", body)
	}
	if !strings.Contains(body, "origin: "+model.CompactionOriginExplicitAfterTurn) {
		t.Fatalf("帧正文应带上这次压缩的来源：%q", body)
	}
	if page.TotalBytes != result.FrameBytes {
		t.Fatalf("帧正文体量应一致：page=%d record=%d", page.TotalBytes, result.FrameBytes)
	}
}

// TestAutoCompactionAfterTurnKeepsRecordGate：自动路径（软/硬阈值）在回合已
// 收尾时**仍然不补记**——放宽门槛只针对显式要求，否则会把上一回合的收尾状态
// 误标成"该回合压缩过"。
func TestAutoCompactionAfterTurnKeepsRecordGate(t *testing.T) {
	service, _, sessionID := compactTestService(t, "task-auto-finished")
	service.ViewMu.Lock()
	// 夹具：判据量过软阈值（4 轮 × 16 万字符 ≈ 16 万 tokens），且这批进展尚未被
	// 自动压过（CompactedEpoch != ProgressEpoch）→ 自动路径会折叠。
	for index := 0; index < 4; index++ {
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: "task-auto-finished", Role: "user", Content: "question-" + string(rune('a'+index)),
		})
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: "task-auto-finished", Role: "assistant", Content: strings.Repeat("E", 160_000),
		})
	}
	state := service.components.tasks.CurrentTaskExecution()
	state.Status = task_context.StatusCompleted
	state.ProgressEpoch = state.CompactedEpoch + 1
	before := state.ContextVersion
	service.ViewMu.Unlock()

	if err := service.components.context.CompactTaskContextFor(sessionID, "task-auto-finished"); err != nil {
		t.Fatalf("CompactTaskContextFor: %v", err)
	}
	service.ViewMu.RLock()
	after := service.components.tasks.CurrentTaskExecution().ContextVersion
	records := service.components.tasks.CurrentTaskExecution().ContextCompactions
	service.ViewMu.RUnlock()
	if after == before {
		t.Fatalf("夹具应触发自动折叠（否则这条测试没有判别力）：ContextVersion %d → %d", before, after)
	}
	if len(records) != 0 {
		t.Fatalf("自动路径在回合收尾后不得补记（门槛只对显式要求放宽）：%#v", records)
	}
}

// TestCompactContextWithoutTaskExecutionSchedulesNextAssembly：会话没有任务
// 执行纪元（刚冷加载/刚清空）时不伪造纪元——登记为「下一次装配时立即压缩」，
// 且该登记在下一条消息组装上下文时真的兑现（低阈值也折叠）。
func TestCompactContextWithoutTaskExecutionSchedulesNextAssembly(t *testing.T) {
	runtime := runtimeWithContextLimits{fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192}
	engine := &fakeEngine{}
	service := newTestService(t, engine, withTestRuntime(runtime))
	sessionID := service.Snapshot().Session.ID
	marks := appendWindowRounds(t, service, "task-scheduled", 2, 400)

	result, err := service.CompactContextNow(task_context.WithSessionID(context.Background(), sessionID))
	if err != nil {
		t.Fatalf("CompactContextNow: %v", err)
	}
	if result.Compacted || !result.Scheduled {
		t.Fatalf("无纪元时应登记而不是假装压缩：%+v", result)
	}
	if !strings.Contains(result.Note, "已登记") {
		t.Fatalf("结果面应说明已登记：%q", result.Note)
	}
	if len(engine.History()) != 0 {
		t.Fatalf("登记本身不应装配/改写 provider 历史：%d 条", len(engine.History()))
	}

	// 下一条消息：新纪元建立后装配上下文 → 登记的强压兑现（判据量远低于软阈值）。
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-scheduled-next"}
	service.components.tasks.BeginTask("task-scheduled-next", "next", "high", nil, TaskCheckpoint{})
	service.ViewMu.Unlock()
	if _, err := service.components.context.PrepareExecutionContextFor(sessionID, "task-scheduled-next", "next"); err != nil {
		t.Fatalf("装配 provider 上下文: %v", err)
	}
	if kept := retainedRounds(engine.History(), marks); kept >= len(marks) {
		t.Fatalf("登记的强压未兑现：kept=%d want<%d（低阈值也应折叠）", kept, len(marks))
	}
}

// TestCompactCommandRegisteredAndSharesPath：/compact 命令注册成功，且与工具
// 走同一条落点——显式路径不设阈值前提，低上下文也照样折叠。
func TestCompactCommandRegisteredAndSharesPath(t *testing.T) {
	service, _, _ := compactTestService(t, "task-compact-3")
	command, ok := service.commands.Get("compact")
	if !ok {
		t.Fatal("/compact 未注册")
	}
	if strings.TrimSpace(command.Description()) == "" {
		t.Fatal("/compact 缺少描述")
	}
	result, err := command.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("/compact: %v", err)
	}
	if !strings.Contains(result.Notice, "已压缩上下文") {
		t.Fatalf("/compact 提示 = %q", result.Notice)
	}
	if strings.Contains(result.Notice, "未达压缩阈值") {
		t.Fatalf("/compact 不得在显式调用下声称未达阈值：%q", result.Notice)
	}
}

// TestCompactCommandNoticeReportsFoldedRange：记录分支的提示只说**记录里已有的
// 区间字段**（message_from/to、event_from/to），不再印 MessagesBefore。
//
// 该夹具正是"装配前引擎历史为空"的场景（只往 transcript 追加事件、没有引擎
// 历史）：MessagesBefore=0，而被压区间是完整的 message-1..message-8。旧句式
// 「压缩前 %d 条消息」在这里会说出"压缩前 0 条消息"——一个与事实相反的数字
// （真实用户在 2026-09-23 会话里看到的就是它，同族句式还能说出"压缩前 2 条消息"，
// 那 2 条其实是引擎里的 system 行）。
func TestCompactCommandNoticeReportsFoldedRange(t *testing.T) {
	service, _, _ := compactTestService(t, "task-command-range")
	service.ViewMu.Lock()
	for index := 0; index < 4; index++ {
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: "task-command-range", MessageID: fmt.Sprintf("message-%d", index+1),
			Role: "user", Content: fmt.Sprintf("q-%d", index),
		})
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: "task-command-range", MessageID: fmt.Sprintf("message-%d", index+101),
			Role: "assistant", Content: fmt.Sprintf("a-%d:%s", index, strings.Repeat("A", 4_000)),
		})
	}
	service.ViewMu.Unlock()

	command, ok := service.commands.Get("compact")
	if !ok {
		t.Fatal("/compact 未注册")
	}
	result, err := command.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("/compact: %v", err)
	}
	service.ViewMu.RLock()
	records := service.components.tasks.CurrentTaskExecution().ContextCompactions
	service.ViewMu.RUnlock()
	if len(records) == 0 {
		t.Fatal("夹具应留下压缩记录（记录分支才有记录句式）")
	}
	record := records[len(records)-1]
	if record.MessageFrom == "" || record.EventFrom == 0 {
		t.Fatalf("夹具应带非空区间：%+v", record)
	}
	if record.MessagesBefore != 0 {
		t.Fatalf("夹具应是「引擎历史为空」场景（旧句式会印出压缩前 0 条消息）：%+v", record)
	}
	if strings.Contains(result.Notice, "压缩前") || strings.Contains(result.Notice, "条消息") {
		t.Fatalf("提示不得再用 messages_before 当消息条数：%q", result.Notice)
	}
	for _, want := range []string{
		"已压缩上下文：v", record.MessageFrom, record.MessageTo,
		fmt.Sprintf("事件 %d..%d", record.EventFrom, record.EventTo),
		"装配后估算 ", "read_tool_result / read_compressed_turn / search_history",
	} {
		if !strings.Contains(result.Notice, want) {
			t.Fatalf("提示缺少 %q：%q", want, result.Notice)
		}
	}
}

// TestCompactionRangeLabel：区间渲染只在**有边界**时成段——空区间返回空串
// （调用方据此跳过），单号不写 `..`，只有事件序号时只报事件。
func TestCompactionRangeLabel(t *testing.T) {
	cases := []struct {
		name string
		in   ContextCompactionResult
		want string
	}{
		{name: "两端", in: ContextCompactionResult{MessageFrom: "message-1", MessageTo: "message-8", EventFrom: 1, EventTo: 12}, want: "消息 message-1..message-8 / 事件 1..12"},
		{name: "单号", in: ContextCompactionResult{MessageFrom: "message-3", MessageTo: "message-3", EventFrom: 7, EventTo: 7}, want: "消息 message-3 / 事件 7..7"},
		{name: "只有事件", in: ContextCompactionResult{EventFrom: 2, EventTo: 5}, want: "事件 2..5"},
		{name: "只有消息", in: ContextCompactionResult{MessageFrom: "message-4"}, want: "消息 message-4"},
		{name: "空区间", in: ContextCompactionResult{}, want: ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := compactionRangeLabel(testCase.in); got != testCase.want {
				t.Fatalf("compactionRangeLabel = %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestCompactCommandNeverReportsFoldWithoutRecord：/compact 是显式路径，只要
// 折叠真的发生就必然落记录（含回合已收尾的 explicit_after_turn），因此回执里
// 不得再出现"不留记录"的说法——用户 2026-09-23 看到的那句「该回合的任务执行已
// 收尾…故本次不留记录」必须消失。
//
// 该分支的判别力在于：Reason/区间/帧引用都非零（记录句式才成立），若命令又
// 回落到"没有记录"的分支，就说明门槛或来源标记被改回去了。
func TestCompactCommandNeverReportsFoldWithoutRecord(t *testing.T) {
	service, _, _ := compactTestService(t, "task-command-after-turn")
	service.ViewMu.Lock()
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
		TaskID: "task-command-after-turn", MessageID: "message-1", Role: "assistant", Content: strings.Repeat("C", 4_000),
	})
	service.components.tasks.CurrentTaskExecution().Status = task_context.StatusCompleted
	service.ViewMu.Unlock()

	command, ok := service.commands.Get("compact")
	if !ok {
		t.Fatal("/compact 未注册")
	}
	result, err := command.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("/compact: %v", err)
	}
	if strings.Contains(result.Notice, "不留记录") || strings.Contains(result.Notice, "已收尾") {
		t.Fatalf("显式路径折叠必留记录，不得再说不留记录：%q", result.Notice)
	}
	if !strings.Contains(result.Notice, "已压缩上下文：") {
		t.Fatalf("回执应按记录成句：%q", result.Notice)
	}
	if !strings.Contains(result.Notice, "explicit_after_turn") {
		t.Fatalf("回执应说明这次压缩的来源（回合之间显式要求）：%q", result.Notice)
	}
	if !strings.Contains(result.Notice, "帧正文 ref ") {
		t.Fatalf("回执应带上帧正文引用（前端据此展开正文）：%q", result.Notice)
	}
}
