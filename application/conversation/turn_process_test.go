package conversation_test

import (
	"testing"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

type turnStateTestAdapter struct {
	*applicationconversation.TurnStateApplication
}

func (*turnStateTestAdapter) QueuedRequests([]sessioncase.QueuedRequest) []applicationconversation.Request {
	return nil
}

func (*turnStateTestAdapter) Combine([]applicationconversation.Request) applicationconversation.Request {
	return applicationconversation.Request{}
}

func TestConversationTurnProcessFinishesWhenQueueIsEmpty(t *testing.T) {
	idleCalls := 0
	process := applicationconversation.NewTurnProcess(applicationconversation.TurnProcessDeps{
		TurnState:      &turnStateTestAdapter{TurnStateApplication: applicationconversation.NewTurnState()},
		AnyChatRunning: func() bool { return false },
		MarkIdle:       func() { idleCalls++ },
	})
	unit := &queuePromotionTestUnit{
		chat:   model.ChatState{Running: true, RequestID: "turn-a"},
		cancel: func() {},
	}

	promotion := process.PromoteLocked("session-a", unit)

	if promotion.Promoted {
		t.Fatal("空队列不应提升下一回合")
	}
	if unit.chat.Running {
		t.Fatal("空队列时当前 ChatState 应结束 running")
	}
	if unit.cancel != nil {
		t.Fatal("空队列时当前 turn cancel 应清空")
	}
	if idleCalls != 1 {
		t.Fatalf("mark idle 调用次数 = %d, want 1", idleCalls)
	}
}

func TestConversationTurnProcessKeepsProcessBusyWhenOtherChatRuns(t *testing.T) {
	idleCalls := 0
	process := applicationconversation.NewTurnProcess(applicationconversation.TurnProcessDeps{
		TurnState:      &turnStateTestAdapter{TurnStateApplication: applicationconversation.NewTurnState()},
		AnyChatRunning: func() bool { return true },
		MarkIdle:       func() { idleCalls++ },
	})
	unit := &queuePromotionTestUnit{
		chat:   model.ChatState{Running: true, RequestID: "turn-a"},
		cancel: func() {},
	}

	process.PromoteLocked("session-a", unit)

	if idleCalls != 0 {
		t.Fatalf("其它会话仍运行时 mark idle 调用次数 = %d, want 0", idleCalls)
	}
}
