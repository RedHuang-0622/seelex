package teamwork

import (
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/store"
)

// MilestoneClosed 是「里程碑已收口」的唯一判据。done 是终态：允许只读查看，
// 不再允许排活、调整其下工作项或重派；追加工作必须新建里程碑或显式 replan。
func MilestoneClosed(plan store.TeamworkPlan, milestoneID string) bool {
	id := strings.TrimSpace(milestoneID)
	if id == "" {
		return false
	}
	for _, milestone := range plan.Milestones {
		if strings.TrimSpace(milestone.ID) == id {
			return milestone.Status == store.TeamworkMilestoneDone
		}
	}
	return false
}

func closedMilestoneError(milestoneID string) error {
	return fmt.Errorf(
		"teamwork: 里程碑 %q 已收口；已收口里程碑不可重新排活或派发。"+
			"如需追加工作，请创建新的里程碑或显式 replan",
		strings.TrimSpace(milestoneID),
	)
}

// AcceptedWithCleanupPendingError 表示 WorkItem 已经完成、作业行也已经结清，
// 但该 WorkItem 的 worktree 尚未释放。WorkItem/Session/已有产出全部保留；再次
// AcceptItem 或 Close 可以幂等重试清理。
type AcceptedWithCleanupPendingError struct {
	ItemID string
	Err    error
}

func (e *AcceptedWithCleanupPendingError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("teamwork: 工作项 %q 已验收，但 worktree 清理仍待重试: %v", e.ItemID, e.Err)
}

func (e *AcceptedWithCleanupPendingError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// IsAcceptedWithCleanupPending 是上述中间态的唯一判据。
func IsAcceptedWithCleanupPending(err error) bool {
	var target *AcceptedWithCleanupPendingError
	return errors.As(err, &target)
}
