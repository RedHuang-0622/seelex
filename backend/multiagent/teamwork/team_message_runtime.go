package teamwork

import (
	"context"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// TeammateConversationRef resolves the only valid conversation window for a
// teammate: the Session owned by one concrete WorkItem. role is an ownership
// check, never a fallback window. Resolving the ref is read-only.
func (c *Coordinator) TeammateConversationRef(ctx context.Context, role, itemID string) (dto.ConversationSessionRef, error) {
	role = strings.TrimSpace(role)
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return dto.ConversationSessionRef{}, fmt.Errorf("team_message: item is required（teammate 的对话窗口是 WorkItem Session）")
	}
	if role == "" {
		return dto.ConversationSessionRef{}, fmt.Errorf("team_message: role is required")
	}

	c.lockPlan()
	defer c.unlockPlan()
	plan, err := c.st.ReadPlan(ctx, c.key)
	if err != nil {
		return dto.ConversationSessionRef{}, err
	}
	if err := requireTeam(plan); err != nil {
		return dto.ConversationSessionRef{}, err
	}
	_, item, ok := findItem(&plan, itemID)
	if !ok {
		return dto.ConversationSessionRef{}, fmt.Errorf("team_message: 计划里没有 WorkItem %q", itemID)
	}
	if owner := strings.TrimSpace(item.Role); owner != role {
		return dto.ConversationSessionRef{}, fmt.Errorf(
			"team_message: WorkItem %q 属于 teammate %q，不是 %q",
			item.ID, owner, role,
		)
	}
	sessionID := strings.TrimSpace(item.SessionID)
	if sessionID == "" {
		return dto.ConversationSessionRef{}, fmt.Errorf(
			"team_message: WorkItem %q 还没有自己的 Session；先派发它再发起对话",
			item.ID,
		)
	}
	return dto.ConversationSessionRef{
		Kind:          dto.ConversationTeammate,
		SessionID:     sessionID,
		MainSessionID: c.key.SessionID,
		TeamID:        plan.TeamID,
		Role:          role,
		WorkItemID:    item.ID,
	}, nil
}
