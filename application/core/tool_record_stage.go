package core

// tool_record_stage.go keeps the Service compatibility surface for leaf-to-main
// tool-record commits. The stage state machine lives in toolrecord.

import (
	applicationtoolrecord "github.com/RedHuang-0622/seelex/application/execution/toolrecord"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

const (
	toolRecordDropNoOwner = applicationtoolrecord.DropNoOwner
	toolRecordDropNoUnit  = applicationtoolrecord.DropNoUnit
)

// stageUnitToolOutcome records a leaf result in its execution-unit stage.
func (service *Service) stageUnitToolOutcome(sessionID, unitID, tool string, failed bool, errorText string) {
	if service == nil || service.toolRecords == nil {
		return
	}
	service.toolRecords.StageUnitOutcome(sessionID, unitID, tool, failed, errorText)
}

// commitUnitToolRecord is the idempotent leaf-to-session commit entry.
func (service *Service) commitUnitToolRecord(sessionID, unitID string, seq uint64, stats []dto.ToolRecordStat) {
	if service == nil || service.toolRecords == nil {
		return
	}
	service.toolRecords.CommitUnit(sessionID, unitID, seq, stats)
}

// closeUnitToolRecord synchronously commits one unit's pending stage.
func (service *Service) closeUnitToolRecord(sessionID, unitID string) {
	if service == nil || service.toolRecords == nil {
		return
	}
	service.toolRecords.CloseUnit(sessionID, unitID)
}

// closeSubagentToolRecordUnit closes the stable subagent unit key.
func (service *Service) closeSubagentToolRecordUnit(ownerSessionID, nodeID string) {
	service.closeUnitToolRecord(ownerSessionID, applicationtoolrecord.SubagentUnitID(nodeID, ""))
}

// CloseRoleToolRecordUnit closes the stable role unit key.

// releaseToolRecordSession reclaims all ledger state for a released session.
func (service *Service) releaseToolRecordSession(sessionID string) {
	if service == nil || service.toolRecords == nil {
		return
	}
	service.toolRecords.ReleaseSession(sessionID)
}

// noteDroppedToolRecordReading records a dropped reading in diagnostics.
func (service *Service) noteDroppedToolRecordReading(reason string) {
	if service == nil || service.toolRecords == nil {
		return
	}
	service.toolRecords.NoteDropped(reason)
}

// toolRecordDroppedReadings returns dropped-reading diagnostics.
func (service *Service) toolRecordDroppedReadings() map[string]int {
	if service == nil || service.toolRecords == nil {
		return nil
	}
	return service.toolRecords.Diagnostics().DroppedReadings
}

// stageUnitToolOutcomeForStatus records a parsed teammate result frame.
func (service *Service) stageUnitToolOutcomeForStatus(ownerSessionID, unitID, name, status, errorText string) {
	parsed, ok := dto.ParseToolEventStatus(status)
	if !ok || !applicationtoolrecord.CountsStatus(parsed) {
		return
	}
	service.stageUnitToolOutcome(ownerSessionID, unitID, name, applicationtoolrecord.Failed(parsed), errorText)
}

// stageSubagentToolOutcome records a terminal subagent result frame.
func (service *Service) stageSubagentToolOutcome(ownerSessionID string, event dto.SubagentToolEvent) {
	if !applicationtoolrecord.CountsStatus(event.Status) {
		return
	}
	unitID := applicationtoolrecord.SubagentUnitID(event.NodeID, event.ID)
	service.stageUnitToolOutcome(ownerSessionID, unitID, event.Name, applicationtoolrecord.Failed(event.Status), event.Error)
}

// closeTerminalSubagentToolRecords closes every terminal node's staged reading.
func (service *Service) closeTerminalSubagentToolRecords(nodes []dto.SubAgentTreeNode) {
	for _, node := range nodes {
		if node.ID != "" && applicationtoolrecord.TerminalSubagentStatus(node.Status) {
			service.closeSubagentToolRecordUnit(node.MainSessionID, node.ID)
		}
		service.closeTerminalSubagentToolRecords(node.Children)
	}
}
