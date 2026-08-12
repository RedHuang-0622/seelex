package seelebridge

import (
	"github.com/RedHuang-0622/seelex/seelebridge/task"
)

// ── task 域 API 兼容别名 ────────────────────────────────────────────────
// task 注册表域已迁入 seelebridge/task；本文件在根包重导出全部公开类型、
// 常量与辅助函数，保证既有调用面（Runtime 装配、todo_tool、application
// 端口、TUI/旧契约）不因拆包而破坏。

// TaskStatus 是 task 的生命周期状态。
type TaskStatus = task.TaskStatus

const (
	TaskPending   = task.TaskPending
	TaskQueued    = task.TaskQueued
	TaskRunning   = task.TaskRunning
	TaskDoing     = task.TaskDoing
	TaskCompleted = task.TaskCompleted
	TaskFailed    = task.TaskFailed
	TaskRetry     = task.TaskRetry
)

// TaskPhase* 是 worktable 条目阶段常量（前端筛选/渲染依赖这些字符串）。
const (
	TaskPhasePlan     = task.TaskPhasePlan
	TaskPhaseTask     = task.TaskPhaseTask
	TaskPhaseTasklist = task.TaskPhaseTasklist
	TaskPhaseSubagent = task.TaskPhaseSubagent
)

// TaskTracePoint 是 task 打点（与 worktable trace 同形；evidence 有界）。
type TaskTracePoint = task.TaskTracePoint

// TaskRecord 是 task 的只读快照 DTO（字段与 worktable WorkItem 同构）。
type TaskRecord = task.TaskRecord

// TaskSpec 是 task 创建入参（主动 taskadd 或被动生命周期装配）。
type TaskSpec = task.TaskSpec

// TodoItemStatus 是清单项三态（兼容 TUI/旧契约）。
type TodoItemStatus = task.TodoItemStatus

const (
	TodoItemPending = task.TodoItemPending
	TodoItemDoing   = task.TodoItemDoing
	TodoItemDone    = task.TodoItemDone
)

// TodoItem 是清单项（快照 DTO，兼容 TUI/旧契约）。
type TodoItem = task.TodoItem

// TaskTerminalHandler 是工具 handler 工厂（task 终态工具定义）。
type TaskTerminalHandler = task.TaskTerminalHandler

// taskToTodoItem 把 kind=todo 的 task 还原为 TodoItem（兼容 TUI/旧契约）。
func taskToTodoItem(record TaskRecord) TodoItem { return task.TaskToTodoItem(record) }

// todoToTaskStatus 把 TodoItemStatus 映射为 TaskStatus（todolist 状态更新入口）。
func todoToTaskStatus(status TodoItemStatus) TaskStatus { return task.TodoToTaskStatus(status) }

// taskKeyForGoal 生成 task 的幂等键（归一化 goal 的 FNV-1a 哈希）。
func taskKeyForGoal(goal string) string { return task.TaskKeyForGoal(goal) }
