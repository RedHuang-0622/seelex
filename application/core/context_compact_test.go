package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/task_context"
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

// TestCompactManualReportsFoldWithoutRecord：折叠发生了、记录却没产生时，
// 结果面必须如实说明（回合已收尾 → 压缩记录只在执行中写），而不是谎报
// 「未达压缩阈值」。这正是"上下文 129k 却被告知无需压缩"的另一种来路。
func TestCompactManualReportsFoldWithoutRecord(t *testing.T) {
	service, _, sessionID := compactTestService(t, "task-finished")
	service.ViewMu.Lock()
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
		TaskID: "task-finished", Role: "assistant", Content: strings.Repeat("B", 4_000),
	})
	// 回合收尾：任务执行不再是 Running → RecordContextCompactionLocked 拒绝写记录。
	service.components.tasks.CurrentTaskExecution().Status = task_context.StatusCompleted
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
	if result.Recorded {
		t.Fatalf("回合已收尾不应产生压缩记录：%+v", result)
	}
	if strings.Contains(result.Note, "未达压缩阈值") || strings.Contains(result.Note, "无需压缩") {
		t.Fatalf("折叠已发生，结果面不得声称未压缩：%q", result.Note)
	}
	if !strings.Contains(result.Note, "已收尾") {
		t.Fatalf("结果面应说明为何没有压缩记录：%q", result.Note)
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
