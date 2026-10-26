package core

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	session_runtime "github.com/RedHuang-0622/seelex/backend/engine/session"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	sessioncase "github.com/RedHuang-0622/seelex/middle/switch/sessionusecase"
	view_state "github.com/RedHuang-0622/seelex/middle/view/state"
)

func TestChatTurnCommitRulesAreCentralized(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("conversationTurnCommit")
	if !ok {
		t.Fatal("serviceComponents 未装配 conversationTurnCommit")
	}
	if field.Type != reflect.TypeOf((*conversationTurnCommit)(nil)) {
		t.Fatalf("conversationTurnCommit 字段类型 = %s", field.Type)
	}
	commitType := reflect.TypeOf((*conversationTurnCommit)(nil))
	if _, ok := commitType.MethodByName("Commit"); !ok {
		t.Fatal("conversationTurnCommit 缺少 Commit")
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
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name.Name != "runChat" {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok && (selector.Sel.Name == "Persist" ||
				selector.Sel.Name == "ApplyLocked" ||
				selector.Sel.Name == "PromoteLocked" ||
				selector.Sel.Name == "MirrorLocked" ||
				selector.Sel.Name == "appendMessageWithOriginLocked" ||
				selector.Sel.Name == "appendSessionMessageWithOriginLocked") {
				t.Errorf("runChat 仍直接调用 %s", selector.Sel.Name)
			}
			return true
		})
		return
	}
}

func TestConversationTurnCommitRunsPersistenceOutcomePromotion(t *testing.T) {
	unit, err := sessioncase.NewSessionUnit("session-a")
	if err != nil {
		t.Fatal(err)
	}
	locker := &recordingLocker{}
	location := session_runtime.Location{WorkspaceID: "workspace-a"}
	turnErr := errors.New("turn failed")
	promotion := chatTurnPromotion{
		Promoted:  true,
		RequestID: "turn-b",
		Request:   chatRequest{DisplayInput: "next"},
		UserEvent: model.TranscriptEvent{RoleName: "user", RoleSessionID: "session-a", RoundID: 2},
	}
	nextUser := &model.Message{ID: "user-b"}
	nextAssistant := &model.Message{ID: "assistant-b"}
	var calls []string
	commit := newConversationTurnCommit(conversationTurnCommitDeps{
		Persist: func(sessionID, requestID string, gotErr error) (session_runtime.Location, error) {
			calls = append(calls, "persist")
			if gotErr != turnErr {
				t.Fatalf("persist err = %v", gotErr)
			}
			return location, turnErr
		},
		Lock:     func() sync.Locker { return locker },
		Unit:     func(sessionID string) *sessioncase.SessionUnit { return unit },
		IsActive: func(sessionID string) bool { return true },
		ApplyOutcome: func(sessionID, requestID string, gotUnit *sessioncase.SessionUnit, projection view_state.RuntimeStateProjection, gotErr error) (string, bool) {
			calls = append(calls, "outcome")
			return "visible", false
		},
		Promote: func(sessionID string, gotUnit *sessioncase.SessionUnit) chatTurnPromotion {
			calls = append(calls, "promote")
			return promotion
		},
		AppendNext: func(sessionID string, active bool, got chatTurnPromotion) (*model.Message, *model.Message) {
			calls = append(calls, "append")
			if !active || got.RequestID != promotion.RequestID {
				t.Fatalf("append args = %v/%+v", active, got)
			}
			return nextUser, nextAssistant
		},
		Mirror: func(sessionID string, gotUnit *sessioncase.SessionUnit, active bool) uint64 {
			calls = append(calls, "mirror")
			return 9
		},
		Stale: func(string, string, *sessioncase.SessionUnit) {
			t.Fatal("stale callback must not run")
		},
	})

	got := commit.Commit("session-a", "turn-a", turnErr, view_state.RuntimeStateProjection{})

	if got.Location != location || got.TurnErr != turnErr || got.VisibleError != "visible" ||
		got.Revision != 9 || got.Promotion.RequestID != "turn-b" ||
		got.NextUser != nextUser || got.NextAssistant != nextAssistant || got.Stale {
		t.Fatalf("commit result = %+v", got)
	}
	want := "persist,outcome,promote,append,mirror"
	if strings.Join(calls, ",") != want {
		t.Fatalf("calls = %v, want %s", calls, want)
	}
	if locker.locked != 0 {
		t.Fatalf("lock leaked: %d", locker.locked)
	}
}

func TestConversationTurnCommitStopsOnStaleRequest(t *testing.T) {
	unit, err := sessioncase.NewSessionUnit("session-a")
	if err != nil {
		t.Fatal(err)
	}
	locker := &recordingLocker{}
	var staleCalled bool
	commit := newConversationTurnCommit(conversationTurnCommitDeps{
		Persist: func(string, string, error) (session_runtime.Location, error) {
			return session_runtime.Location{}, nil
		},
		Lock:     func() sync.Locker { return locker },
		Unit:     func(string) *sessioncase.SessionUnit { return unit },
		IsActive: func(string) bool { return false },
		ApplyOutcome: func(string, string, *sessioncase.SessionUnit, view_state.RuntimeStateProjection, error) (string, bool) {
			return "", true
		},
		Promote: func(string, *sessioncase.SessionUnit) chatTurnPromotion {
			t.Fatal("stale request must not promote")
			return chatTurnPromotion{}
		},
		AppendNext: func(string, bool, chatTurnPromotion) (*model.Message, *model.Message) {
			t.Fatal("stale request must not append")
			return nil, nil
		},
		Mirror: func(string, *sessioncase.SessionUnit, bool) uint64 {
			t.Fatal("stale request must not mirror")
			return 0
		},
		Stale: func(string, string, *sessioncase.SessionUnit) { staleCalled = true },
	})

	got := commit.Commit("session-a", "turn-a", nil, view_state.RuntimeStateProjection{})

	if !got.Stale || !staleCalled || locker.locked != 0 {
		t.Fatalf("commit = %+v staleCalled=%v locks=%d", got, staleCalled, locker.locked)
	}
}
