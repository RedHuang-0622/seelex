package core

import (
	"fmt"
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/task_context"
)

// 恢复顺序回归：存储/会话顺序一致是为了保留稳定的上下文前缀，让模型缓存
// 命中。运行中（未压缩）引擎历史 = system + 全量已定稿轮次（append-only）；
// 冷恢复只装载尾部窗口后，首个请求必须重建出与会话顺序一致的完整前缀，
// 不能把尾部保留段当作“已覆盖前缀”跳过后再把中段事件追加到尾部之后。

func TestRunningSessionAccumulatesAllRoundsInOrderControl(t *testing.T) {
	engine := &fakeEngine{}
	service := newTestService(t, engine)
	defer service.Shutdown()

	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-order-1"}
	service.components.tasks.BeginTask("task-order-1", "inspect", "high", nil, TaskCheckpoint{})
	events := appendOrderedRoundsLocked(service, 12)
	service.ViewMu.Unlock()
	if len(events) != 24 {
		t.Fatalf("fixture events = %d, want 24", len(events))
	}

	if _, err := service.components.context.PrepareExecutionContext("task-order-1", "next"); err != nil {
		t.Fatal(err)
	}
	if got := orderRoundLabels(engine.History()); fmt.Sprint(got) != fmt.Sprint(expectedOrderRoundLabels(12)) {
		t.Fatalf("running-mode context order = %v, want original", got)
	}
}

func TestColdRestoreTailFirstRequestKeepsTranscriptOrder(t *testing.T) {
	engine := &fakeEngine{}
	service := newTestService(t, engine)
	defer service.Shutdown()

	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-order-2"}
	service.components.tasks.BeginTask("task-order-2", "inspect", "high", nil, TaskCheckpoint{})
	events := appendOrderedRoundsLocked(service, 12)
	service.ViewMu.Unlock()

	// 模拟 resumeSessionCold：引擎只装载尾部窗口（≤4 个完整协议单元），
	// 会话 transcript（内存全量）仍保有序。
	restored := task_context.TranscriptTailHistory(events, 1_000_000, 4)
	if err := engine.ReplaceHistory("session-1", restored); err != nil {
		t.Fatal(err)
	}

	// 恢复后首个请求：PrepareExecutionContextFor 增量装配必须识别“尾部
	// 窗口不是前缀”，从完整 transcript 按原序重建，而不是 [tail]+[middle]
	// 重排重复。
	if _, err := service.components.context.PrepareExecutionContext("task-order-2", "next"); err != nil {
		t.Fatal(err)
	}
	got := orderRoundLabels(engine.History())
	want := expectedOrderRoundLabels(12)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("after cold restore context order = %v, want original %v", got, want)
	}
}

func appendOrderedRoundsLocked(service *Service, rounds int) []TranscriptEvent {
	events := make([]TranscriptEvent, 0, rounds*2)
	for round := 0; round < rounds; round++ {
		user := TranscriptEvent{
			TaskID: "task-order", Role: "user",
			Content: fmt.Sprintf("request-%02d", round), TokenCount: 1,
		}
		assistant := TranscriptEvent{
			TaskID: "task-order", Role: "assistant",
			Content: fmt.Sprintf("answer-%02d", round), TokenCount: 1,
		}
		service.components.tasks.AppendTranscriptEventLocked(user)
		service.components.tasks.AppendTranscriptEventLocked(assistant)
		events = append(events, user, assistant)
	}
	return events
}

func orderRoundLabels(history []EngineMessage) []string {
	labels := make([]string, 0, len(history))
	for _, message := range history {
		round := -1
		kind := ""
		if _, err := fmt.Sscanf(message.Content, "request-%d", &round); err == nil {
			kind = "user"
		}
		if _, err := fmt.Sscanf(message.Content, "answer-%d", &round); err == nil {
			kind = "answer"
		}
		if round < 0 {
			labels = append(labels, "other:"+message.Content)
			continue
		}
		labels = append(labels, fmt.Sprintf("%s-%02d", kind, round))
	}
	return labels
}

func expectedOrderRoundLabels(rounds int) []string {
	labels := make([]string, 0, rounds*2)
	for round := 0; round < rounds; round++ {
		labels = append(labels,
			fmt.Sprintf("user-%02d", round),
			fmt.Sprintf("answer-%02d", round),
		)
	}
	return labels
}
