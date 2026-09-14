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

// TestCompactContextBelowThresholdIsHonestNoOp：未达压缩阈值时不伪造压缩、
// 不做状态改写，只如实报告当前估算与阈值（调用方据此提示，而不是报故障）。
func TestCompactContextBelowThresholdIsHonestNoOp(t *testing.T) {
	service, _, sessionID := compactTestService(t, "task-compact-2")
	service.ViewMu.Lock()
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
		TaskID: "task-compact", Role: "user", Content: "small question",
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
	if result.Compacted {
		t.Fatalf("未达阈值不应产生压缩：%+v", result)
	}
	if !strings.Contains(result.Note, "未达压缩阈值") || result.EstimatedTokens == 0 {
		t.Fatalf("结果应说明估算与阈值：%+v", result)
	}
	service.ViewMu.RLock()
	compactions := service.components.tasks.CurrentTaskExecution().ContextCompactions
	service.ViewMu.RUnlock()
	if len(compactions) != 0 {
		t.Fatalf("未达阈值不应留下压缩记录：%#v", compactions)
	}
}

// TestCompactContextWithoutTaskExecution：会话没有任务执行纪元（例如刚启动
// 还没发过消息）时不伪造纪元，返回明确提示而不是错误。
func TestCompactContextWithoutTaskExecution(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	result, err := service.CompactContextNow(task_context.WithSessionID(context.Background(), service.Snapshot().Session.ID))
	if err != nil {
		t.Fatalf("CompactContextNow: %v", err)
	}
	if result.Compacted || !strings.Contains(result.Note, "没有进行中的任务执行") {
		t.Fatalf("结果 = %+v", result)
	}
}

// TestCompactCommandRegisteredAndSharesPath：/compact 命令注册成功，且与工具
// 走同一条落点——未达阈值时给同样的"无需压缩"提示。
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
	if !strings.Contains(result.Notice, "未达压缩阈值") {
		t.Fatalf("/compact 提示 = %q", result.Notice)
	}
}
