package core

import (
	"reflect"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	sessioncase "github.com/RedHuang-0622/seelex/middle/switch/sessionusecase"
)

func TestSessionRestoringShellIsAssembled(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("sessionRestoringShell")
	if !ok {
		t.Fatal("serviceComponents 未装配 sessionRestoringShell")
	}
	if field.Type != reflect.TypeOf((*sessionRestoringShell)(nil)) {
		t.Fatalf("sessionRestoringShell 字段类型 = %s", field.Type)
	}
	shellType := reflect.TypeOf((*sessionRestoringShell)(nil))
	if _, ok := shellType.MethodByName("PointLocked"); !ok {
		t.Fatal("sessionRestoringShell 缺少 PointLocked")
	}
}

func TestSessionRestoringShellClearsAllSessionFacts(t *testing.T) {
	unit, err := sessioncase.NewSessionUnit("session-b")
	if err != nil {
		t.Fatal(err)
	}
	unit.SetChatState(model.ChatState{Running: true, RequestID: "turn-b"}, nil)
	snapshot := model.Snapshot{
		Session: model.SessionState{ID: "session-a", Name: "old"},
		Task:    &model.TaskState{RequestID: "turn-a"},
		Runtime: model.RuntimeState{
			Plan:             &model.PlanState{},
			TodoItems:        []dto.TodoItem{{Text: "todo-1"}},
			SubAgentTree:     []dto.SubAgentTreeNode{{ID: "sub-1"}},
			WorkTable:        []model.WorkItem{{ID: "task-1"}},
			WorkTableBatches: []model.WorkTableBatch{{ID: "batch-1"}},
			TeamworkBoard:    &dto.TeamworkBoardView{},
		},
		Interaction: &model.Interaction{ID: "approval-1", SessionID: "session-a"},
	}
	var activeSession string
	var mirrored bool
	shell := newSessionRestoringShell(sessionRestoringShellDeps{
		Title: func(sessionID string) string {
			if sessionID != "session-b" {
				t.Fatalf("title session = %q", sessionID)
			}
			return "session b"
		},
		SetActive: func(sessionID string) { activeSession = sessionID },
		SetSessionChat: func(sessionID string, chat model.ChatState) {
			if sessionID != "session-b" || chat.RequestID != "turn-b" {
				t.Fatalf("SetSessionChat = %s/%+v", sessionID, chat)
			}
		},
		MirrorActiveView: func() { mirrored = true },
		BumpRevision:     func() uint64 { return 41 },
	})

	revision := shell.PointLocked("session-b", unit, &snapshot)

	if revision != 41 {
		t.Fatalf("revision = %d, want 41", revision)
	}
	if snapshot.Session.ID != "session-b" || snapshot.Session.Name != "session b" ||
		snapshot.Session.Status != SessionStatusRestoring {
		t.Fatalf("session shell = %+v", snapshot.Session)
	}
	if activeSession != "session-b" || !mirrored {
		t.Fatalf("active/mirrored = %q/%v", activeSession, mirrored)
	}
	if snapshot.Task != nil || snapshot.Runtime.Plan != nil || snapshot.Runtime.TodoItems != nil ||
		snapshot.Runtime.SubAgentTree != nil || snapshot.Runtime.WorkTable != nil ||
		snapshot.Runtime.WorkTableBatches != nil || snapshot.Runtime.TeamworkBoard != nil ||
		snapshot.Interaction != nil {
		t.Fatalf("restoring shell leaked previous session facts: %+v", snapshot)
	}
}
