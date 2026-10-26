package cold

import (
	"fmt"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// HistoryResult is the assembled transcript/provider history.
type HistoryResult struct {
	Transcript    []model.TranscriptEvent
	Renumbered    bool
	EngineHistory []contract.EngineMessage
}

// HistoryDeps supplies transcript reconstruction and provider-history reads.
type HistoryDeps struct {
	TailBudget             func() int
	MinRounds              func() int
	LatestUser             func([]model.Message) string
	TranscriptContainsUser func([]model.TranscriptEvent, string) bool
	RecordTranscript       func(model.SessionRecord) []model.TranscriptEvent
	TranscriptTailHistory  func([]model.TranscriptEvent, int, int) []contract.EngineMessage
	AssembleWireHistory    func(sessionID string, tailBudget, k int) ([]contract.EngineMessage, bool, error)
	RecordHistory          func(model.SessionRecord, int, int) []contract.EngineMessage
	HistoryContainsUser    func([]contract.EngineMessage, string) bool
	RecordResumeHistory    func(model.SessionRecord) []contract.EngineMessage
}

// HistoryApplication owns cold transcript/provider-history assembly.
type HistoryApplication struct {
	deps HistoryDeps
}

// NewHistory constructs the cold-history application.
func NewHistory(deps HistoryDeps) *HistoryApplication {
	return &HistoryApplication{deps: deps}
}

// Assemble reconstructs transcript and provider history for a stored session.
func (application *HistoryApplication) Assemble(sessionID string, loaded ReadResult) (HistoryResult, error) {
	result := HistoryResult{Transcript: loaded.Transcript, EngineHistory: loaded.History}
	if !loaded.HasRecord {
		return result, nil
	}
	tailBudget := application.deps.TailBudget()
	latestUser := application.deps.LatestUser(loaded.Record.Conversation.Messages)
	if len(result.Transcript) == 0 ||
		(latestUser != "" && !application.deps.TranscriptContainsUser(result.Transcript, latestUser)) {
		result.Transcript = application.deps.RecordTranscript(loaded.Record)
		result.Renumbered = true
	}
	result.EngineHistory = application.deps.TranscriptTailHistory(
		result.Transcript,
		tailBudget,
		application.deps.MinRounds(),
	)
	wire, wireOK, err := application.deps.AssembleWireHistory(sessionID, tailBudget, 3)
	if err != nil {
		return HistoryResult{}, fmt.Errorf("assemble wire history %q: %w", sessionID, err)
	}
	if wireOK && len(wire) > 0 {
		result.EngineHistory = wire
	}
	recordHistory := application.deps.RecordHistory(loaded.Record, tailBudget, 4)
	if len(result.EngineHistory) == 0 ||
		(latestUser != "" && !application.deps.HistoryContainsUser(result.EngineHistory, latestUser)) {
		result.EngineHistory = recordHistory
	}
	if len(result.EngineHistory) == 0 {
		result.EngineHistory = application.deps.RecordResumeHistory(loaded.Record)
	}
	return result, nil
}
