package query

import (
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// SessionRuntimeOf extracts session-owned runtime facts from the full runtime.
func SessionRuntimeOf(runtime model.RuntimeState) model.SessionRuntime {
	return model.SessionRuntime{
		Effort:           runtime.Effort,
		FullAccess:       runtime.FullAccess,
		PermissionTier:   runtime.PermissionTier,
		Tokens:           runtime.Tokens,
		Replan:           runtime.Replan,
		Plan:             model.CloneRuntimeState(model.RuntimeState{Plan: runtime.Plan}).Plan,
		TodoItems:        append([]dto.TodoItem(nil), runtime.TodoItems...),
		SubAgentTree:     append([]dto.SubAgentTreeNode(nil), runtime.SubAgentTree...),
		GoalSkillActive:  runtime.GoalSkillActive,
		GoalGovernance:   CloneGoalGovernanceView(runtime.GoalGovernance),
		ActiveSkills:     append([]string(nil), runtime.ActiveSkills...),
		WorkTable:        model.CloneWorkItems(runtime.WorkTable),
		WorkTableBatches: model.CloneWorkTableBatches(runtime.WorkTableBatches),
	}
}

// CloneGoalGovernanceView detaches the mutable governance history slice.
func CloneGoalGovernanceView(view *dto.GoalGovernanceView) *dto.GoalGovernanceView {
	if view == nil {
		return nil
	}
	clone := *view
	clone.History = append([]dto.GoalHistoryView(nil), view.History...)
	return &clone
}
