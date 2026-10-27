// Package cold owns application orchestration for cold session loading.
package cold

import (
	"fmt"
	"sync"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	session_runtime "github.com/RedHuang-0622/seelex/kernel/session"
)

// ReadResult is one cold-load persistence read.
type ReadResult struct {
	PendingTailNotice string
	Location          session_runtime.Location
	Record            model.SessionRecord
	HasRecord         bool
	RecordTotal       int
	History           []contract.EngineMessage
	HistoryTotal      int
	Transcript        []model.TranscriptEvent
}

// ReadDeps supplies record, transcript and history reads.
type ReadDeps struct {
	RecoverTail    func(sessionID string) string
	Locate         func(sessionID string) session_runtime.Location
	LoadRecord     func(location session_runtime.Location, sessionID string) (model.SessionRecord, int, bool, error)
	LoadTranscript func(location session_runtime.Location, sessionID string) ([]model.TranscriptEvent, error)
	LoadHistory    func(location session_runtime.Location) ([]contract.EngineMessage, int, error)
}

// ReadApplication owns the cold-load read phase.
type ReadApplication struct {
	deps ReadDeps
}

// NewRead constructs the cold-read application.
func NewRead(deps ReadDeps) *ReadApplication {
	return &ReadApplication{deps: deps}
}

// Read loads record and transcript concurrently, then history when needed.
func (application *ReadApplication) Read(sessionID string) (ReadResult, error) {
	result := ReadResult{
		PendingTailNotice: application.deps.RecoverTail(sessionID),
		Location:          application.deps.Locate(sessionID),
	}
	var historyErr, transcriptErr, recordErr error
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		result.Record, result.RecordTotal, result.HasRecord, recordErr =
			application.deps.LoadRecord(result.Location, sessionID)
	}()
	go func() {
		defer group.Done()
		result.Transcript, transcriptErr = application.deps.LoadTranscript(result.Location, sessionID)
	}()
	group.Wait()
	if recordErr != nil {
		return ReadResult{}, fmt.Errorf("load session record %q: %w", sessionID, recordErr)
	}
	if !result.HasRecord {
		result.History, result.HistoryTotal, historyErr = application.deps.LoadHistory(result.Location)
	}
	if historyErr != nil && !result.HasRecord {
		return ReadResult{}, fmt.Errorf("load session %q: %w", sessionID, historyErr)
	}
	if historyErr != nil {
		result.History = nil
	}
	if transcriptErr != nil && !result.HasRecord {
		return ReadResult{}, fmt.Errorf("load session transcript %q: %w", sessionID, transcriptErr)
	}
	return result, nil
}
