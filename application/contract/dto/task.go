// Package dto 承载 application/contract 与 seelebridge 共享的纯 DTO 类型
// （无运行时依赖；contract 依赖它、seelebridge 以 alias 兼容）。
package dto

import (
	"strings"
	"time"
)

// TaskStatus 是 task 生命周期状态。
type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskQueued    TaskStatus = "queued"
	TaskRunning   TaskStatus = "running"
	TaskDoing     TaskStatus = "doing"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskRetry     TaskStatus = "retry"
	// TaskInterrupted 是进程中断/崩溃遗留的工作表条目状态（重启后可见；
	// 由子代理树 interrupted 节点投影而来，供用户在父会话重跑）。
	TaskInterrupted TaskStatus = "interrupted"
)

// TaskPhase* 是 worktable 条目阶段常量（前端筛选/渲染依赖这些字符串）。
const (
	TaskPhasePlan     = "plan"
	TaskPhaseTask     = "task"
	TaskPhaseTasklist = "tasklist"
	TaskPhaseSubagent = "subagent"
)

// TaskTracePoint 是 task 打点（与 worktable trace 同形；evidence 有界）。
type TaskTracePoint struct {
	At        time.Time `json:"at,omitempty"`
	Status    string    `json:"status"`
	Operation string    `json:"operation,omitempty"`
	Evidence  string    `json:"evidence,omitempty"`
	Duration  string    `json:"duration,omitempty"`
}

// TaskRecord 是 task 的只读快照 DTO（字段与 worktable WorkItem 同构）。
type TaskRecord struct {
	ID           string           `json:"id"`                     // plan:<node_id> | subagent:<id> | todo:<n> | task:<n>
	Key          string           `json:"key,omitempty"`          // 幂等键（归一化 goal hash / source id）
	Phase        string           `json:"phase"`                  // 展示派生字段：plan | tasklist | task | subagent（创建时由 Kind 派生）
	Task         string           `json:"task"`                   // goal / label / todo text
	Description  string           `json:"description,omitempty"`  //
	Status       TaskStatus       `json:"status"`                 // pending/running/completed/failed/retry
	RetryCount   int              `json:"retry_count,omitempty"`  // 重试数字
	Assignee     string           `json:"assignee,omitempty"`     // main:<mainSessionID> | subagent:<subagentSessionID>；role:sessionID 被动识别
	Dependencies []string         `json:"dependencies,omitempty"` // 前置任务（WorkItem ID 引用）
	Attachments  []string         `json:"attachments,omitempty"`  // 可选附件路径
	Kind         string           `json:"kind"`                   // 权威类型：plan | todo | task | subagent
	SourceID     string           `json:"source_id,omitempty"`    // 原数据面 ID（详情溯源）
	Participants []string         `json:"participants,omitempty"` // 名单：创建者自动上名单；接管者（role:sessionID）追加并成为当前 Assignee
	BatchID      string           `json:"batch_id,omitempty"`     // 所属批次（发起该批条目的 chat 请求 requestID；空 = 早期/未分批会话）
	CreatedAt    time.Time        `json:"created_at,omitempty"`   // 条目创建时间（批次排序/展示）
	StartedAt    time.Time        `json:"started_at,omitempty"`
	EndedAt      time.Time        `json:"ended_at,omitempty"`
	Elapsed      string           `json:"elapsed,omitempty"`
	Trace        []TaskTracePoint `json:"trace,omitempty"`
}

// TaskSpec 是 task 创建入参（主动 taskadd 或被动生命周期装配）。
type TaskSpec struct {
	ID           string   `json:"id,omitempty"` // 主动留空 → 后端生成 task:<n>
	Key          string   `json:"key,omitempty"`
	Phase        string   `json:"phase"`
	Task         string   `json:"task"`
	Description  string   `json:"description,omitempty"`
	Assignee     string   `json:"assignee,omitempty"`
	Kind         string   `json:"kind"`
	SourceID     string   `json:"source_id,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
	Attachments  []string `json:"attachments,omitempty"`
	// BatchID 所属批次（chat 请求 requestID）。空 → 由注册表默认批次盖章
	// （startChat 写入 SetCurrentTaskBatch）。
	BatchID string `json:"batch_id,omitempty"`
}

// PhaseForKind 由权威类型 Kind 派生展示阶段（Phase 与 Kind 不允许双轨漂移）。
// todo→tasklist、task→task、plan→plan、subagent→subagent；未知类型按原样
// 返回（保守兼容旧数据/扩展类型）。
func PhaseForKind(kind string) string {
	switch kind {
	case "todo":
		return TaskPhaseTasklist
	case "task":
		return TaskPhaseTask
	case "plan":
		return TaskPhasePlan
	case "subagent":
		return TaskPhaseSubagent
	default:
		return kind
	}
}

// ActorIdentity 由执行角色与会话 ID 合成 Assignee/Participants 身份标识。
//
// 识别规则（被动）：系统按实际执行者生成，AI 不提供自由文本。role 与
// sessionID 任一为空时返回空串；否则返回 "<role>:<sessionID>"。
func ActorIdentity(role, sessionID string) string {
	role = strings.TrimSpace(role)
	sessionID = strings.TrimSpace(sessionID)
	if role == "" || sessionID == "" {
		return ""
	}
	return role + ":" + sessionID
}

// TodoItemStatus 是清单项三态（兼容 TUI/旧契约；权威状态在
// TaskRecord.Status。移除窗口：TUI/旧契约调用方迁移到 TaskRecord 后删除，
// 建议随 v0.2 清理批次，2026-12-31）。
type TodoItemStatus string

const (
	TodoItemPending TodoItemStatus = "pending"
	TodoItemDoing   TodoItemStatus = "doing"
	TodoItemDone    TodoItemStatus = "done"
)

// TodoItem 是清单项（快照 DTO，兼容 TUI/旧契约。移除窗口：同
// TodoItemStatus，TUI/旧契约调用方迁移后删除，建议 v0.2，2026-12-31）。
// Status 是权威三态；Done 是派生布尔（Status == done），两者始终一致。
type TodoItem struct {
	Text   string         `json:"text"`
	Status TodoItemStatus `json:"status"`
	Done   bool           `json:"done"`
}

// WithDerivedDone 返回 Done 与 Status 一致的副本（Status == done → Done=true）。
func (item TodoItem) WithDerivedDone() TodoItem {
	item.Done = item.Status == TodoItemDone
	return item
}
