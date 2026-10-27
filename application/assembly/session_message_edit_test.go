package assembly

import (
	"context"
	"errors"
	applicationedit "github.com/RedHuang-0622/seelex/application/conversation/edit"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/task"
	"github.com/RedHuang-0622/seelex/store"
	"strings"
	"testing"
)

func editForkFixture(t *testing.T) (*Service, *forkServiceSessions, *multiSessionEngine) {
	t.Helper()
	parent := model.SessionRecord{
		Version: 3,
		ID:      "parent",
		Title:   model.SessionTitle{Value: "Parent"},
		Conversation: model.ConversationRecord{Messages: []model.Message{
			{ID: "m1", Role: "user", Content: "old question"},
			{ID: "m2", Role: "assistant", Content: "old answer"},
			{ID: "m3", Role: "user", Content: "edit me"},
			{ID: "m4", Role: "assistant", Content: "discard me"},
			{ID: "m5", Role: "user", Content: "later question"},
			{ID: "m6", Role: "assistant", Content: "later answer"},
		}},
	}
	sessions := &forkServiceSessions{
		parent: parent,
		events: []store.Event{
			{Seq: 1, TaskID: "chat-1", Role: "user", Content: "old question", MessageID: "m1"},
			{Seq: 2, TaskID: "chat-1", Role: "assistant", Content: "old answer", MessageID: "m2"},
			{Seq: 3, TaskID: "chat-2", Role: "user", Content: "edit me", MessageID: "m3"},
			{Seq: 4, TaskID: "chat-2", Role: "assistant", Content: "discard me", MessageID: "m4"},
			{Seq: 5, TaskID: "chat-3", Role: "user", Content: "later question", MessageID: "m5"},
			{Seq: 6, TaskID: "chat-3", Role: "assistant", Content: "later answer", MessageID: "m6"},
		},
		context:    []byte(`{"schema_version":3}`),
		generation: "generation-parent",
	}
	engine := newMultiSessionEngine()
	service := newTestService(t, engine, withTestSessions(sessions))
	service.ViewMu.Lock()
	service.Core.Snapshot.Session = model.SessionState{ID: "parent"}
	_ = service.sessionUnitLocked("parent")
	service.ViewMu.Unlock()
	editForkRetainedWindow(t, service, 0)
	return service, sessions, engine
}

func editForkRetainedWindow(t *testing.T, service *Service, retainedFrom int) {
	t.Helper()
	transcript := []model.TranscriptEvent{
		{Seq: 1, Role: "user", Content: "old question"},
		{Seq: 2, Role: "assistant", Content: "old answer"},
		{Seq: 3, Role: "user", Content: "edit me"},
		{Seq: 4, Role: "assistant", Content: "discard me"},
		{Seq: 5, Role: "user", Content: "later question"},
		{Seq: 6, Role: "assistant", Content: "later answer"},
	}
	service.ViewMu.Lock()
	compactions := []model.ContextCompaction(nil)
	if retainedFrom > 0 {
		compactions = []model.ContextCompaction{{EventTo: uint64(retainedFrom)}}
	}
	service.components.tasks.RestoreSessionTaskLockedFor("parent", task_context.RestoredTaskState{
		Transcript:          transcript,
		TranscriptSeq:       6,
		ContextRetainedFrom: retainedFrom,
		ContextCompactions:  compactions,
	})
	service.ViewMu.Unlock()
}

func userTexts(messages []model.Message) []string {
	texts := make([]string, 0, len(messages))
	for _, message := range messages {
		if message.Role == "user" {
			texts = append(texts, message.Content)
		}
	}
	return texts
}

func TestEditUserMessageAndForkUsesPreviousParagraphAndSubmitsReplacement(t *testing.T) {
	service, sessions, engine := editForkFixture(t)
	parentBefore := append([]store.Event(nil), sessions.events...)

	result, err := service.components.editFork.application.EditUserMessageAndFork(context.Background(), "parent", model.EditUserMessageForkRequest{
		TargetEventSeq:   3,
		NewText:          "edited question",
		ExpectedRevision: service.components.frontendSnapshot.Snapshot().ConversationRevision,
		OperationID:      "edit-op-1",
	})
	if err != nil {
		t.Fatalf("EditUserMessageAndFork: %v", err)
	}
	if result.ForkPoint != 2 {
		t.Fatalf("fork point = %d, want previous paragraph end 2", result.ForkPoint)
	}
	engine.releaseSession(result.ChildSessionID)
	waitSessionIdle(t, service)

	if got := service.components.frontendSnapshot.Snapshot().Session.ID; got != result.ChildSessionID {
		t.Fatalf("active session = %q, want child %q", got, result.ChildSessionID)
	}
	snapshot := service.components.frontendSnapshot.Snapshot()
	if snapshot.Session.ForkedFrom == nil || snapshot.Session.ForkedFrom.ParentSessionID != "parent" {
		t.Fatalf("child snapshot lineage = %#v", snapshot.Session.ForkedFrom)
	}
	gotUsers := userTexts(snapshot.Conversation)
	if len(gotUsers) < 2 || gotUsers[0] != "old question" || gotUsers[1] != "edited question" {
		t.Fatalf("child user inputs = %#v, want inherited prefix then edited text", gotUsers)
	}
	for _, text := range gotUsers {
		if text == "edit me" || text == "discard me" || text == "later question" {
			t.Fatalf("discarded parent turn leaked into child: %#v", gotUsers)
		}
	}
	if sessions.savedRecord.ForkedFrom == nil {
		t.Fatal("child lineage is missing")
	}
	lineage := sessions.savedRecord.ForkedFrom
	if lineage.ForkPoint.EventSeq != 2 || lineage.SourceEventSeq != 3 || lineage.SourceMessageID != "m3" {
		t.Fatalf("lineage = %#v, want cut 2 sourced from event 3/m3", lineage)
	}
	if len(sessions.parent.Conversation.Messages) != 6 || sessions.parent.Conversation.Messages[2].Content != "edit me" {
		t.Fatalf("parent record changed: %#v", sessions.parent.Conversation.Messages)
	}
	for index := range parentBefore {
		if parentBefore[index].Seq != sessions.events[index].Seq || parentBefore[index].Content != sessions.events[index].Content {
			t.Fatalf("parent event stream changed at %d: before=%#v after=%#v", index, parentBefore[index], sessions.events[index])
		}
	}
}

func TestEditUserMessageAndForkCanReplaceLatestUserMessage(t *testing.T) {
	service, _, engine := editForkFixture(t)
	result, err := service.components.editFork.application.EditUserMessageAndFork(context.Background(), "parent", model.EditUserMessageForkRequest{
		TargetEventSeq: 5, NewText: "latest edited", ExpectedRevision: 6, OperationID: "latest-edit",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ForkPoint != 4 {
		t.Fatalf("latest fork point = %d, want 4", result.ForkPoint)
	}
	engine.releaseSession(result.ChildSessionID)
	waitSessionIdle(t, service)
	got := userTexts(service.components.frontendSnapshot.Snapshot().Conversation)
	if len(got) != 3 || got[2] != "latest edited" {
		t.Fatalf("latest child users = %#v", got)
	}
}

func TestEditUserMessageAndForkRejectsCompressedAndRunningParents(t *testing.T) {
	service, sessions, _ := editForkFixture(t)
	editForkRetainedWindow(t, service, 2)
	revision := service.components.frontendSnapshot.Snapshot().ConversationRevision

	_, err := service.components.editFork.application.EditUserMessageAndFork(context.Background(), "parent", model.EditUserMessageForkRequest{
		TargetEventSeq: 1, NewText: "cannot edit", ExpectedRevision: revision, OperationID: "compressed",
	})
	if err == nil || !strings.Contains(err.Error(), "压缩") {
		t.Fatalf("compressed edit error = %v, want explicit compressed-prefix rejection", err)
	}
	if sessions.savedChild != "" {
		t.Fatalf("compressed edit created child %q", sessions.savedChild)
	}

	service.ViewMu.Lock()
	unit := service.sessions.Unit("parent")
	if unit == nil {
		t.Fatalf("parent unit missing")
	}
	unit.SetChatState(model.ChatState{Running: true, RequestID: "running-parent"}, nil)
	service.ViewMu.Unlock()
	_, err = service.components.editFork.application.EditUserMessageAndFork(context.Background(), "parent", model.EditUserMessageForkRequest{
		TargetEventSeq: 3, NewText: "cannot edit", ExpectedRevision: revision, OperationID: "running",
	})
	if !errors.Is(err, ErrChatRunning) {
		t.Fatalf("running parent error = %v, want ErrChatRunning", err)
	}
}

func TestEditUserMessageAndForkRejectsStaleParentRevision(t *testing.T) {
	service, sessions, _ := editForkFixture(t)
	_, err := service.components.editFork.application.EditUserMessageAndFork(context.Background(), "parent", model.EditUserMessageForkRequest{
		TargetEventSeq: 3, NewText: "stale", ExpectedRevision: 5, OperationID: "stale-revision",
	})
	if err == nil || !strings.Contains(err.Error(), "已推进") {
		t.Fatalf("stale revision error = %v", err)
	}
	if sessions.savedChild != "" {
		t.Fatalf("stale edit created child %q", sessions.savedChild)
	}
}

func TestEditUserMessageAndForkSameOperationIsIdempotent(t *testing.T) {
	service, sessions, engine := editForkFixture(t)
	revision := service.components.frontendSnapshot.Snapshot().ConversationRevision
	request := model.EditUserMessageForkRequest{
		TargetEventSeq: 3, NewText: "only once", ExpectedRevision: revision, OperationID: "double-click",
	}
	first, err := service.components.editFork.application.EditUserMessageAndFork(context.Background(), "parent", request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.components.editFork.application.EditUserMessageAndFork(context.Background(), "parent", request)
	if err != nil {
		t.Fatal(err)
	}
	if first.ChildSessionID == "" || first.ChildSessionID != second.ChildSessionID {
		t.Fatalf("idempotent child IDs = %q / %q", first.ChildSessionID, second.ChildSessionID)
	}
	if sessions.savedChild != first.ChildSessionID {
		t.Fatalf("saved child = %q, want %q", sessions.savedChild, first.ChildSessionID)
	}
	engine.releaseSession(first.ChildSessionID)
	waitSessionIdle(t, service)
}

func TestEditUserMessageAndForkSamePointWithNewOperationCreatesNewChild(t *testing.T) {
	service, _, engine := editForkFixture(t)
	first, err := service.components.editFork.application.EditUserMessageAndFork(context.Background(), "parent", model.EditUserMessageForkRequest{
		TargetEventSeq: 3, NewText: "first branch", ExpectedRevision: 6, OperationID: "branch-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	engine.releaseSession(first.ChildSessionID)
	waitSessionIdle(t, service)
	if err := service.components.sessionResume.Resume("parent"); err != nil {
		t.Fatalf("resume parent: %v", err)
	}
	second, err := service.components.editFork.application.EditUserMessageAndFork(context.Background(), "parent", model.EditUserMessageForkRequest{
		TargetEventSeq: 3, NewText: "second branch", ExpectedRevision: 6, OperationID: "branch-b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ChildSessionID == "" || second.ChildSessionID == "" || first.ChildSessionID == second.ChildSessionID {
		t.Fatalf("same-point children = %q / %q, want distinct IDs", first.ChildSessionID, second.ChildSessionID)
	}
	engine.releaseSession(second.ChildSessionID)
	waitSessionIdle(t, service)
}

func TestEditUserMessageAndForkKeepsChildIsolatedFromParentLaterTurns(t *testing.T) {
	service, _, engine := editForkFixture(t)
	child, err := service.components.editFork.application.EditUserMessageAndFork(context.Background(), "parent", model.EditUserMessageForkRequest{
		TargetEventSeq: 3, NewText: "child only", ExpectedRevision: 6, OperationID: "isolation-child",
	})
	if err != nil {
		t.Fatal(err)
	}
	engine.releaseSession(child.ChildSessionID)
	waitSessionIdle(t, service)

	if err := service.components.sessionResume.Resume("parent"); err != nil {
		t.Fatalf("resume parent: %v", err)
	}
	if err := service.components.submitApplication.SubmitToSession(context.Background(), "parent", "parent later turn"); err != nil {
		t.Fatalf("parent later submit: %v", err)
	}
	engine.releaseSession("parent")
	waitSessionIdle(t, service)

	if err := service.components.sessionResume.Resume(child.ChildSessionID); err != nil {
		t.Fatalf("resume child: %v", err)
	}
	for _, text := range userTexts(service.components.frontendSnapshot.Snapshot().Conversation) {
		if text == "parent later turn" {
			t.Fatal("parent later turn leaked into child")
		}
	}
}

func TestEditForkRollbackDeletesChildAndRestoresParentView(t *testing.T) {
	service, sessions, engine := editForkFixture(t)
	result, err := service.components.editFork.application.EditUserMessageAndFork(context.Background(), "parent", model.EditUserMessageForkRequest{
		TargetEventSeq: 3, NewText: "will rollback", ExpectedRevision: 6, OperationID: "rollback-child",
	})
	if err != nil {
		t.Fatal(err)
	}
	engine.releaseSession(result.ChildSessionID)
	waitSessionIdle(t, service)
	rollback := applicationedit.NewRollback(applicationedit.RollbackDeps{
		Delete: service.components.sessionDelete.Delete,
		Resume: service.components.sessionResume.Resume,
	})
	if err := rollback.Rollback("parent", result.ChildSessionID, errors.New("submit failed")); err == nil {
		t.Fatal("rollback must preserve the original failure")
	}
	if got := service.components.frontendSnapshot.Snapshot().Session.ID; got != "parent" {
		t.Fatalf("view after rollback = %q, want parent", got)
	}
	found := false
	for _, deleted := range sessions.deleted {
		if deleted == result.ChildSessionID {
			found = true
		}
	}
	if !found {
		t.Fatalf("rollback deleted = %#v, want child %q", sessions.deleted, result.ChildSessionID)
	}
}

func TestEditUserMessageAndForkExplainsMissingEventSeq(t *testing.T) {
	service, _, _ := editForkFixture(t)
	messages := service.components.editFork.annotation.Annotate("parent", []model.Message{
		{ID: "legacy-user", Role: "user", Content: "legacy input"},
	}, false)
	if len(messages) != 1 {
		t.Fatalf("messages = %#v", messages)
	}
	if messages[0].Editable || !strings.Contains(messages[0].EditDisabledReason, "事件") {
		t.Fatalf("legacy message annotation = %#v, want non-editable with event reason", messages[0])
	}
}

func TestAnnotateConversationUsesRetainedWindowAndRunningState(t *testing.T) {
	service, _, _ := editForkFixture(t)
	editForkRetainedWindow(t, service, 2)
	messages := service.components.editFork.annotation.Annotate("parent", []model.Message{
		{ID: "compressed", Role: "user", Content: "old", EventSeq: 1},
		{ID: "retained", Role: "user", Content: "kept", EventSeq: 3},
		{ID: "assistant", Role: "assistant", Content: "answer", EventSeq: 4},
	}, false)
	if messages[0].Editable || !strings.Contains(messages[0].EditDisabledReason, "压缩") {
		t.Fatalf("compressed annotation = %#v", messages[0])
	}
	if !messages[1].Editable || messages[1].EditDisabledReason != "" {
		t.Fatalf("retained annotation = %#v", messages[1])
	}
	if messages[2].Editable || messages[2].EditDisabledReason != "" {
		t.Fatalf("assistant annotation = %#v", messages[2])
	}
	running := service.components.editFork.annotation.Annotate("parent", messages[1:2], true)
	if running[0].Editable || !strings.Contains(running[0].EditDisabledReason, "运行") {
		t.Fatalf("running annotation = %#v", running[0])
	}
}
