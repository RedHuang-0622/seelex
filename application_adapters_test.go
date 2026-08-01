package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/workspace"
)

type fakeReactorEngine struct {
	sessionID string
	history   []types.Message
	onChat    func()
}

func (fake *fakeReactorEngine) ChatStream(_ context.Context, input string, _ func(string)) (string, error) {
	fake.history = append(fake.history, types.Message{Role: "user", Content: &input})
	if fake.onChat != nil {
		fake.onChat()
	}
	return "", nil
}

func (fake *fakeReactorEngine) History() []types.Message {
	return append([]types.Message(nil), fake.history...)
}

func (fake *fakeReactorEngine) ClearHistory() {
	retained := make([]types.Message, 0, len(fake.history))
	for _, message := range fake.history {
		if message.Role == "system" {
			retained = append(retained, message)
		}
	}
	fake.history = retained
}

func (fake *fakeReactorEngine) SessionID() string { return fake.sessionID }
func (*fakeReactorEngine) SetSystemPrompt(string) {}
func (*fakeReactorEngine) SetMaxLoops(int)        {}
func (fake *fakeReactorEngine) AppendHistory(message types.Message) {
	fake.history = append(fake.history, message)
}

func TestEnginePortStartSessionCreatesAnIndependentReactor(t *testing.T) {
	oldInput := "old-session-input"
	old := &fakeReactorEngine{
		sessionID: "session-old",
		history:   []types.Message{{Role: "user", Content: &oldInput}},
	}
	fresh := &fakeReactorEngine{sessionID: "session-new"}
	factoryCalls := 0
	port := newEnginePort(old, func() reactorEngine {
		factoryCalls++
		return fresh
	}, nil)

	if got := port.StartSession(); got != "session-new" {
		t.Fatalf("new session ID = %q, want session-new", got)
	}
	if factoryCalls != 1 {
		t.Fatalf("new-engine factory calls = %d, want 1", factoryCalls)
	}
	if got := port.History(); len(got) != 0 {
		t.Fatalf("new session inherited old history: %#v", got)
	}
	if got := old.History(); len(got) != 1 || got[0].Content == nil || *got[0].Content != oldInput {
		t.Fatalf("old reactor was mutated instead of being retired: %#v", got)
	}
}

func TestEngineMessageRoundTripPreservesResumeContext(t *testing.T) {
	t.Parallel()
	empty := ""
	toolResult := "done"
	original := []seelebridge.Message{
		{
			Role: "assistant", ReasoningContent: "reasoning", Content: &empty,
			ToolCalls: []types.ToolCall{{
				ID: "call-1", Type: "function",
				Function: types.ToolCallFunction{Name: "read", Arguments: `{"path":"README.md"}`},
			}},
		},
		{Role: "tool", Content: &toolResult, ToolCallID: "call-1", Name: "read"},
	}

	restored := restoreMessages(adaptMessages(original))
	if !reflect.DeepEqual(restored, original) {
		t.Fatalf("restored history differs\n got: %#v\nwant: %#v", restored, original)
	}
}

func TestEnginePortReplaceHistoryUsesFreshReactorAndCollapsesSystemMessages(t *testing.T) {
	oldPrompt, duplicatePrompt, userInput := "product prompt", "stale summary", "old input"
	old := &fakeReactorEngine{sessionID: "engine-old", history: []types.Message{
		{Role: "system", Content: &oldPrompt},
		{Role: "system", Content: &duplicatePrompt},
		{Role: "user", Content: &userInput},
	}}
	fresh := &fakeReactorEngine{sessionID: "engine-fresh"}
	port := newEnginePort(old, func() reactorEngine { return fresh }, nil)
	resume := "resume from checkpoint"
	if err := port.replaceRawHistory("logical-session", []seelebridge.Message{
		{Role: "system", Content: &oldPrompt},
		{Role: "system", Content: &duplicatePrompt},
		{Role: "user", Content: &resume},
	}); err != nil {
		t.Fatal(err)
	}
	if got := fresh.History(); len(got) != 2 || got[0].Role != "system" || got[1].Content == nil || *got[1].Content != resume {
		t.Fatalf("fresh history = %#v, want one system and resume input", got)
	}
	if got := port.SessionID(); got != "logical-session" {
		t.Fatalf("logical session ID = %q", got)
	}
}

func TestEnginePortDefersCleanReactorUntilActiveCallReturns(t *testing.T) {
	prompt, checkpoint := "product prompt", "checkpoint"
	old := &fakeReactorEngine{sessionID: "engine-old"}
	fresh := &fakeReactorEngine{sessionID: "engine-fresh"}
	port := newEnginePort(old, func() reactorEngine { return fresh }, nil)
	old.onChat = func() {
		if err := port.replaceRawHistory("logical-session", []seelebridge.Message{
			{Role: "system", Content: &prompt},
			{Role: "user", Content: &checkpoint},
		}); err != nil {
			t.Errorf("replace history: %v", err)
		}
	}
	if _, err := port.ChatStream(context.Background(), "run", nil); err != nil {
		t.Fatal(err)
	}
	if got := port.History(); len(got) != 2 || got[0].Role != "system" || got[1].Content != checkpoint {
		t.Fatalf("deferred fresh history = %#v", got)
	}
}

func TestDecodeSessionRecordMigratesLegacyArchive(t *testing.T) {
	payload := []byte(`{"version":1,"name":"Stable legacy title","conversation":[{"id":"user-1","role":"user","content":"first request"}],"plan":{"entry_node_id":"inspect"},"plan_arguments":"{\"entry\":\"inspect\"}"}`)
	record, err := decodeSessionRecord(payload, "session-a")
	if err != nil {
		t.Fatal(err)
	}
	if record.Version != 2 || record.ID != "session-a" || record.Title.Value != "Stable legacy title" || record.Title.Source != "legacy_history" {
		t.Fatalf("migrated record = %#v", record)
	}
	if len(record.Conversation.Messages) != 1 || record.ActivePlanID != "legacy-plan" || len(record.PlanStack) != 1 || record.PlanStack[0].Arguments == "" {
		t.Fatalf("migrated record payload = %#v", record)
	}
}

func TestDecodeSessionRecordPreservesV2Components(t *testing.T) {
	want := application.SessionRecord{Version: 2, ID: "session-b", Title: application.SessionTitle{Value: "Stable title", Source: "user"}}
	payload, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeSessionRecord(payload, "session-b")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("record = %#v, want %#v", got, want)
	}
}

func TestWorkspacePortUsesRootBasenameAndUniqueIDs(t *testing.T) {
	parentA, parentB := t.TempDir(), t.TempDir()
	rootA, rootB := filepath.Join(parentA, "shared-name"), filepath.Join(parentB, "shared-name")
	for _, root := range []string{rootA, rootB} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	port := workspacePort{repo: workspace.NewRepo()}
	first, err := port.Create("custom label", rootA, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := port.Create("custom label", rootB, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Name != "shared-name" || second.Name != "shared-name" {
		t.Fatalf("workspace names = %q, %q; want root basename", first.Name, second.Name)
	}
	if first.ID == second.ID {
		t.Fatalf("duplicate display names must not collide on ID %q", first.ID)
	}
	got, err := port.Get(first.ID)
	if err != nil || got.ID != first.ID || got.Name != "shared-name" {
		t.Fatalf("workspace lookup changed identity: got=%#v err=%v", got, err)
	}
}
