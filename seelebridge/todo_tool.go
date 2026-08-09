package seelebridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ── todolist 工具族（与 task 注册表融合，docs/2026-08-09-worktable/tasklist.md）──
// todolist 直接内化为 worktable 的打点表：todolist_* 工具与 taskadd 共享
// 同一个 task 注册表（seelebridge/task_registry.go，Actor + Mailbox），
// todolist 项即 kind=todo 的 task（保持有序列表与索引语义），每次操作写入
// trace 打点；无独立 todo 业务实体，业务耦合降低。
//
// 全部 done → 响应提示模型调用 task_complete 提交任务（收尾契约衔接）。

// TodoItemStatus 是待办项的三态（pending → doing → done；用户可回退）。
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

func (item TodoItem) withDerivedDone() TodoItem {
	item.Done = item.Status == TodoItemDone
	return item
}

func validTodoStatus(status TodoItemStatus) bool {
	return status == TodoItemPending || status == TodoItemDoing || status == TodoItemDone
}

// todoToTaskStatus 把 todolist 三态映射为 task 状态（doing → doing）。
func todoToTaskStatus(status TodoItemStatus) TaskStatus {
	switch status {
	case TodoItemDoing:
		return TaskDoing
	case TodoItemDone:
		return TaskCompleted
	default:
		return TaskPending
	}
}

// taskToTodoItem 把 kind=todo 的 task 还原为 TodoItem（兼容 TUI/旧契约）。
func taskToTodoItem(record TaskRecord) TodoItem {
	status := TodoItemPending
	switch record.Status {
	case TaskDoing:
		status = TodoItemDoing
	case TaskCompleted:
		status = TodoItemDone
	}
	return TodoItem{Text: record.Task, Status: status}.withDerivedDone()
}

// TodoSnapshot 返回当前清单只读拷贝（application 快照投影数据源；
// 主代理每次 todolist_* 工具完成经 runtime.changed 增量带到 GUI）。
func (r *Runtime) TodoSnapshot() []TodoItem {
	if r == nil || r.tasks == nil {
		return nil
	}
	records := r.tasks.TodoSnapshot()
	items := make([]TodoItem, 0, len(records))
	for _, record := range records {
		items = append(items, taskToTodoItem(record))
	}
	return items
}

// SetTodoStatus 设置指定待办项的三态（GUI 工作表格人工状态更新入口；
// 只改状态，不新增工具族——todolist 仍是 harness 默认工具族）。
func (r *Runtime) SetTodoStatus(index int, status TodoItemStatus) error {
	if r == nil || r.tasks == nil {
		return errors.New("todolist: unavailable")
	}
	if !validTodoStatus(status) {
		return fmt.Errorf("todolist: invalid status %q (want pending|doing|done)", status)
	}
	_, err := r.tasks.SetTodoStatusByIndex(index, todoToTaskStatus(status))
	return err
}

// registerTodoTools 注册 todolist 工具族（RegisterBuiltins 内调用）。
func (r *Runtime) registerTodoTools() {
	r.RegisterTool("todolist_init",
		"Replace the current todo list with the given items (max "+todoLimitHint+"). Use to plan your own work; the list is yours to maintain as you execute.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"items"},
			"properties": map[string]interface{}{
				"items": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Todo items, each a short actionable step.",
				},
			},
		},
		r.todoInitHandler)
	r.RegisterTool("todolist_add",
		"Append an item to the current todo list.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"item"},
			"properties": map[string]interface{}{
				"item": map[string]interface{}{"type": "string"},
			},
		},
		r.todoAddHandler)
	r.RegisterTool("todolist_done",
		"Mark a todo item as done by its index (0-based, from todolist_status). When ALL items are done, call task_complete to submit the task.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"index"},
			"properties": map[string]interface{}{
				"index": map[string]interface{}{"type": "integer", "minimum": 0},
			},
		},
		r.todoDoneHandler)
	r.RegisterTool("todolist_status",
		"Show the current todo list with done flags and indexes.",
		map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		r.todoStatusHandler)
}

// registerTaskTools 注册主动任务工具 taskadd（worktable task 体系；主动触发）。
func (r *Runtime) registerTaskTools() {
	r.RegisterTool("taskadd",
		"Register a task in the work table (task is a worktable entry). Tasks are deduplicated by normalized goal: if the same task already exists, the existing task id is returned and no duplicate is created. Do not create tasks that already exist.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"goal"},
			"properties": map[string]interface{}{
				"goal":         map[string]interface{}{"type": "string", "description": "Task goal / name; used as the dedup key."},
				"description":  map[string]interface{}{"type": "string", "description": "Optional task description."},
				"dependencies": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Optional prerequisite task ids."},
				"attachments":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Optional attachment paths."},
			},
		},
		r.taskAddHandler)
}

const todoLimitHint = "seele.yaml limits.todo_max_items"

// ── 处理器 ───────────────────────────────────────────────────────────

func (r *Runtime) todoInitHandler(_ context.Context, argsJSON string) (string, error) {
	var input struct {
		Items []string `json:"items"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("todolist_init: invalid args: %w", err)
	}
	limit := r.limits.TodoMaxItems
	if len(input.Items) > limit {
		return "", fmt.Errorf("todolist_init: %d items exceeds limit %d", len(input.Items), limit)
	}
	items := make([]TodoItem, 0, len(input.Items))
	for _, text := range input.Items {
		if text = strings.TrimSpace(text); text != "" {
			items = append(items, TodoItem{Text: text, Status: TodoItemPending})
		}
	}
	if err := r.tasks.ReplaceTodo(items); err != nil {
		return "", err
	}
	return r.todoStatusJSON(), nil
}

func (r *Runtime) todoAddHandler(_ context.Context, argsJSON string) (string, error) {
	var input struct {
		Item string `json:"item"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("todolist_add: invalid args: %w", err)
	}
	if text := strings.TrimSpace(input.Item); text == "" {
		return "", fmt.Errorf("todolist_add: item is required")
	}
	if err := r.tasks.AppendTodo(TodoItem{Text: strings.TrimSpace(input.Item), Status: TodoItemPending}, r.limits.TodoMaxItems); err != nil {
		return "", err
	}
	return r.todoStatusJSON(), nil
}

func (r *Runtime) todoDoneHandler(_ context.Context, argsJSON string) (string, error) {
	var input struct {
		Index int `json:"index"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("todolist_done: invalid args: %w", err)
	}
	if _, err := r.tasks.SetTodoStatusByIndex(input.Index, TaskCompleted); err != nil {
		return "", err
	}
	items := r.TodoSnapshot()
	allDone := true
	for _, item := range items {
		if !item.Done {
			allDone = false
			break
		}
	}
	// 全部 done → 提示收尾（衔接 task_complete 终态；模型按收尾契约提交）。
	status := r.todoStatusJSON()
	if allDone {
		status = strings.TrimSuffix(status, "}") + `, "all_done": true, "hint": "所有待办已完成，调用 task_complete 提交任务"}`
	}
	return status, nil
}

func (r *Runtime) todoStatusHandler(_ context.Context, _ string) (string, error) {
	return r.todoStatusJSON(), nil
}

// taskAddHandler 主动登记 task（幂等：按归一化 goal 去重）。
func (r *Runtime) taskAddHandler(_ context.Context, argsJSON string) (string, error) {
	var input struct {
		Goal         string   `json:"goal"`
		Description  string   `json:"description,omitempty"`
		Dependencies []string `json:"dependencies,omitempty"`
		Attachments  []string `json:"attachments,omitempty"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("taskadd: invalid args: %w", err)
	}
	goal := strings.TrimSpace(input.Goal)
	if goal == "" {
		return "", errors.New("taskadd: goal is required")
	}
	task, created, err := r.TaskAdd(TaskSpec{
		Key:          taskKeyForGoal(goal),
		Phase:        TaskPhaseTask,
		Task:         goal,
		Description:  strings.TrimSpace(input.Description),
		Kind:         "task",
		Dependencies: input.Dependencies,
		Attachments:  input.Attachments,
	})
	if err != nil {
		return "", err
	}
	out, _ := json.Marshal(map[string]interface{}{
		"task_id": task.ID, "status": string(task.Status), "created": created, "duplicate": !created,
		"hint": "task 已登记到工作表格；相同任务自动去重（不重复建条目）",
	})
	return string(out), nil
}

// todoStatusJSON 渲染清单 JSON（{items:[{text,done}], done:n, total:n}）。
func (r *Runtime) todoStatusJSON() string {
	items := r.TodoSnapshot()
	done := 0
	encoded := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if item.Done {
			done++
		}
		encoded = append(encoded, map[string]interface{}{"text": item.Text, "done": item.Done})
	}
	out, _ := json.Marshal(map[string]interface{}{
		"items": encoded, "done": done, "total": len(items),
	})
	return string(out)
}
