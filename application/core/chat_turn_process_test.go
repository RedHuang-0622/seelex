package core

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

func TestServiceComponentsAssembleConversationTurnProcess(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("conversationTurnProcess")
	if !ok {
		t.Fatal("serviceComponents 未装配 conversationTurnProcess")
	}
	if field.Type != reflect.TypeOf((*conversationTurnProcess)(nil)) {
		t.Fatalf("conversationTurnProcess 字段类型 = %s", field.Type)
	}
}

func TestChatTurnStartRulesAreCentralized(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "chat.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	banned := map[string]bool{
		"StartReActBudgetForLocked":   true,
		"BeginTaskFor":                true,
		"ActivateTaskSkillsLocked":    true,
		"AppendTranscriptEventLocked": true,
	}
	ast.Inspect(parsed, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if ok && banned[selector.Sel.Name] {
			t.Errorf("chat.go 仍直接调用 %s；ChatTurn 起点规则应集中在 chat_turn_process.go", selector.Sel.Name)
		}
		return true
	})
}

func TestChatTurnPersistenceRulesAreCentralized(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("conversationTurnCompletion")
	if !ok {
		t.Fatal("serviceComponents 未装配 conversationTurnCompletion")
	}
	if field.Type != reflect.TypeOf((*conversationTurnCompletion)(nil)) {
		t.Fatalf("conversationTurnCompletion 字段类型 = %s", field.Type)
	}

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "chat.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	banned := map[string]bool{
		"PersistCurrentSession":     true,
		"QueueFailConsumedInput":    true,
		"QueueConfirmConsumedInput": true,
		"ReleaseWorkingHistoryFor":  true,
	}
	ast.Inspect(parsed, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if ok && banned[selector.Sel.Name] {
			t.Errorf("chat.go 仍直接调用 %s；ChatTurn 持久化完成规则应集中在 chat_turn_completion.go", selector.Sel.Name)
		}
		return true
	})
}

func TestChatTurnQueuePromotionRulesAreCentralized(t *testing.T) {
	processType := reflect.TypeOf((*conversationTurnProcess)(nil))
	if _, ok := processType.MethodByName("PromoteLocked"); !ok {
		t.Fatal("conversationTurnProcess 缺少 PromoteLocked")
	}

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "chat.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	banned := map[string]bool{
		"QueuedRequests":     true,
		"Combine":            true,
		"ProjectQueueLocked": true,
	}
	ast.Inspect(parsed, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if ok && banned[selector.Sel.Name] {
			t.Errorf("chat.go 仍直接调用 %s；ChatTurn 队列提升规则应集中在 chat_turn_process.go", selector.Sel.Name)
		}
		return true
	})
}

type queuePromotionTestUnit struct {
	chat     model.ChatState
	requests []sessioncase.QueuedRequest
	cancel   context.CancelFunc
}

func (unit *queuePromotionTestUnit) ChatState() model.ChatState {
	return unit.chat
}

func (unit *queuePromotionTestUnit) SetChatState(chat model.ChatState, _ *sessioncase.View) {
	unit.chat = chat
}

func (unit *queuePromotionTestUnit) UpdateChat(update func(*model.ChatState), _ *sessioncase.View) {
	update(&unit.chat)
}

func (unit *queuePromotionTestUnit) QueueProjection() ([]string, int) {
	return nil, len(unit.requests)
}

func (unit *queuePromotionTestUnit) SetCancel(cancel context.CancelFunc) {
	unit.cancel = cancel
}

func (unit *queuePromotionTestUnit) SetStream(sessioncase.VisibleOutputSink) {}

func (unit *queuePromotionTestUnit) PendingRequests() []sessioncase.QueuedRequest {
	return unit.requests
}

func (unit *queuePromotionTestUnit) SetRequests(requests []sessioncase.QueuedRequest) {
	unit.requests = requests
}

func TestConversationTurnProcessFinishesWhenQueueIsEmpty(t *testing.T) {
	idleCalls := 0
	process := newConversationTurnProcess(conversationTurnProcessDeps{
		TurnState:      newConversationTurnState(),
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
	process := newConversationTurnProcess(conversationTurnProcessDeps{
		TurnState:      newConversationTurnState(),
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
