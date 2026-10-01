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
	RegisterTool func(name, description string, inputSchema map[string]interface{}, handler func(ctx context.Context, argsJSON string) (string, error))
	// SessionFromContext 解析执行 ctx 的会话归属（生产 = telemetry 会话键）。
	// 工具族写的是**调用它的那个会话**的 scope：子代理/后台会话里跑的清单与
	// taskadd 若落进实时注册表（当前视图会话），别的会话的工作表格与清单会被
	// 顶掉/贴错会话号——行属于谁取决于"谁在看"（2026-09-29 会话归属收口）。
	// 空 = 无 ctx 归属（旧调用面），退回实时注册表。
	SessionFromContext func(ctx context.Context) string
	TaskAddFor         func(sessionID string, spec TaskSpec) (TaskRecord, bool, error)
	ReplaceTodoFor     func(sessionID string, items []TodoItem) error
	AppendTodoFor      func(sessionID string, item TodoItem, limit int) error
	SetTodoStatusFor   func(sessionID string, index int, status TaskStatus) (TaskRecord, error)
	TodoSnapshotFor    func(sessionID string) []TodoItem
	TodoMaxItems       int
}

// Tools 是 todolist 工具族与 taskadd 的注册与处理（todo 与 task 注册表融合，
// docs/2026-08-09-worktable/tasklist.md：todolist 项即 kind=todo 的 task）。
type Tools struct {
	deps Deps
}

// sessionID 解析本次工具调用的会话归属（空 = 旧调用面，落实时注册表）。
func (t *Tools) sessionID(ctx context.Context) string {
	if t.deps.SessionFromContext == nil {
		return ""
	}
	return t.deps.SessionFromContext(ctx)
}

// NewTools 构造工具族（deps 全部为闭包，域内不依赖根包）。
func NewTools(deps Deps) *Tools {
	return &Tools{deps: deps}
}

// RegisterTodoTools 注册 todo 清单工具族：规范名 todo_init/add/done/status，
// 并保留 todolist_* 兼容别名（deprecated，迁移窗口内并存）。
func (t *Tools) RegisterTodoTools() {
	t.deps.RegisterTool("todo_init",
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
	t.deps.RegisterTool("todolist_init",
		"[deprecated] 兼容旧名，请改用 todo_init。Replace the current todo list with the given items (max "+todoLimitHint+").",
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
	t.deps.RegisterTool("todo_add",
		"Append an item to the current todo list.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"item"},
			"properties": map[string]interface{}{
				"item": map[string]interface{}{"type": "string"},
			},
		},
		t.todoAddHandler)
	t.deps.RegisterTool("todolist_add",
		"[deprecated] 兼容旧名，请改用 todo_add。Append an item to the current todo list.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"item"},
			"properties": map[string]interface{}{
				"item": map[string]interface{}{"type": "string"},
			},
		},
		t.todoAddHandler)
	t.deps.RegisterTool("todo_done",
		"Mark a todo item as done by its index (0-based, from todo_status). When ALL items are done, call task_complete to submit the task.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"index"},
			"properties": map[string]interface{}{
				"index": map[string]interface{}{"type": "integer", "minimum": 0},
			},
		},
		t.todoDoneHandler)
	t.deps.RegisterTool("todolist_done",
		"[deprecated] 兼容旧名，请改用 todo_done。Mark a todo item as done by its index (0-based, from todo_status).",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"index"},
			"properties": map[string]interface{}{
				"index": map[string]interface{}{"type": "integer", "minimum": 0},
			},
		},
		t.todoDoneHandler)
	t.deps.RegisterTool("todo_status",
		"Show the current todo list with done flags and indexes.",
		map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		t.todoStatusHandler)
	t.deps.RegisterTool("todolist_status",
		"[deprecated] 兼容旧名，请改用 todo_status。Show the current todo list with done flags and indexes.",
		map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		t.todoStatusHandler)
}

// RegisterTaskTools 注册主动任务工具 task_add（worktable task 体系；主动
// 触发），并保留 taskadd 兼容别名（deprecated）。
func (t *Tools) RegisterTaskTools() {
	t.deps.RegisterTool("task_add",
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
	t.deps.RegisterTool("taskadd",
		"[deprecated] 兼容旧名，请改用 task_add。Register a task in the work table (task is a worktable entry).",
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

func (t *Tools) todoInitHandler(ctx context.Context, argsJSON string) (string, error) {
	var input struct {
		Items []string `json:"items"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("todo_init: invalid args: %w", err)
	}
	limit := t.deps.TodoMaxItems
	if len(input.Items) > limit {
		return "", fmt.Errorf("todo_init: %d items exceeds limit %d", len(input.Items), limit)
	}
	items := make([]TodoItem, 0, len(input.Items))
	for _, text := range input.Items {
		if text = strings.TrimSpace(text); text != "" {
			items = append(items, TodoItem{Text: text, Status: TodoItemPending})
		}
	}
	sessionID := t.sessionID(ctx)
	if err := t.deps.ReplaceTodoFor(sessionID, items); err != nil {
		return "", err
	}
	return t.todoStatusJSON(sessionID), nil
}

func (t *Tools) todoAddHandler(ctx context.Context, argsJSON string) (string, error) {
	var input struct {
		Item string `json:"item"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("todo_add: invalid args: %w", err)
	}
	if text := strings.TrimSpace(input.Item); text == "" {
		return "", fmt.Errorf("todo_add: item is required")
	}
	sessionID := t.sessionID(ctx)
	if err := t.deps.AppendTodoFor(sessionID, TodoItem{Text: strings.TrimSpace(input.Item), Status: TodoItemPending}, t.deps.TodoMaxItems); err != nil {
		return "", err
	}
	return t.todoStatusJSON(sessionID), nil
}

func (t *Tools) todoDoneHandler(ctx context.Context, argsJSON string) (string, error) {
	var input struct {
		Index int `json:"index"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("todo_done: invalid args: %w", err)
	}
	sessionID := t.sessionID(ctx)
	if _, err := t.deps.SetTodoStatusFor(sessionID, input.Index, TaskCompleted); err != nil {
		return "", err
	}
	items := t.deps.TodoSnapshotFor(sessionID)
	allDone := true
	for _, item := range items {
		if !item.Done {
			allDone = false
			break
		}
	}
	// 全部 done → 提示收尾（衔接 task_complete 终态；模型按收尾契约提交）。
	status := t.todoStatusJSON(sessionID)
	if allDone {
		status = strings.TrimSuffix(status, "}") + `, "all_done": true, "hint": "所有待办已完成，调用 task_complete 提交任务"}`
	}
	return status, nil
}

func (t *Tools) todoStatusHandler(ctx context.Context, _ string) (string, error) {
	return t.todoStatusJSON(t.sessionID(ctx)), nil
}

// taskAddHandler 主动登记 task（幂等：按归一化 goal 去重）。
//
// 会话归属取**调用 ctx 的会话键**：子代理/后台会话里调的 taskadd 必须登记到
// 自己那个会话的 scope，不能借实时注册表落进当前视图会话的表格。
func (t *Tools) taskAddHandler(ctx context.Context, argsJSON string) (string, error) {
	var input struct {
		Goal         string   `json:"goal"`
		Description  string   `json:"description,omitempty"`
		Dependencies []string `json:"dependencies,omitempty"`
		Attachments  []string `json:"attachments,omitempty"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("task_add: invalid args: %w", err)
	}
	goal := strings.TrimSpace(input.Goal)
	if goal == "" {
		return "", errors.New("task_add: goal is required")
	}
	sessionID := ""
	if t.deps.SessionFromContext != nil {
		sessionID = t.deps.SessionFromContext(ctx)
	}
	record, created, err := t.deps.TaskAddFor(sessionID, TaskSpec{
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
// sessionID 是调用会话：读的是**这个会话**的清单（空 = 实时注册表）。
func (t *Tools) todoStatusJSON(sessionID string) string {
	items := t.deps.TodoSnapshotFor(sessionID)
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
