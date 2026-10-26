package toolrecord

import "github.com/RedHuang-0622/seelex/contract/dto"

// StageStatus parses and records one teammate tool result frame.
func (application *Application) StageStatus(ownerSessionID, unitID, name, status, errorText string) {
	parsed, ok := dto.ParseToolEventStatus(status)
	if !ok || !CountsStatus(parsed) {
		return
	}
	application.StageUnitOutcome(ownerSessionID, unitID, name, Failed(parsed), errorText)
}

// StageSubagentEvent records one terminal subagent tool result frame.
func (application *Application) StageSubagentEvent(ownerSessionID string, event dto.SubagentToolEvent) {
	if !CountsStatus(event.Status) {
		return
	}
	application.StageUnitOutcome(
		ownerSessionID,
		SubagentUnitID(event.NodeID, event.ID),
		event.Name,
		Failed(event.Status),
		event.Error,
	)
}

// CloseSubagentUnit commits one stable subagent unit key.
func (application *Application) CloseSubagentUnit(ownerSessionID, nodeID string) {
	application.CloseUnit(ownerSessionID, SubagentUnitID(nodeID, ""))
}

// CloseTerminalSubagents commits every terminal subagent node's staged reading.
func (application *Application) CloseTerminalSubagents(nodes []dto.SubAgentTreeNode) {
	for _, node := range nodes {
		if node.ID != "" && TerminalSubagentStatus(node.Status) {
			application.CloseSubagentUnit(node.MainSessionID, node.ID)
		}
		application.CloseTerminalSubagents(node.Children)
	}
}
