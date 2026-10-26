package core

// tool_record.go keeps only the host adapter around the tool-record state
// machine in application/execution/toolrecord.

// toolRecordWritableSession is the host's draft-versus-materialized predicate.
func (service *Service) toolRecordWritableSession(sessionID string) bool {
	if service == nil || service.Core == nil {
		return true
	}
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	return !service.draftTargetLocked().Matches(sessionID)
}

// flushAllToolRecords writes every in-memory session as a shutdown fallback.
func (service *Service) flushAllToolRecords() {
	if service == nil || service.toolRecords == nil {
		return
	}
	service.toolRecords.FlushAll()
}

func (service *Service) flushToolRecord(sessionID string) {
	if service == nil || service.toolRecords == nil {
		return
	}
	service.toolRecords.Flush(sessionID)
}

// recordToolOutcome is the single main-agent outcome forwarding entry.
func (service *Service) recordToolOutcome(sessionID, tool string, failed bool, errorText string) {
	if service == nil || service.toolRecords == nil {
		return
	}
	service.toolRecords.RecordOutcome(sessionID, tool, failed, errorText)
}
