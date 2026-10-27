package conversation_test

import (
	"errors"
	"strings"
	"testing"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	viewstate "github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

func TestConversationTurnOutcomeAppliesCurrentResult(t *testing.T) {
	var calls []string
	outcome := applicationconversation.NewTurnOutcome(applicationconversation.TurnOutcomeDeps{
		ClearError: func(unit applicationconversation.ChatStateUnit) {
			calls = append(calls, "clear")
			unit.UpdateChat(func(chat *model.ChatState) { chat.Error = "" }, nil)
		},
		SetError: func(applicationconversation.ChatStateUnit, string) {
			t.Fatal("成功回合不得写入错误")
		},
		AppendErrorLine: func(string, string) {
			t.Fatal("成功回合不得追加错误行")
		},
		ApplyRuntimeProjection: func(sessionID string, _ viewstate.RuntimeStateProjection) {
			calls = append(calls, "project:"+sessionID)
		},
	})
	unit := &queuePromotionTestUnit{
		chat: model.ChatState{Running: true, RequestID: "turn-a", Error: "stale"},
	}

	visibleError, stale := outcome.ApplyLocked("session-a", "turn-a", unit, viewstate.RuntimeStateProjection{}, nil)

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
		ClearError: func(applicationconversation.ChatStateUnit) {},
		SetError: func(unit applicationconversation.ChatStateUnit, visibleError string) {
			unit.UpdateChat(func(chat *model.ChatState) { chat.Error = visibleError }, nil)
		},
		AppendErrorLine: func(sessionID, visibleError string) {
			appended = sessionID + ":" + visibleError
		},
		ApplyRuntimeProjection: func(string, viewstate.RuntimeStateProjection) {},
	})
	unit := &queuePromotionTestUnit{
		chat: model.ChatState{Running: true, RequestID: "turn-a"},
	}

	visibleError, stale := outcome.ApplyLocked("session-a", "turn-a", unit, viewstate.RuntimeStateProjection{}, turnErr)

	if stale || visibleError != turnErr.Error() || unit.chat.Error != turnErr.Error() {
		t.Fatalf("error projection = (%q, stale=%v, chat=%q), want original error", visibleError, stale, unit.chat.Error)
	}
	if appended != "session-a:"+turnErr.Error() {
		t.Fatalf("appended error = %q", appended)
	}
}

func TestConversationTurnOutcomeRejectsSupersededRequest(t *testing.T) {
	outcome := applicationconversation.NewTurnOutcome(applicationconversation.TurnOutcomeDeps{
		ClearError: func(applicationconversation.ChatStateUnit) {
			t.Fatal("stale request 不得清理当前状态")
		},
		SetError: func(applicationconversation.ChatStateUnit, string) {
			t.Fatal("stale request 不得写错误")
		},
		AppendErrorLine: func(string, string) {
			t.Fatal("stale request 不得追加错误行")
		},
		ApplyRuntimeProjection: func(string, viewstate.RuntimeStateProjection) {
			t.Fatal("stale request 不得覆盖 runtime projection")
		},
	})
	unit := &queuePromotionTestUnit{
		chat: model.ChatState{Running: true, RequestID: "turn-new"},
	}

	visibleError, stale := outcome.ApplyLocked(
		"session-a", "turn-old", unit, viewstate.RuntimeStateProjection{}, errors.New("old failure"),
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
