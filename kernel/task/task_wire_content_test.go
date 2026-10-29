package task_context

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// TestRecordUserWireContent 守住 I1（字节冻结）在 kernel 层的落点：
// 装配拼进当前输入的动态尾块必须作为「已发出字节」写回 user 事件，
// 否则下一轮重投影会自该消息起分叉。
func TestRecordUserWireContent(t *testing.T) {
	const (
		sessionID = "session-wire"
		requestID = "req-wire-1"
		rawInput  = "第 1 轮：核对前缀。"
	)
	wire := rawInput + "\n\n【工作打点表】\n- todo:1 running 核对前缀失效区间"

	coordinator := providerContentCoordinator()
	coordinator.AppendTranscriptEventForLocked(sessionID, model.TranscriptEvent{
		TaskID: requestID, Role: "user", Content: rawInput,
	})

	if !coordinator.RecordUserWireContentLocked(sessionID, requestID, wire) {
		t.Fatalf("应命中该轮的 user 事件")
	}
	event := userEventFor(t, coordinator, sessionID, requestID)
	if event.Content != rawInput {
		t.Fatalf("视图正文被改写：%q, want %q（视图仍应读原始输入）", event.Content, rawInput)
	}
	if event.ProviderContent != wire {
		t.Fatalf("ProviderContent=%q, want 已发出字节 %q", event.ProviderContent, wire)
	}
	if projected := providerContentForEvent(event); projected != wire {
		t.Fatalf("重投影=%q, want %q", projected, wire)
	}

	// 幂等：重复记录同一份字节不改变结果。
	if !coordinator.RecordUserWireContentLocked(sessionID, requestID, wire) {
		t.Fatalf("重复记录应仍命中")
	}
	if again := userEventFor(t, coordinator, sessionID, requestID); again.ProviderContent != wire {
		t.Fatalf("幂等性被破坏：%q", again.ProviderContent)
	}

	// wire 与原始输入相同时清空 ProviderContent（记录保持紧凑，语义等价）。
	if !coordinator.RecordUserWireContentLocked(sessionID, requestID, rawInput) {
		t.Fatalf("同值写入应命中")
	}
	if same := userEventFor(t, coordinator, sessionID, requestID); same.ProviderContent != "" {
		t.Fatalf("同值时应清空 ProviderContent，实际 %q", same.ProviderContent)
	}
	if projected := providerContentForEvent(userEventFor(t, coordinator, sessionID, requestID)); projected != rawInput {
		t.Fatalf("同值重投影=%q, want %q", projected, rawInput)
	}
}

// TestRecordUserWireContentScopedToRequest 守住作用域：多轮共存时只改本轮
// 那条 user 行，绝不误伤其他轮的输入（那会制造新的分叉）。
func TestRecordUserWireContentScopedToRequest(t *testing.T) {
	const sessionID = "session-scope"
	coordinator := providerContentCoordinator()
	coordinator.AppendTranscriptEventForLocked(sessionID, model.TranscriptEvent{
		TaskID: "req-1", Role: "user", Content: "第一轮输入",
	})
	coordinator.AppendTranscriptEventForLocked(sessionID, model.TranscriptEvent{
		TaskID: "req-1", Role: "assistant", Content: "第一轮答复",
	})
	coordinator.AppendTranscriptEventForLocked(sessionID, model.TranscriptEvent{
		TaskID: "req-2", Role: "user", Content: "第二轮输入",
	})

	if !coordinator.RecordUserWireContentLocked(sessionID, "req-2", "第二轮输入+动态块") {
		t.Fatalf("应命中 req-2")
	}
	if got := userEventFor(t, coordinator, sessionID, "req-1").ProviderContent; got != "" {
		t.Fatalf("req-1 的 user 行被误改：%q", got)
	}
	if got := userEventFor(t, coordinator, sessionID, "req-2").ProviderContent; got != "第二轮输入+动态块" {
		t.Fatalf("req-2 ProviderContent=%q", got)
	}

	// 未知请求：返回 false 且不改任何行。
	if coordinator.RecordUserWireContentLocked(sessionID, "req-missing", "x") {
		t.Fatalf("未知请求不应命中")
	}
	// 空 wire：直接拒绝。
	if coordinator.RecordUserWireContentLocked(sessionID, "req-2", "") {
		t.Fatalf("空 wire 不应写入")
	}
}

// userEventFor 返回指定请求最新的一条 user 事件。
func userEventFor(t *testing.T, coordinator *Coordinator, sessionID, requestID string) model.TranscriptEvent {
	t.Helper()
	events := coordinator.TranscriptFor(sessionID)
	for index := len(events) - 1; index >= 0; index-- {
		event := events[index]
		if event.Role == "user" && event.TaskID == requestID {
			return event
		}
	}
	t.Fatalf("no user event for session=%q request=%q: %+v", sessionID, requestID, events)
	return model.TranscriptEvent{}
}
