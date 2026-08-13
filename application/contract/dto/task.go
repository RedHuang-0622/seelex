// Package dto 承载 application/contract 与 seelebridge 共享的纯 DTO 类型
// （无运行时依赖；contract 依赖它、seelebridge 以 alias 兼容）。
package dto

import "time"

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
	Phase        string           `json:"phase"`                  // plan | tasklist | subagent | task
	Task         string           `json:"task"`                   // goal / label / todo text
	Description  string           `json:"description,omitempty"`  //
	Status       TaskStatus       `json:"status"`                 // pending/running/completed/failed/retry
	RetryCount   int              `json:"retry_count,omitempty"`  // 重试数字
	Assignee     string           `json:"assignee,omitempty"`     // main | 子代理 id | 执行节点
	Dependencies []string         `json:"dependencies,omitempty"` // 前置任务（WorkItem ID 引用）
	Attachments  []string         `json:"attachments,omitempty"`  // 可选附件路径
	Kind         string           `json:"kind"`                   // plan | todo | subagent | task
	SourceID     string           `json:"source_id,omitempty"`    // 原数据面 ID（详情溯源）
	Participants []string         `json:"participants,omitempty"` // 同一 task 的多个子代理
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
}

// TodoItemStatus 是清单项三态（兼容 TUI/旧契约；权威状态在 TaskRecord.Status）。
type TodoItemStatus string

const (
	TodoItemPending TodoItemStatus = "pending"
	TodoItemDoing   TodoItemStatus = "doing"
	TodoItemDone    TodoItemStatus = "done"
)

// TodoItem 是清单项（快照 DTO，兼容 TUI/旧契约）。
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
