package teamwork

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/jobs"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// TeammateMessageDispatch is the handle and exact worker payload for one
// bounded team_message turn. The job is started before this value is returned;
// the unified ConversationSession adapter waits with Peek and never cancels it
// on a read timeout.
type TeammateMessageDispatch struct {
	Handle  jobs.Handle
	Request WorkerRequest
}

// DeclareTeammateMessage registers and starts one dialogue-only worker job.
// It participates in max_jobs and jobs_manage lifecycle exactly like other
// teammate work, but it does not write WorkItem state, bind a worktree, or call
// SettleWorkItem.
func (c *Coordinator) DeclareTeammateMessage(
	ctx context.Context,
	ref dto.ConversationSessionRef,
	message string,
	systemPrompt string,
) (TeammateMessageDispatch, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return TeammateMessageDispatch{}, errors.New("team_message: message is required")
	}
	resolved, err := c.TeammateConversationRef(ctx, ref.Role, ref.WorkItemID)
	if err != nil {
		return TeammateMessageDispatch{}, err
	}
	if sessionID := strings.TrimSpace(ref.SessionID); sessionID != "" && sessionID != resolved.SessionID {
		return TeammateMessageDispatch{}, fmt.Errorf(
			"team_message: WorkItem %q 使用 Session %q，不是 %q",
			resolved.WorkItemID, resolved.SessionID, sessionID,
		)
	}

	c.lockPlan()
	defer c.unlockPlan()
	plan, err := c.st.ReadPlan(ctx, c.key)
	if err != nil {
		return TeammateMessageDispatch{}, err
	}
	if err := requireTeam(plan); err != nil {
		return TeammateMessageDispatch{}, err
	}
	_, item, ok := findItem(&plan, resolved.WorkItemID)
	if !ok {
		return TeammateMessageDispatch{}, fmt.Errorf("team_message: 计划里没有 WorkItem %q", resolved.WorkItemID)
	}
	member, ok := memberFor(plan, item.Role)
	if !ok {
		return TeammateMessageDispatch{}, fmt.Errorf("team_message: teammate %q 不在编", item.Role)
	}
	if err := c.ensureCapacity(item.Role); err != nil {
		return TeammateMessageDispatch{}, err
	}
	groups, err := memberPermissionGroups(member)
	if err != nil {
		return TeammateMessageDispatch{}, err
	}
	request := WorkerRequest{
		MainSessionID:    c.key.SessionID,
		TeamID:           plan.TeamID,
		Role:             item.Role,
		RoleSessionID:    resolved.SessionID,
		Subject:          SubjectForRole(item.Role),
		ToolsPolicy:      member.ToolsPolicy,
		PermissionGroups: groups,
		Plugins:          member.Plugins,
		WorkItemID:       item.ID,
		Milestone:        item.Milestone,
		DialogueOnly:     true,
		DialogueMessage:  message,
		SystemPrompt:     strings.TrimSpace(systemPrompt),
		MaxTurns:         maxTurnsFor(member, c.maxTurns),
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return TeammateMessageDispatch{}, fmt.Errorf("team_message: 编码对话作业: %w", err)
	}
	handle, err := c.jobs.Declare(ctx, jobs.Spec{
		Kind:        KindWorker,
		Scope:       jobs.Scope{Session: c.key.SessionID, Subject: request.Subject},
		Node:        item.ID,
		Description: fmt.Sprintf("%s/%s: team_message", item.Role, item.ID),
		Payload:     payload,
	})
	if err != nil {
		return TeammateMessageDispatch{}, err
	}
	if err := c.jobs.Start(ctx, handle); err != nil {
		_ = c.jobs.Kill(ctx, handle)
		_ = c.jobs.Done(ctx, handle)
		return TeammateMessageDispatch{}, fmt.Errorf("team_message: 启动对话作业失败: %w", err)
	}
	return TeammateMessageDispatch{Handle: handle, Request: request}, nil
}
