package assembly

import (
	"errors"
	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	"github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestChatTurnOutcomeRulesAreCentralized(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("conversationTurnOutcome")
	if !ok {
		t.Fatal("serviceComponents 未装配 conversationTurnOutcome")
	}
	if field.Type != reflect.TypeOf((*applicationconversation.TurnOutcomeApplication)(nil)) {
		t.Fatalf("conversationTurnOutcome 字段类型 = %s", field.Type)
	}
	outcomeType := reflect.TypeOf((*applicationconversation.TurnOutcomeApplication)(nil))
	if _, ok := outcomeType.MethodByName("ApplyLocked"); !ok {
		t.Fatal("conversationTurnOutcome 缺少 ApplyLocked")
	}
	if _, ok := outcomeType.MethodByName("MirrorLocked"); !ok {
		t.Fatal("conversationTurnOutcome 缺少 MirrorLocked")
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
		"ClearErrorLocked":                true,
		"SetErrorLocked":                  true,
		"applyRuntimeProjectionForLocked": true,
		"setSessionChatLockedFor":         true,
		"VisibleTaskStateFor":             true,
		"bumpLocked":                      true,
	}
	foundRunChat := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		declaration, ok := node.(*ast.FuncDecl)
		if !ok || declaration.Recv == nil || declaration.Name.Name != "runChat" {
			return true
		}
		foundRunChat = true
		ast.Inspect(declaration.Body, func(inner ast.Node) bool {
			selector, ok := inner.(*ast.SelectorExpr)
			if ok && banned[selector.Sel.Name] {
				t.Errorf("runChat 仍直接调用 %s；ChatTurn 终态结果投影应集中在 chat_turn_outcome.go", selector.Sel.Name)
			}
			return true
		})
		return false
	})
	if foundRunChat {
		t.Fatal("core runChat forwarder must be removed")
	}
}

func TestConversationTurnOutcomeAppliesCurrentResult(t *testing.T) {
	var calls []string
	outcome := applicationconversation.NewTurnOutcome(applicationconversation.TurnOutcomeDeps{
		ClearError: func(unit conversationChatStateUnit) {
			calls = append(calls, "clear")
			unit.UpdateChat(func(chat *model.ChatState) { chat.Error = "" }, nil)
		},
		SetError: func(conversationChatStateUnit, string) {
			t.Fatal("成功回合不得写入错误")
		},
		AppendErrorLine: func(string, string) {
			t.Fatal("成功回合不得追加错误行")
		},
		ApplyRuntimeProjection: func(sessionID string, _ view_state.RuntimeStateProjection) {
			calls = append(calls, "project:"+sessionID)
		},
	})
	unit := &queuePromotionTestUnit{
		chat: model.ChatState{Running: true, RequestID: "turn-a", Error: "stale"},
	}

	visibleError, stale := outcome.ApplyLocked("session-a", "turn-a", unit, view_state.RuntimeStateProjection{}, nil)

	if stale {
		t.Fatal("当前 request 不得判为 stale")
	}
	if visibleError != "" {
		t.Fatalf("visibleError = %q, want empty", visibleError)
	}
	if unit.chat.Error != "" {
		t.Fatalf("当前回合成功后仍保留旧错误 %q", unit.chat.Error)
	}
	if got, want := len(calls), 2; got != want || calls[0] != "clear" || calls[1] != "project:session-a" {
		t.Fatalf("calls = %v, want [clear project:session-a]", calls)
	}
}

func TestConversationTurnOutcomeRecordsErrorAndProjection(t *testing.T) {
	turnErr := errors.New("provider unavailable")
	var appended string
	outcome := applicationconversation.NewTurnOutcome(applicationconversation.TurnOutcomeDeps{
		ClearError: func(conversationChatStateUnit) {},
		SetError: func(unit conversationChatStateUnit, visibleError string) {
			unit.UpdateChat(func(chat *model.ChatState) { chat.Error = visibleError }, nil)
		},
		AppendErrorLine: func(sessionID, visibleError string) {
			appended = sessionID + ":" + visibleError
		},
		ApplyRuntimeProjection: func(string, view_state.RuntimeStateProjection) {},
	})
	unit := &queuePromotionTestUnit{
		chat: model.ChatState{Running: true, RequestID: "turn-a"},
	}

	visibleError, stale := outcome.ApplyLocked("session-a", "turn-a", unit, view_state.RuntimeStateProjection{}, turnErr)

	if stale || visibleError != turnErr.Error() || unit.chat.Error != turnErr.Error() {
		t.Fatalf("error projection = (%q, stale=%v, chat=%q), want original error", visibleError, stale, unit.chat.Error)
	}
	if appended != "session-a:"+turnErr.Error() {
		t.Fatalf("appended error = %q", appended)
	}
}

func TestConversationTurnOutcomeRejectsSupersededRequest(t *testing.T) {
	outcome := applicationconversation.NewTurnOutcome(applicationconversation.TurnOutcomeDeps{
		ClearError: func(conversationChatStateUnit) {
			t.Fatal("stale request 不得清理当前状态")
		},
		SetError: func(conversationChatStateUnit, string) {
			t.Fatal("stale request 不得写错误")
		},
		AppendErrorLine: func(string, string) {
			t.Fatal("stale request 不得追加错误行")
		},
		ApplyRuntimeProjection: func(string, view_state.RuntimeStateProjection) {
			t.Fatal("stale request 不得覆盖 runtime projection")
		},
	})
	unit := &queuePromotionTestUnit{
		chat: model.ChatState{Running: true, RequestID: "turn-new"},
	}

	visibleError, stale := outcome.ApplyLocked(
		"session-a", "turn-old", unit, view_state.RuntimeStateProjection{}, errors.New("old failure"),
	)

	if !stale || visibleError != "" {
		t.Fatalf("ApplyLocked = (%q, stale=%v), want empty and stale", visibleError, stale)
	}
}

func TestConversationTurnOutcomeMirrorsActiveSessionAndRevision(t *testing.T) {
	var calls []string
	outcome := applicationconversation.NewTurnOutcome(applicationconversation.TurnOutcomeDeps{
		SetSessionChat: func(sessionID string, chat model.ChatState) {
			calls = append(calls, "chat:"+sessionID+":"+chat.RequestID)
		},
		MirrorTask: func(sessionID string) {
			calls = append(calls, "task:"+sessionID)
		},
		BumpRevision: func() uint64 {
			calls = append(calls, "revision")
			return 11
		},
	})
	unit := &queuePromotionTestUnit{
		chat: model.ChatState{Running: false, RequestID: "turn-a"},
	}

	revision := outcome.MirrorLocked("session-a", unit, true)

	if revision != 11 {
		t.Fatalf("revision = %d, want 11", revision)
	}
	want := []string{"chat:session-a:turn-a", "task:session-a", "revision"}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestConversationTurnOutcomeDoesNotMirrorBackgroundTask(t *testing.T) {
	var calls []string
	outcome := applicationconversation.NewTurnOutcome(applicationconversation.TurnOutcomeDeps{
		SetSessionChat: func(sessionID string, _ model.ChatState) {
			calls = append(calls, "chat:"+sessionID)
		},
		MirrorTask: func(sessionID string) {
			t.Fatalf("后台会话不得镜像共享 Snapshot.Task: %s", sessionID)
		},
		BumpRevision: func() uint64 {
			t.Fatal("后台会话不得推进进程 revision")
			return 0
		},
	})
	unit := &queuePromotionTestUnit{
		chat: model.ChatState{RequestID: "turn-a"},
	}

	revision := outcome.MirrorLocked("session-b", unit, false)

	if revision != 0 {
		t.Fatalf("后台会话 revision = %d, want 0", revision)
	}
	if got, want := strings.Join(calls, ","), "chat:session-b"; got != want {
		t.Fatalf("calls = %q, want %q", got, want)
	}
}
