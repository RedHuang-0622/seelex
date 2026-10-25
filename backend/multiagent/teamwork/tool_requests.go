package teamwork

// tool_requests.go 是 leader 编排工具的**入参形状与解码**（P21-37「域方法随类型走」：从
// `backend/engine/assemble/runtime_teamwork.go` 的十个 handler 里搬进本域）。
//
// 为什么值得搬：工具 schema 已经在 `tool_shapes.go`（域包），而 handler 里那份匿名结构体是
// **同一份请求形状的第二份描述**——schema 改了、解析结构没跟上，模型照 schema 发的东西就会被
// 静默丢掉。现在 schema 与解析结构同处一域。错误文案（`team_x: 参数解析失败: %w`）仍留在
// 装配层：那是工具面的话术，不是域事实。

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/jobs"

	"github.com/RedHuang-0622/seelex/store"
)

// PlanRequest 是 `team_plan` 的入参形状（与 `TeamPlanSchema` 一一对应）。
type PlanRequest struct {
	TeamID     string                    `json:"team_id"`
	Version    int                       `json:"version"`
	Members    []store.TeamworkMember    `json:"members"`
	Milestones []store.TeamworkMilestone `json:"milestones"`
}

// DecodePlanRequest 解析 `team_plan` 入参并补默认值（version 缺省 1；其余字段原样）。
func DecodePlanRequest(argsJSON string) (store.TeamworkPlan, error) {
	var raw PlanRequest
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return store.TeamworkPlan{}, err
	}
	if raw.Version == 0 {
		raw.Version = 1
	}
	return store.TeamworkPlan{
		TeamID: raw.TeamID, Version: raw.Version,
		Members: raw.Members, Milestones: raw.Milestones,
	}, nil
}

// DispatchRequest 是 `team_dispatch` 的入参形状（`item` 给了就按 Work Item 派发）。
type DispatchRequest struct {
	Role      string `json:"role"`
	Item      string `json:"item"`
	Goal      string `json:"goal"`
	Workspace string `json:"workspace"`
}

// DecodeDispatchRequest 解析 `team_dispatch` 入参：role / item trim；goal **原样**
// （它是这一轮的正文，首尾空白由模型决定，不在这里改写）。
func DecodeDispatchRequest(argsJSON string) (DispatchRequest, error) {
	var raw DispatchRequest
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return DispatchRequest{}, err
	}
	raw.Role = strings.TrimSpace(raw.Role)
	raw.Item = strings.TrimSpace(raw.Item)
	raw.Workspace = strings.ToLower(strings.TrimSpace(raw.Workspace))
	switch WorkspaceMode(raw.Workspace) {
	case "", WorkspaceWorktree, WorkspaceMain:
	default:
		return DispatchRequest{}, &UnknownWorkspaceError{Value: raw.Workspace}
	}
	return raw, nil
}

// UnknownWorkspaceError 是 workspace 入参的唯一词法错误。
type UnknownWorkspaceError struct{ Value string }

func (e *UnknownWorkspaceError) Error() string {
	return "workspace=" + e.Value + " 未知；只允许 worktree 或 main"
}

// WorkRequest 是 `team_work` 的入参形状（分里程碑排活）。
type WorkRequest struct {
	Milestone string `json:"milestone"`
	Items     []struct {
		ID          string   `json:"id"`
		Role        string   `json:"role"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Goal        string   `json:"goal"`
		DependsOn   []string `json:"depends_on"`
	} `json:"items"`
}

// DecodeWorkRequest 解析 `team_work` 入参：里程碑 trim，工作项原样搬成规格。
func DecodeWorkRequest(argsJSON string) (string, []WorkItemSpec, error) {
	var raw WorkRequest
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return "", nil, err
	}
	specs := make([]WorkItemSpec, 0, len(raw.Items))
	for _, item := range raw.Items {
		specs = append(specs, WorkItemSpec{
			ID: item.ID, Role: item.Role, Name: item.Name,
			Description: item.Description, Goal: item.Goal, DependsOn: item.DependsOn,
		})
	}
	return strings.TrimSpace(raw.Milestone), specs, nil
}

// ItemRequest 是 `team_item` 的入参形状（调整一条尚未开始的工作项）。
type ItemRequest struct {
	ID          string   `json:"id"`
	Role        string   `json:"role"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Goal        string   `json:"goal"`
	DependsOn   []string `json:"depends_on"`
}

// DecodeItemRequest 解析 `team_item` 入参：id trim，其余字段搬成规格。
func DecodeItemRequest(argsJSON string) (string, WorkItemSpec, error) {
	var raw ItemRequest
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return "", WorkItemSpec{}, err
	}
	return strings.TrimSpace(raw.ID), WorkItemSpec{
		Role: raw.Role, Name: raw.Name, Description: raw.Description,
		Goal: raw.Goal, DependsOn: raw.DependsOn,
	}, nil
}

// ItemNoteRequest 是 `team_accept` / `team_fail` 共用的入参形状（id + 处置说明）。
type ItemNoteRequest struct {
	ID   string `json:"id"`
	Note string `json:"note"`
}

// DecodeItemNoteRequest 解析验收/判负入参：id trim；note 原样（那是给 leader 看的原文）。
func DecodeItemNoteRequest(argsJSON string) (string, string, error) {
	var raw ItemNoteRequest
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return "", "", err
	}
	return strings.TrimSpace(raw.ID), raw.Note, nil
}

// JoinRequest 是 `team_join` 的入参形状。
type JoinRequest struct {
	Handles  []string `json:"handles"`
	BudgetMS int      `json:"budget_ms"`
}

// JoinDefaultBudget 是 `team_join` 未给预算时的等待上限（5s）。
const JoinDefaultBudget = 5 * time.Second

// DecodeJoinRequest 解析汇合入参：句柄 trim 并丢弃空白项；预算缺省（<=0）取 5s。
func DecodeJoinRequest(argsJSON string) ([]jobs.Handle, time.Duration, error) {
	var raw JoinRequest
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return nil, 0, err
	}
	handles := make([]jobs.Handle, 0, len(raw.Handles))
	for _, handle := range raw.Handles {
		if trimmed := strings.TrimSpace(handle); trimmed != "" {
			handles = append(handles, jobs.Handle(trimmed))
		}
	}
	budget := time.Duration(raw.BudgetMS) * time.Millisecond
	if budget <= 0 {
		budget = JoinDefaultBudget
	}
	return handles, budget, nil
}

// MilestoneRequest 是 `team_milestone` 的入参形状（给里程碑写交付说明）。
type MilestoneRequest struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

// DecodeMilestoneRequest 解析里程碑入参：id trim，content 原样。
func DecodeMilestoneRequest(argsJSON string) (string, string, error) {
	var raw MilestoneRequest
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return "", "", err
	}
	return strings.TrimSpace(raw.ID), raw.Content, nil
}
