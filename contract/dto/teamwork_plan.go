package dto

import "strings"

// TeamworkPlan 是 leader 的硬编排计划，也是 Store 持久化头载荷的稳定 DTO。
//
// 顺序事实只有两层：里程碑之间的 DependsOn 屏障，以及里程碑内部工作项的
// DependsOn DAG。字段与 JSON tag 是既有持久化格式，不得在分层迁移中改变。
type TeamworkPlan struct {
	TeamID     string              `json:"team_id"`
	Version    int                 `json:"version"`
	Members    []TeamworkMember    `json:"members"`
	Milestones []TeamworkMilestone `json:"milestones,omitempty"`
	State      TeamworkState       `json:"state,omitempty"`
}

// TeamworkMember 是一个 teammate 的在编条目。
type TeamworkMember struct {
	Role          string         `json:"role"`
	RoleSessionID string         `json:"role_session_id"`
	Worktree      string         `json:"worktree,omitempty"`
	ToolsPolicy   string         `json:"tools_policy,omitempty"`
	Permission    map[string]int `json:"permission_groups,omitempty"`
	Plugins       []string       `json:"plugins,omitempty"`
	MaxTurns      int            `json:"max_turns,omitempty"`
}

// TeamworkMilestone 是里程碑屏障及其内部工作项声明。
type TeamworkMilestone struct {
	ID        string             `json:"id"`
	Name      string             `json:"name,omitempty"`
	DependsOn []string           `json:"depends_on,omitempty"`
	Required  []string           `json:"required,omitempty"`
	Status    string             `json:"status,omitempty"`
	Content   string             `json:"content,omitempty"`
	Items     []TeamworkWorkItem `json:"items,omitempty"`
}

// TeamworkWorkItem 是里程碑内的调度单位（甘特图节点）。
type TeamworkWorkItem struct {
	ID            string   `json:"id"`
	Milestone     string   `json:"milestone"`
	Role          string   `json:"role"`
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	Goal          string   `json:"goal,omitempty"`
	DependsOn     []string `json:"depends_on,omitempty"`
	Status        string   `json:"status,omitempty"`
	SessionID     string   `json:"session_id,omitempty"`
	Worktree      string   `json:"worktree,omitempty"`
	WorkspaceMode string   `json:"workspace_mode,omitempty"`
	Handle        string   `json:"handle,omitempty"`
	Note          string   `json:"note,omitempty"`
	StartedAt     int64    `json:"started_at,omitempty"`
	FinishedAt    int64    `json:"finished_at,omitempty"`
}

// TeamworkState 是计划的运行态投影；句柄只是内存快照，不是跨进程事实源。
type TeamworkState struct {
	Jobs         map[string]string `json:"jobs,omitempty"`
	Milestones   map[string]string `json:"milestones,omitempty"`
	Unmerged     map[string]string `json:"unmerged,omitempty"`
	State        string            `json:"state,omitempty"`
	ClosedAt     int64             `json:"closed_at,omitempty"`
	ClosedReason string            `json:"closed_reason,omitempty"`
}

const (
	TeamworkRoleMain = "main"
	TeamworkRoleUser = "user"
)

const (
	TeamworkMilestonePending = "pending"
	TeamworkMilestoneActive  = "active"
	TeamworkMilestoneDone    = "done"
)

const (
	TeamworkItemPending = "pending"
	TeamworkItemRunning = "running"
	TeamworkItemReview  = "review"
	TeamworkItemDone    = "done"
	TeamworkItemFailed  = "failed"
)

const TeamworkStateClosed = "closed"

// TeamworkPlanIsEmpty 报告计划尚未声明团队（TeamID 为空）。
func TeamworkPlanIsEmpty(plan TeamworkPlan) bool {
	return strings.TrimSpace(plan.TeamID) == ""
}

// TeamworkWorkItemStatusOrPending 把空状态读成 pending。
func TeamworkWorkItemStatusOrPending(item TeamworkWorkItem) string {
	if strings.TrimSpace(item.Status) == "" {
		return TeamworkItemPending
	}
	return item.Status
}

// TeamworkWorkItemTerminal 报告工作项是否已有 done/failed 结论。
func TeamworkWorkItemTerminal(item TeamworkWorkItem) bool {
	switch TeamworkWorkItemStatusOrPending(item) {
	case TeamworkItemDone, TeamworkItemFailed:
		return true
	default:
		return false
	}
}
