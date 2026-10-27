package cold_test

import (
	"errors"
	cold "github.com/RedHuang-0622/seelex/application/session/cold"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/session"
	"strings"
	"testing"
)

func TestSessionColdReadReturnsRecordAndTranscript(t *testing.T) {
	location := session_runtime.Location{WorkspaceID: "workspace-a"}
	record := model.SessionRecord{Title: model.SessionTitle{Value: "saved"}}
	events := []model.TranscriptEvent{{Seq: 3}}
	read := cold.NewRead(cold.ReadDeps{
		RecoverTail: func(sessionID string) string { return "notice:" + sessionID },
		Locate:      func(string) session_runtime.Location { return location },
		LoadRecord: func(got session_runtime.Location, sessionID string) (model.SessionRecord, int, bool, error) {
			if got != location || sessionID != "session-a" {
				t.Fatalf("record args = %+v/%s", got, sessionID)
			}
			return record, 9, true, nil
		},
		LoadTranscript: func(got session_runtime.Location, sessionID string) ([]model.TranscriptEvent, error) {
			if got != location || sessionID != "session-a" {
				t.Fatalf("transcript args = %+v/%s", got, sessionID)
			}
			return events, nil
		},
		LoadHistory: func(session_runtime.Location) ([]contract.EngineMessage, int, error) {
			t.Fatal("有 record 时不得读取 legacy history")
			return nil, 0, nil
		},
	})

	got, err := read.Read("session-a")

	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.PendingTailNotice != "notice:session-a" || got.Location != location ||
		!got.HasRecord || got.RecordTotal != 9 || got.Record.Title.Value != "saved" ||
		len(got.Transcript) != 1 || got.Transcript[0].Seq != 3 {
		t.Fatalf("read result = %+v", got)
	}
}

func TestSessionColdReadFallsBackToLegacyHistory(t *testing.T) {
	history := []contract.EngineMessage{{Role: "user", Content: "legacy"}}
	read := cold.NewRead(cold.ReadDeps{
		RecoverTail: func(string) string { return "" },
		Locate:      func(string) session_runtime.Location { return session_runtime.Location{} },
		LoadRecord: func(session_runtime.Location, string) (model.SessionRecord, int, bool, error) {
			return model.SessionRecord{}, 0, false, nil
		},
		LoadTranscript: func(session_runtime.Location, string) ([]model.TranscriptEvent, error) {
			return nil, nil
		},
		LoadHistory: func(session_runtime.Location) ([]contract.EngineMessage, int, error) {
			return history, 12, nil
		},
	})

	got, err := read.Read("session-a")

	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.HasRecord || got.HistoryTotal != 12 || len(got.History) != 1 || got.History[0].Content != "legacy" {
		t.Fatalf("legacy result = %+v", got)
	}
}

func TestSessionColdReadErrorPrecedence(t *testing.T) {
	recordErr := errors.New("record failed")
	historyErr := errors.New("history failed")
	transcriptErr := errors.New("transcript failed")
	tests := []struct {
		name          string
		hasRecord     bool
		recordErr     error
		historyErr    error
		transcriptErr error
		want          error
		text          string
	}{
		{name: "record", recordErr: recordErr, want: recordErr, text: "load session record"},
		{name: "legacy-history", historyErr: historyErr, want: historyErr, text: "load session \"session-a\""},
		{name: "legacy-transcript", transcriptErr: transcriptErr, want: transcriptErr, text: "load session transcript"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			read := cold.NewRead(cold.ReadDeps{
				RecoverTail: func(string) string { return "" },
				Locate:      func(string) session_runtime.Location { return session_runtime.Location{} },
				LoadRecord: func(session_runtime.Location, string) (model.SessionRecord, int, bool, error) {
					return model.SessionRecord{}, 0, test.hasRecord, test.recordErr
				},
				LoadTranscript: func(session_runtime.Location, string) ([]model.TranscriptEvent, error) {
					return nil, test.transcriptErr
				},
				LoadHistory: func(session_runtime.Location) ([]contract.EngineMessage, int, error) {
					return nil, 0, test.historyErr
				},
			})

			_, err := read.Read("session-a")

			if !errors.Is(err, test.want) || !strings.Contains(err.Error(), test.text) {
				t.Fatalf("Read error = %v, want %v containing %q", err, test.want, test.text)
			}
		})
	}
}

func TestSessionColdReadIgnoresLegacyErrorsWhenRecordExists(t *testing.T) {
	read := cold.NewRead(cold.ReadDeps{
		RecoverTail: func(string) string { return "" },
		Locate:      func(string) session_runtime.Location { return session_runtime.Location{} },
		LoadRecord: func(session_runtime.Location, string) (model.SessionRecord, int, bool, error) {
			return model.SessionRecord{}, 0, true, nil
		},
		LoadTranscript: func(session_runtime.Location, string) ([]model.TranscriptEvent, error) {
			return nil, errors.New("old transcript unavailable")
		},
		LoadHistory: func(session_runtime.Location) ([]contract.EngineMessage, int, error) {
			return nil, 0, errors.New("old history unavailable")
		},
	})

	got, err := read.Read("session-a")

	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !got.HasRecord || got.History != nil {
		t.Fatalf("result = %+v", got)
	}
}
