package toolrecord

import (
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// StageToolNameLimit bounds the number of named tools retained by one leaf
// stage. Counts never disappear; excess tool names fold into StageOverflowTool.
const StageToolNameLimit = 256

// StageOverflowTool receives tool names beyond StageToolNameLimit.
const StageOverflowTool = "other"

// Stage is one execution unit's local tool-outcome staging area. It does not
// touch session locks, views or storage.
type Stage struct {
	mu        sync.Mutex
	unitID    string
	sessionID string
	acc       *Accumulator
	distinct  int
	seq       uint64
}

// NewStage constructs an execution-unit staging area.
func NewStage(unitID, sessionID string) *Stage {
	return &Stage{
		unitID:    strings.TrimSpace(unitID),
		sessionID: strings.TrimSpace(sessionID),
		acc:       NewAccumulator(),
	}
}

// UnitID returns the stable execution-unit key.
func (stage *Stage) UnitID() string {
	if stage == nil {
		return ""
	}
	return stage.unitID
}

// SessionID returns the owner session captured by the execution context.
func (stage *Stage) SessionID() string {
	if stage == nil {
		return ""
	}
	return stage.sessionID
}

// Record accumulates one completed tool call in the local stage.
func (stage *Stage) Record(tool string, ok bool, reason string) {
	if stage == nil {
		return
	}
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return
	}
	stage.mu.Lock()
	defer stage.mu.Unlock()
	if _, seen := stage.acc.Stat(tool); !seen && stage.distinct >= StageToolNameLimit {
		tool = StageOverflowTool
	}
	if _, seen := stage.acc.Stat(tool); !seen {
		stage.distinct++
	}
	stage.acc.Record(tool, ok, reason)
}

// TakeForCommit synchronously returns this window's delta and clears the
// stage. A no-op return leaves seq unchanged, preserving idempotent close.
func (stage *Stage) TakeForCommit() (uint64, []dto.ToolRecordStat) {
	if stage == nil {
		return 0, nil
	}
	stage.mu.Lock()
	defer stage.mu.Unlock()
	snapshot, dirty := stage.acc.Snapshot()
	if !dirty {
		return 0, nil
	}
	stage.seq++
	stage.acc = NewAccumulator()
	stage.distinct = 0
	return stage.seq, snapshot.Tools
}
