package core

import (
	"strings"

	"github.com/RedHuang-0622/seelex/seelebridge"
)

// HandleSubagentToolEvent projects a Runtime tool dispatch into the bounded
// authoritative Plan node snapshot and publishes one frontend incremental.
func (service *Service) HandleSubagentToolEvent(event seelebridge.SubagentToolEvent) {
	if strings.TrimSpace(event.NodeID) == "" || strings.TrimSpace(event.ID) == "" {
		return
	}
	projected := SubagentToolEvent{
		ID:        event.ID,
		NodeID:    event.NodeID,
		Name:      event.Name,
		Arguments: truncateSubagentEvidence(event.Arguments),
		Result:    truncateSubagentEvidence(event.Result),
		Error:     truncateSubagentEvidence(event.Error),
		Status:    event.Status,
		StartedAt: event.StartedAt,
		Duration:  event.Duration,
	}

	service.mu.Lock()
	plan := service.snapshot.Runtime.Plan
	if plan == nil {
		service.mu.Unlock()
		return
	}
	node := findPlanNodeByID(plan.Nodes, event.NodeID)
	if node == nil {
		service.mu.Unlock()
		return
	}
	upsertSubagentToolEvent(node, projected)
	revision := service.bumpLocked()
	requestID := service.snapshot.Chat.RequestID
	service.mu.Unlock()

	kind := EventSubagentToolCompleted
	if projected.Status == "running" {
		kind = EventSubagentToolStarted
	}
	service.events.Publish(kind, revision, requestID, projected)
}

func upsertSubagentToolEvent(node *PlanNode, event SubagentToolEvent) {
	for index := range node.ToolEvents {
		if node.ToolEvents[index].ID == event.ID {
			node.ToolEvents[index] = event
			return
		}
	}
	node.ToolEvents = append(node.ToolEvents, event)
	if limit := Limits().PlanNodeEvents; limit > 0 && len(node.ToolEvents) > limit {
		node.ToolEvents = append([]SubagentToolEvent(nil), node.ToolEvents[len(node.ToolEvents)-limit:]...)
	}
}

func truncateSubagentEvidence(value string) string {
	limit := Limits().EvidenceChars
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

func findPlanNodeByID(nodes []PlanNode, nodeID string) *PlanNode {
	for index := range nodes {
		if nodes[index].ID == nodeID {
			return &nodes[index]
		}
		if node := findPlanNodeByID(nodes[index].Children, nodeID); node != nil {
			return node
		}
	}
	return nil
}

func clonePlanNode(node PlanNode) PlanNode {
	cloned := cloneRuntimeState(RuntimeState{Plan: &PlanState{Nodes: []PlanNode{node}}})
	return cloned.Plan.Nodes[0]
}

func subagentChangedPayload(plan *PlanState, planID, runID string, node PlanNode) SubagentEvent {
	return SubagentEvent{
		PlanID: planID, RunID: runID, NodeID: node.ID, Node: clonePlanNode(node),
		PlanStatus: plan.Status, Progress: plan.Progress,
	}
}
