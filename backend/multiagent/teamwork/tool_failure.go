package teamwork

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

// ToolFailurePolicy 是 WorkItem 连续显式工具失败返回策略。默认关闭；阈值只
// 约束显式工具错误（执行失败、超时、权限拒绝、非零退出），正常 result 内的业务
// 错误不是工具失败。
type ToolFailurePolicy struct {
	Enabled   bool
	Threshold int
}

// WorkItemToolFailureError 是交还 leader 的结构化原因。它不包含自动回滚：
// Worktree、Session 与已有产出都保留。
type WorkItemToolFailureError struct {
	WorkItemID string
	Threshold  int
	Tool       string
	Cause      error
}

func (e *WorkItemToolFailureError) Error() string {
	if e == nil {
		return ""
	}
	cause := ""
	if e.Cause != nil {
		cause = "；最后一次错误：" + e.Cause.Error()
	}
	return fmt.Sprintf(
		"workitem_tool_failure: WorkItem %q 连续 %d 次显式工具失败，worker 已停止%s。"+
			"worktree、Session 与已有产出均保留，不自动 git reset 或删除 worktree。请使用 team_message(item=%q) 询问失败原因，"+
			"再由 leader 决定 team_accept / team_fail / 重派",
		e.WorkItemID, e.Threshold, cause, e.WorkItemID,
	)
}

func (e *WorkItemToolFailureError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// IsWorkItemToolFailure is the single classification used by the worker
// executor to hand control back to the leader without settling the WorkItem.
func IsWorkItemToolFailure(err error) bool {
	var target *WorkItemToolFailureError
	return errors.As(err, &target)
}

// WorkItemToolFailureTracker 统计一个 WorkItem 当前 worker 回合内的连续失败。
// 成功调用清零；关闭策略时不做任何计数。
type WorkItemToolFailureTracker struct {
	mu        sync.Mutex
	policy    ToolFailurePolicy
	itemID    string
	failures  int
	lastTool  string
	lastCause error
	stopped   bool
}

func NewWorkItemToolFailureTracker(itemID string, policy ToolFailurePolicy) *WorkItemToolFailureTracker {
	threshold := policy.Threshold
	if threshold <= 0 {
		threshold = 3
	}
	policy.Threshold = threshold
	return &WorkItemToolFailureTracker{policy: policy, itemID: strings.TrimSpace(itemID)}
}

// Observe 记录一次工具收口。返回 true 表示本次调用达到阈值，调用方应立即
// 取消当前回合。toolErr == nil 视为成功并清零。
func (t *WorkItemToolFailureTracker) Observe(tool string, toolErr error) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.policy.Enabled {
		return false
	}
	if toolErr == nil {
		t.failures = 0
		t.lastTool = ""
		t.lastCause = nil
		return false
	}
	t.failures++
	t.lastTool = strings.TrimSpace(tool)
	t.lastCause = toolErr
	if t.failures >= t.policy.Threshold {
		t.stopped = true
		return true
	}
	return false
}

func (t *WorkItemToolFailureTracker) ConsecutiveFailures() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.policy.Enabled {
		return 0
	}
	return t.failures
}

func (t *WorkItemToolFailureTracker) Stopped() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stopped
}

func (t *WorkItemToolFailureTracker) Error() error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.policy.Enabled || !t.stopped {
		return nil
	}
	return &WorkItemToolFailureError{
		WorkItemID: t.itemID, Threshold: t.policy.Threshold,
		Tool: t.lastTool, Cause: t.lastCause,
	}
}
