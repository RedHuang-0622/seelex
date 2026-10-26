package core

// tool_record.go keeps the Service compatibility surface. The tool-record state
// machine lives in application/execution/toolrecord.

import (
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// SetToolRecordEnabled switches the tool-record ledger after persisting the setting.
func (service *Service) SetToolRecordEnabled(enabled bool) (bool, error) {
	if service == nil || service.toolRecords == nil {
		return false, nil
	}
	return service.toolRecords.SetEnabled(enabled)
}

// ApplyToolRecordEnabled installs the startup value without rewriting storage.
func (service *Service) ApplyToolRecordEnabled(enabled bool) {
	if service == nil || service.toolRecords == nil {
		return
	}
	service.toolRecords.ApplyEnabled(enabled)
}

// ToolRecordEnabled reports the live switch.
func (service *Service) ToolRecordEnabled() bool {
	if service == nil || service.toolRecords == nil {
		return false
	}
	return service.toolRecords.Enabled()
}

// FlushToolRecord writes one dirty session snapshot after draining leaf stages.
func (service *Service) FlushToolRecord(sessionID string) {
	if service == nil || service.toolRecords == nil {
		return
	}
	service.toolRecords.Flush(sessionID)
}

// toolRecordWritableSession is the host's draft-versus-materialized predicate.
func (service *Service) toolRecordWritableSession(sessionID string) bool {
	if service == nil || service.Core == nil {
		return true
	}
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	return !service.draftTargetLocked().Matches(sessionID)
}

// SetToolRecordSnapshotStore injects or clears the per-session snapshot port.
func (service *Service) SetToolRecordSnapshotStore(store contract.ToolRecordSnapshotStore) {
	if service == nil || service.toolRecords == nil {
		return
	}
	service.toolRecords.SetSnapshotStore(store)
}

// flushAllToolRecords writes every in-memory session as a shutdown fallback.
func (service *Service) flushAllToolRecords() {
	if service == nil || service.toolRecords == nil {
		return
	}
	service.toolRecords.FlushAll()
}

// ToolRecordBoard returns the current-view session projection.
func (service *Service) ToolRecordBoard() dto.ToolRecordBoard {
	if service == nil || service.toolRecords == nil {
		return dto.ToolRecordBoard{}
	}
	return service.toolRecords.Board()
}

// RecordToolCommandOutcome records the command-exit-code success criterion.
func (service *Service) RecordToolCommandOutcome(sessionID, tool string, exitCode int) {
	if service == nil || service.toolRecords == nil {
		return
	}
	service.toolRecords.RecordCommandOutcome(sessionID, tool, exitCode)
}

// recordToolOutcome is the single main-agent outcome forwarding entry.
func (service *Service) recordToolOutcome(sessionID, tool string, failed bool, errorText string) {
	if service == nil || service.toolRecords == nil {
		return
	}
	service.toolRecords.RecordOutcome(sessionID, tool, failed, errorText)
}
