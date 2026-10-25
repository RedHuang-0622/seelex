package assemble

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/backend/multiagent/teamwork"
	"github.com/RedHuang-0622/seelex/backend/workunit"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

var _ contract.WorkItemExecutionPort = (*Runtime)(nil)

// WorkItemExecution is the one backend adapter used by both WorkItem details
// and the teammate WorkItem progress page.
func (r *Runtime) WorkItemExecution(ctx context.Context, mainSessionID, itemID string, page dto.ConversationViewRequest) (dto.WorkItemExecutionView, error) {
	mainSessionID = strings.TrimSpace(mainSessionID)
	itemID = strings.TrimSpace(itemID)
	if mainSessionID == "" || itemID == "" {
		return dto.WorkItemExecutionView{}, errors.New("workitem execution: main session and item are required")
	}
	board := r.TeamworkBoardSnapshot(mainSessionID)
	if board == nil {
		return dto.WorkItemExecutionView{}, fmt.Errorf("workitem execution: team %q has no plan", mainSessionID)
	}
	var workItem dto.TeamworkWorkItemView
	found := false
	for _, candidate := range board.WorkItems {
		if strings.TrimSpace(candidate.ID) == itemID {
			workItem = candidate
			found = true
			break
		}
	}
	if !found {
		return dto.WorkItemExecutionView{}, fmt.Errorf("workitem execution: item %q is not in the team plan", itemID)
	}
	progress := dto.WorkItemProgressView{}
	reader := r.teamUnitReader(mainSessionID, r.teamSceneIndex(mainSessionID))
	record, hasRecord, err := reader.Record(teamwork.SceneNodeID(workItem.Role, workItem.ID))
	if err != nil {
		return dto.WorkItemExecutionView{}, err
	}
	if hasRecord {
		progress = workItemProgressView(workunit.ProgressOf(workunit.KindTeammate, record))
	}
	sessionPage := dto.SessionRecordPage{
		SessionID: workItem.SessionID,
		Recovery:  dto.RecoveryMissing,
		Records:   []dto.ConversationRecord{},
	}
	if strings.TrimSpace(workItem.SessionID) != "" {
		sessionPage, err = r.ViewAgentConversation(ctx, dto.ConversationSessionRef{
			Kind: dto.ConversationTeammate, SessionID: workItem.SessionID,
			MainSessionID: mainSessionID, TeamID: board.TeamID, Role: workItem.Role, WorkItemID: workItem.ID,
		}, page)
		if err != nil {
			return dto.WorkItemExecutionView{}, err
		}
	}
	return dto.WorkItemExecutionView{
		WorkItem: workItem,
		Progress: progress,
		Session:  sessionPage,
	}, nil
}

func workItemProgressView(progress workunit.Progress) dto.WorkItemProgressView {
	stages := make([]dto.StageView, 0, len(progress.Stages))
	for _, stage := range progress.Stages {
		stages = append(stages, dto.StageView{Stage: stage.Stage, Preview: stage.Preview})
	}
	updated := ""
	if !progress.UpdatedAt.IsZero() {
		updated = progress.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return dto.WorkItemProgressView{
		Kind: string(progress.Kind), NodeID: progress.NodeID, SessionID: progress.SessionID,
		Goal: progress.Goal, Status: progress.Status, InFlight: progress.InFlight,
		Summary: progress.Summary, Error: progress.Error, Stages: stages,
		Worktree: progress.Worktree.Path, UpdatedAt: updated,
	}
}
