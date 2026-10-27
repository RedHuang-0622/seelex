package assembly

import (
	applicationtoolrecord "github.com/RedHuang-0622/seelex/application/execution/toolrecord"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

const (
	toolRecordDropNoOwner = applicationtoolrecord.DropNoOwner
	toolRecordDropNoUnit  = applicationtoolrecord.DropNoUnit
)

func (service *Service) stageUnitToolOutcome(sessionID, unitID, tool string, failed bool, errorText string) {
	service.toolRecords.StageUnitOutcome(sessionID, unitID, tool, failed, errorText)
}

func (service *Service) commitUnitToolRecord(sessionID, unitID string, seq uint64, stats []dto.ToolRecordStat) {
	service.toolRecords.CommitUnit(sessionID, unitID, seq, stats)
}

func (service *Service) closeUnitToolRecord(sessionID, unitID string) {
	service.toolRecords.CloseUnit(sessionID, unitID)
}

func (service *Service) closeSubagentToolRecordUnit(ownerSessionID, nodeID string) {
	service.toolRecords.CloseSubagentUnit(ownerSessionID, nodeID)
}

func (service *Service) toolRecordDroppedReadings() map[string]int {
	return service.toolRecords.Diagnostics().DroppedReadings
}

func (service *Service) stageUnitToolOutcomeForStatus(ownerSessionID, unitID, name, status, errorText string) {
	service.toolRecords.StageStatus(ownerSessionID, unitID, name, status, errorText)
}

func (service *Service) stageSubagentToolOutcome(ownerSessionID string, event dto.SubagentToolEvent) {
	service.toolRecords.StageSubagentEvent(ownerSessionID, event)
}

func (service *Service) closeTerminalSubagentToolRecords(nodes []dto.SubAgentTreeNode) {
	service.toolRecords.CloseTerminalSubagents(nodes)
}

func (service *Service) recordToolOutcome(sessionID, tool string, failed bool, errorText string) {
	service.toolRecords.RecordOutcome(sessionID, tool, failed, errorText)
}
