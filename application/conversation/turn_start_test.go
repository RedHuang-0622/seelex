package conversation_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/prompt"
	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

type turnStartTestView struct {
	title             model.SessionTitle
	appended          []string
	revision          uint64
	activeName        string
	restoreRequested  bool
	compactRequested  bool
	draftMaterialized bool
}

func (view *turnStartTestView) SessionTitleFor(string) model.SessionTitle {
	return view.title
}

func (view *turnStartTestView) SetSessionTitleLocked(_ string, title model.SessionTitle) {
	view.title = title
}

func (view *turnStartTestView) AppendMessageWithOriginLocked(role, content string, _ *model.ToolCall, _ model.MessageOrigin) *model.Message {
	view.appended = append(view.appended, "active:"+role+":"+content)
	return &model.Message{Role: role, Content: content}
}

func (view *turnStartTestView) AppendMessageWithOriginLockedFor(sessionID, role, content string, _ *model.ToolCall, _ model.MessageOrigin) *model.Message {
	view.appended = append(view.appended, sessionID+":"+role+":"+content)
	return &model.Message{Role: role, Content: content}
}

func (view *turnStartTestView) SetSessionChatLockedFor(string, model.ChatState) {}

func (view *turnStartTestView) BumpLocked() uint64 {
	view.revision++
	return view.revision
}

type turnStartTestProcess struct {
	requestID string
	request   applicationconversation.Request
}

func (process *turnStartTestProcess) BeginLocked(
	parent context.Context,
	_ string,
	_ applicationconversation.TurnUnit,
	request applicationconversation.Request,
	_ applicationconversation.StartOptions,
) (context.Context, string, model.TranscriptEvent) {
	process.request = request
	process.requestID = "turn-a"
	return parent, process.requestID, model.TranscriptEvent{Role: "user"}
}

func newTurnStartTestApplication(t *testing.T, view *turnStartTestView, unit *sessioncase.SessionUnit) (*applicationconversation.TurnStartApplication, *[]string) {
	t.Helper()
	var mu sync.RWMutex
	var calls []string
	started := make([]applicationconversation.Request, 0, 1)
	queue := applicationconversation.NewQueue(applicationconversation.QueueDeps{
		TurnState: applicationconversation.NewTurnState(),
		Requests: applicationconversation.NewRequestApplication(applicationconversation.RequestDeps{
			ParseModelEnvelope: func(string) (string, int, bool) { return "", 0, false },
			WrapModelInput:     func(display, _ string) string { return display },
		}),
		MirrorChatState: func(string, model.ChatState) { calls = append(calls, "queue-mirror") },
		BumpRevision:    func() uint64 { calls = append(calls, "queue-revision"); return 3 },
		PublishSnapshot: func(uint64, string) { calls = append(calls, "queue-snapshot") },
		PublishChatState: func(string) {
			calls = append(calls, "queue-chat")
		},
	})
	application := applicationconversation.NewTurnStart(applicationconversation.TurnStartDeps{
		ViewMu: &mu,
		Lifecycle: func() (bool, bool) {
			return false, false
		},
		SessionUnit: func(string) *sessioncase.SessionUnit {
			return unit
		},
		IsActive:          func(string) bool { return false },
		IsRestoringLocked: func(string) bool { return view.restoreRequested },
		IsCompactingLocked: func(string) bool {
			return view.compactRequested
		},
		Queue:   queue,
		Process: &turnStartTestProcess{},
		View:    view,
		TitleFor: func(input string) model.SessionTitle {
			return model.SessionTitle{Value: input, Source: "first_request"}
		},
		SetSessionChat:            func(string, model.ChatState) {},
		PublishChatState:          func(string) { calls = append(calls, "publish-chat") },
		SetCurrentTaskBatch:       func(string, string) { calls = append(calls, "batch") },
		PublishRuntimeProjections: func() { calls = append(calls, "runtime") },
		DiscardMailbox:            func(string) { calls = append(calls, "mailbox") },
		PublishEvent: func(_ event.EventKind, _ uint64, _, _ string, _ any) {
			calls = append(calls, "event")
		},
		Dispatch: func(_ context.Context, _ string, _ string, request applicationconversation.Request) {
			started = append(started, request)
			calls = append(calls, "dispatch")
		},
		DeferUntilRestored: func(_ context.Context, _, text string) {
			calls = append(calls, "defer-restore:"+text)
		},
		DeferAfterCompaction: func(_ context.Context, _, text string) {
			calls = append(calls, "defer-compaction:"+text)
		},
		ClosedError:   errors.New("closed"),
		DrainingError: errors.New("draining"),
	})
	return application, &calls
}

func TestTurnStartApplicationDefersBeforeOpeningRestoredShell(t *testing.T) {
	unit, err := sessioncase.NewSessionUnit("session-a")
	if err != nil {
		t.Fatal(err)
	}
	view := &turnStartTestView{restoreRequested: true}
	application, calls := newTurnStartTestApplication(t, view, unit)

	err = application.StartFor(context.Background(), "session-a", applicationconversation.Request{DisplayInput: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if got := (*calls)[0]; got != "defer-restore:hello" {
		t.Fatalf("first call = %q, want restore deferral", got)
	}
	if unit.ChatState().Running {
		t.Fatal("restoring session must not open a turn")
	}
}

func TestTurnStartApplicationDefersBeforeCompactionWritesHistory(t *testing.T) {
	unit, err := sessioncase.NewSessionUnit("session-a")
	if err != nil {
		t.Fatal(err)
	}
	view := &turnStartTestView{compactRequested: true}
	application, calls := newTurnStartTestApplication(t, view, unit)

	err = application.StartFor(context.Background(), "session-a", applicationconversation.Request{DisplayInput: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if got := (*calls)[0]; got != "defer-compaction:hello" {
		t.Fatalf("first call = %q, want compaction deferral", got)
	}
	if unit.ChatState().Running {
		t.Fatal("compacting session must not open a turn")
	}
}

func TestTurnStartApplicationQueuesRunningSessionBeforeDraining(t *testing.T) {
	unit, err := sessioncase.NewSessionUnit("session-a")
	if err != nil {
		t.Fatal(err)
	}
	unit.SetChatState(model.ChatState{Running: true, RequestID: "turn-running"}, nil)
	view := &turnStartTestView{}
	application, _ := newTurnStartTestApplication(t, view, unit)

	err = application.StartFor(context.Background(), "session-a", applicationconversation.Request{DisplayInput: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	pending := unit.PendingRequests()
	if len(pending) != 1 || pending[0].DisplayInput != "queued" {
		t.Fatalf("pending queue = %+v, want one FIFO item", pending)
	}
	if len(view.appended) != 0 {
		t.Fatalf("running session opened messages: %v", view.appended)
	}
}

func TestTurnStartApplicationDrainsRecoveredInputBeforeStarting(t *testing.T) {
	unit, err := sessioncase.NewSessionUnit("session-a")
	if err != nil {
		t.Fatal(err)
	}
	pending := applicationconversation.Request{DisplayInput: "recovered"}
	unit.Enqueue(sessioncase.QueuedRequest{DisplayInput: pending.DisplayInput, Payload: pending})
	view := &turnStartTestView{}
	application, calls := newTurnStartTestApplication(t, view, unit)

	err = application.StartFor(context.Background(), "session-a", applicationconversation.Request{DisplayInput: "current"})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(view.appended); got != 2 {
		t.Fatalf("appended messages = %d, want user+assistant", got)
	}
	if view.appended[0] != "session-a:user:recovered\n---\ncurrent" {
		t.Fatalf("started input = %q, want drained recovered input first", view.appended[0])
	}
	if got := (*calls)[len(*calls)-1]; got != "dispatch" {
		t.Fatalf("last call = %q, want dispatch", got)
	}
	if len(unit.PendingRequests()) != 0 {
		t.Fatalf("recovered queue not drained: %+v", unit.PendingRequests())
	}
}

func TestConversationSubmissionMaterializesOnlyCurrentDraft(t *testing.T) {
	var transition sync.Mutex
	var started []string
	view := &turnStartTestView{}
	starter := turnSubmissionTestStarter(func(_ context.Context, sessionID string, _ applicationconversation.Request) error {
		started = append(started, sessionID)
		return nil
	})
	application := applicationconversation.NewSubmission(applicationconversation.SubmissionDeps{
		TransitionLock:   func() sync.Locker { return &transition },
		CurrentSessionID: func() string { return "session-a" },
		MaterializeDraft: func(string) error { view.draftMaterialized = true; return nil },
		Request:          applicationconversation.NewRequestApplication(applicationconversation.RequestDeps{}),
		Layers:           func() []prompt.PromptLayer { return nil },
		EffortFor:        func(string) string { return "high" },
		BudgetFor:        func(string) prompt.ReActBudget { return prompt.ReActBudget{MaxToolCalls: 7} },
		Start:            starter,
	})

	if err := application.Submit(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if !view.draftMaterialized {
		t.Fatal("current-session submission must materialize its draft")
	}
	if len(started) != 1 || started[0] != "session-a" {
		t.Fatalf("started = %v, want session-a", started)
	}
	view.draftMaterialized = false
	if err := application.SubmitFor(context.Background(), "session-b", "background"); err != nil {
		t.Fatal(err)
	}
	if view.draftMaterialized {
		t.Fatal("explicit background submission must not consume the active draft")
	}
}

type turnSubmissionTestStarter func(context.Context, string, applicationconversation.Request) error

func (start turnSubmissionTestStarter) StartFor(ctx context.Context, sessionID string, request applicationconversation.Request) error {
	return start(ctx, sessionID, request)
}
