package cold_test

import (
	"errors"
	cold "github.com/RedHuang-0622/seelex/application/session/cold"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/session"
	"testing"
)

func TestSessionColdHistoryUsesLegacyHistoryWithoutRecord(t *testing.T) {
	legacy := []contract.EngineMessage{{Role: "user", Content: "legacy"}}
	component := cold.NewHistory(cold.HistoryDeps{})

	got, err := component.Assemble("session-a", cold.ReadResult{
		Location:  session_runtime.Location{WorkspaceID: "workspace-a"},
		HasRecord: false,
		History:   legacy,
	})

	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if len(got.EngineHistory) != 1 || got.EngineHistory[0].Content != "legacy" || got.Renumbered {
		t.Fatalf("legacy history = %+v", got)
	}
}

func TestSessionColdHistoryRebuildsTranscriptAndPrefersWireHistory(t *testing.T) {
	record := model.SessionRecord{
		Conversation: model.ConversationRecord{Messages: []model.Message{{Role: "user", Content: "latest"}}},
	}
	rebuilt := []model.TranscriptEvent{{Seq: 1, Role: "user", Content: "latest"}}
	wireHistory := []contract.EngineMessage{{Role: "user", Content: "wire"}}
	calls := []string{}
	component := cold.NewHistory(cold.HistoryDeps{
		TailBudget:             func() int { return 100 },
		MinRounds:              func() int { return 2 },
		LatestUser:             func([]model.Message) string { return "latest" },
		TranscriptContainsUser: func([]model.TranscriptEvent, string) bool { return false },
		RecordTranscript: func(model.SessionRecord) []model.TranscriptEvent {
			calls = append(calls, "rebuild")
			return rebuilt
		},
		TranscriptTailHistory: func(transcript []model.TranscriptEvent, budget, minRounds int) []contract.EngineMessage {
			calls = append(calls, "tail")
			if budget != 100 || minRounds != 2 {
				t.Fatalf("tail args = %d/%d", budget, minRounds)
			}
			return []contract.EngineMessage{{Role: "user", Content: "tail"}}
		},
		AssembleWireHistory: func(sessionID string, tailBudget, k int) ([]contract.EngineMessage, bool, error) {
			calls = append(calls, "wire")
			return wireHistory, true, nil
		},
		RecordHistory: func(model.SessionRecord, int, int) []contract.EngineMessage {
			calls = append(calls, "record")
			return nil
		},
		HistoryContainsUser: func([]contract.EngineMessage, string) bool { return true },
		RecordResumeHistory: func(model.SessionRecord) []contract.EngineMessage {
			calls = append(calls, "resume")
			return nil
		},
	})

	got, err := component.Assemble("session-a", cold.ReadResult{
		Location:   session_runtime.Location{WorkspaceID: "workspace-a"},
		Record:     record,
		HasRecord:  true,
		Transcript: nil,
	})

	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if !got.Renumbered || len(got.Transcript) != 1 {
		t.Fatalf("transcript result = %+v", got)
	}
	if len(got.EngineHistory) != 1 || got.EngineHistory[0].Content != "wire" {
		t.Fatalf("engine history = %+v", got.EngineHistory)
	}
	if len(calls) < 3 || calls[0] != "rebuild" || calls[1] != "tail" || calls[2] != "wire" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestSessionColdHistoryFallsBackToRecordAndResumeHistory(t *testing.T) {
	record := model.SessionRecord{}
	component := cold.NewHistory(cold.HistoryDeps{
		TailBudget:             func() int { return 10 },
		MinRounds:              func() int { return 1 },
		LatestUser:             func([]model.Message) string { return "" },
		TranscriptContainsUser: func([]model.TranscriptEvent, string) bool { return true },
		RecordTranscript:       func(model.SessionRecord) []model.TranscriptEvent { return nil },
		TranscriptTailHistory:  func([]model.TranscriptEvent, int, int) []contract.EngineMessage { return nil },
		AssembleWireHistory: func(string, int, int) ([]contract.EngineMessage, bool, error) {
			return nil, false, nil
		},
		RecordHistory: func(model.SessionRecord, int, int) []contract.EngineMessage {
			return []contract.EngineMessage{{Role: "assistant", Content: "record"}}
		},
		HistoryContainsUser: func([]contract.EngineMessage, string) bool { return false },
		RecordResumeHistory: func(model.SessionRecord) []contract.EngineMessage {
			return []contract.EngineMessage{{Role: "assistant", Content: "resume"}}
		},
	})

	got, err := component.Assemble("session-a", cold.ReadResult{
		Location:   session_runtime.Location{WorkspaceID: "workspace-a"},
		Record:     record,
		HasRecord:  true,
		Transcript: []model.TranscriptEvent{{Seq: 2}},
	})

	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if len(got.EngineHistory) != 1 || got.EngineHistory[0].Content != "record" {
		t.Fatalf("record fallback = %+v", got.EngineHistory)
	}
}

func TestSessionColdHistoryUsesFinalResumeFallback(t *testing.T) {
	component := cold.NewHistory(cold.HistoryDeps{
		TailBudget:             func() int { return 10 },
		MinRounds:              func() int { return 1 },
		LatestUser:             func([]model.Message) string { return "" },
		TranscriptContainsUser: func([]model.TranscriptEvent, string) bool { return true },
		RecordTranscript:       func(model.SessionRecord) []model.TranscriptEvent { return nil },
		TranscriptTailHistory:  func([]model.TranscriptEvent, int, int) []contract.EngineMessage { return nil },
		AssembleWireHistory: func(string, int, int) ([]contract.EngineMessage, bool, error) {
			return nil, false, nil
		},
		RecordHistory:       func(model.SessionRecord, int, int) []contract.EngineMessage { return nil },
		HistoryContainsUser: func([]contract.EngineMessage, string) bool { return true },
		RecordResumeHistory: func(model.SessionRecord) []contract.EngineMessage {
			return []contract.EngineMessage{{Role: "assistant", Content: "resume"}}
		},
	})

	got, err := component.Assemble("session-a", cold.ReadResult{
		Location:   session_runtime.Location{WorkspaceID: "workspace-a"},
		HasRecord:  true,
		Transcript: []model.TranscriptEvent{{Seq: 2}},
	})

	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if len(got.EngineHistory) != 1 || got.EngineHistory[0].Content != "resume" {
		t.Fatalf("resume fallback = %+v", got.EngineHistory)
	}
}

func TestSessionColdHistoryWrapsWireError(t *testing.T) {
	wireErr := errors.New("wire failed")
	component := cold.NewHistory(cold.HistoryDeps{
		TailBudget:             func() int { return 10 },
		MinRounds:              func() int { return 1 },
		LatestUser:             func([]model.Message) string { return "" },
		TranscriptContainsUser: func([]model.TranscriptEvent, string) bool { return true },
		RecordTranscript:       func(model.SessionRecord) []model.TranscriptEvent { return nil },
		TranscriptTailHistory:  func([]model.TranscriptEvent, int, int) []contract.EngineMessage { return nil },
		AssembleWireHistory: func(string, int, int) ([]contract.EngineMessage, bool, error) {
			return nil, false, wireErr
		},
		RecordHistory:       func(model.SessionRecord, int, int) []contract.EngineMessage { return nil },
		HistoryContainsUser: func([]contract.EngineMessage, string) bool { return true },
		RecordResumeHistory: func(model.SessionRecord) []contract.EngineMessage { return nil },
	})

	_, err := component.Assemble("session-a", cold.ReadResult{
		Location:   session_runtime.Location{WorkspaceID: "workspace-a"},
		HasRecord:  true,
		Transcript: []model.TranscriptEvent{{Seq: 2}},
	})

	if !errors.Is(err, wireErr) {
		t.Fatalf("Assemble error = %v, want wire error", err)
	}
}
