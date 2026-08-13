package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const todoLimitHint = "seele.yaml limits.todo_max_items"

// Deps 是 todo/task 工具的运行时回调集合，由根包（Runtime）注入。
type Deps struct {
	RegisterTool         func(name, description string, inputSchema map[string]interface{}, handler func(ctx context.Context, argsJSON string) (string, error))
	ReplaceTodo          func(items []TodoItem) error
	AppendTodo           func(item TodoItem, limit int) error
	SetTodoStatusByIndex func(index int, status TaskStatus) (TaskRecord, error)
	TodoSnapshot         func() []TodoItem
	TaskAdd              func(spec TaskSpec) (TaskRecord, bool, error)
	TodoMaxItems         int
}

// Tools 是 todolist 工具族与 taskadd 的注册与处理（todo 与 task 注册表融合，
// docs/2026-08-09-worktable/tasklist.md：todolist 项即 kind=todo 的 task）。
type Tools struct {
	deps Deps
}

// NewTools 构造工具族（deps 全部为闭包，域内不依赖根包）。
func NewTools(deps Deps) *Tools {
	return &Tools{deps: deps}
}

func validTodoStatus(status TodoItemStatus) bool {
	return status == TodoItemPending || status == TodoItemDoing || status == TodoItemDone
}

// RegisterTodoTools 注册 todolist_init/add/done/status。
func (t *Tools) RegisterTodoTools() {
	t.deps.RegisterTool("todolist_init",
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
		t.todoInitHandler)
	t.deps.RegisterTool("todolist_add",
		"Append an item to the current todo list.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"item"},
			"properties": map[string]interface{}{
				"item": map[string]interface{}{"type": "string"},
			},
		},
		t.todoAddHandler)
	t.deps.RegisterTool("todolist_done",
		"Mark a todo item as done by its index (0-based, from todolist_status). When ALL items are done, call task_complete to submit the task.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"index"},
			"properties": map[string]interface{}{
				"index": map[string]interface{}{"type": "integer", "minimum": 0},
			},
		},
		t.todoDoneHandler)
	t.deps.RegisterTool("todolist_status",
		"Show the current todo list with done flags and indexes.",
		map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		t.todoStatusHandler)
}

// RegisterTaskTools 注册主动任务工具 taskadd（worktable task 体系；主动触发）。
func (t *Tools) RegisterTaskTools() {
	t.deps.RegisterTool("taskadd",
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
		t.taskAddHandler)
}

func (t *Tools) todoInitHandler(_ context.Context, argsJSON string) (string, error) {
	var input struct {
		Items []string `json:"items"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("todolist_init: invalid args: %w", err)
	}
	limit := t.deps.TodoMaxItems
	if len(input.Items) > limit {
		return "", fmt.Errorf("todolist_init: %d items exceeds limit %d", len(input.Items), limit)
	}
	items := make([]TodoItem, 0, len(input.Items))
	for _, text := range input.Items {
		if text = strings.TrimSpace(text); text != "" {
			items = append(items, TodoItem{Text: text, Status: TodoItemPending})
		}
	}
	if err := t.deps.ReplaceTodo(items); err != nil {
		return "", err
	}
	return t.todoStatusJSON(), nil
}

func (t *Tools) todoAddHandler(_ context.Context, argsJSON string) (string, error) {
	var input struct {
		Item string `json:"item"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("todolist_add: invalid args: %w", err)
	}
	if text := strings.TrimSpace(input.Item); text == "" {
		return "", fmt.Errorf("todolist_add: item is required")
	}
	if err := t.deps.AppendTodo(TodoItem{Text: strings.TrimSpace(input.Item), Status: TodoItemPending}, t.deps.TodoMaxItems); err != nil {
		return "", err
	}
	return t.todoStatusJSON(), nil
}

func (t *Tools) todoDoneHandler(_ context.Context, argsJSON string) (string, error) {
	var input struct {
		Index int `json:"index"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("todolist_done: invalid args: %w", err)
	}
	if _, err := t.deps.SetTodoStatusByIndex(input.Index, TaskCompleted); err != nil {
		return "", err
	}
	items := t.deps.TodoSnapshot()
	allDone := true
	for _, item := range items {
		if !item.Done {
			allDone = false
			break
		}
	}
	// 全部 done → 提示收尾（衔接 task_complete 终态；模型按收尾契约提交）。
	status := t.todoStatusJSON()
	if allDone {
		status = strings.TrimSuffix(status, "}") + `, "all_done": true, "hint": "所有待办已完成，调用 task_complete 提交任务"}`
	}
	return status, nil
}

func (t *Tools) todoStatusHandler(_ context.Context, _ string) (string, error) {
	return t.todoStatusJSON(), nil
}

// taskAddHandler 主动登记 task（幂等：按归一化 goal 去重）。
func (t *Tools) taskAddHandler(_ context.Context, argsJSON string) (string, error) {
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
	record, created, err := t.deps.TaskAdd(TaskSpec{
		Key:          TaskKeyForGoal(goal),
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
		"task_id": record.ID, "status": string(record.Status), "created": created, "duplicate": !created,
		"hint": "task 已登记到工作表格；相同任务自动去重（不重复建条目）",
	})
	return string(out), nil
}

// todoStatusJSON 渲染清单 JSON（{items:[{text,done}], done:n, total:n}）。
func (t *Tools) todoStatusJSON() string {
	items := t.deps.TodoSnapshot()
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
