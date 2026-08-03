package seelebridge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// ── todolist 工具族（docs/2026-08-03-subagent-fork-architecture/plan.md §5）──
// 模型自由层的轻量计划组织：无 DAG 校验、无节点类型、无拓扑约束——模型
// 用自然语言维护自己的待办清单。与 plan 的区别：todolist 无执行器（模型
// 自己逐项干），plan 是确定性 DAG（workplan runner 执行）。
//
// 状态：Runtime 内存态（会话级持久化接 SessionContextRecord 为后续切片）；
// 全部 done → 响应提示模型调用 task_complete 提交任务（收尾契约衔接）。

// TodoItem 是清单项（快照 DTO，GUI 面板消费）。
type TodoItem struct {
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// todoState 是 todolist 的 actor 资源（自带锁，读写即消息进出；
// 与 skill.Registry / filesystem 同构）。
type todoState struct {
	mu    sync.Mutex
	items []TodoItem
}

func newTodoState() *todoState { return &todoState{items: make([]TodoItem, 0)} }

// Snapshot 返回清单只读拷贝（GUI/遥测轮询）。
func (s *todoState) Snapshot() []TodoItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]TodoItem(nil), s.items...)
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
			items = append(items, TodoItem{Text: text})
		}
	}
	r.todo.mu.Lock()
	r.todo.items = items
	r.todo.mu.Unlock()
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
	r.todo.mu.Lock()
	if len(r.todo.items) >= r.limits.TodoMaxItems {
		r.todo.mu.Unlock()
		return "", fmt.Errorf("todolist_add: list already at limit %d", r.limits.TodoMaxItems)
	}
	r.todo.items = append(r.todo.items, TodoItem{Text: strings.TrimSpace(input.Item)})
	r.todo.mu.Unlock()
	return r.todoStatusJSON(), nil
}

func (r *Runtime) todoDoneHandler(_ context.Context, argsJSON string) (string, error) {
	var input struct {
		Index int `json:"index"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("todolist_done: invalid args: %w", err)
	}
	r.todo.mu.Lock()
	if input.Index < 0 || input.Index >= len(r.todo.items) {
		r.todo.mu.Unlock()
		return "", fmt.Errorf("todolist_done: index %d out of range (0..%d)", input.Index, len(r.todo.items)-1)
	}
	r.todo.items[input.Index].Done = true
	allDone := true
	for _, item := range r.todo.items {
		if !item.Done {
			allDone = false
			break
		}
	}
	r.todo.mu.Unlock()
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

// todoStatusJSON 渲染清单 JSON（{items:[{text,done}], done:n, total:n}）。
func (r *Runtime) todoStatusJSON() string {
	items := r.todo.Snapshot()
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
