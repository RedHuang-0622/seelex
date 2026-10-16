package task

import (
	"context"

	"github.com/RedHuang-0622/Seele/tools"
	"github.com/RedHuang-0622/Seele/types"
)

// 本文件实现 plan.md §3.1 的 TaskTerminalProvider：把 task_complete /
// task_check_node / task_failed / task_needs_user_decision 工具注册为
// tools.Registry 产品工具。handler 由 application 侧提供
// （TaskService.TerminalHandler 产物，内部完成"投影同步 flush → 校验 →
// 应用状态"，见 application/core/task_service.go），本包只负责工具定义与注册。
// task_check_node 是非终态的在途打点（tasklist 模式下逐节点完成 → 前端打勾），
// 其余三个是终态工具。

// TaskTerminalHandler 是工具 handler 工厂：kind 为 task_complete /
// task_check_node / task_failed / task_needs_user_decision 四值。
type TaskTerminalHandler func(kind string) func(context.Context, string) (string, error)

const TaskTerminalProviderName = "seelex-task-terminal"

// TaskTerminalProvider 实现 tools.ToolProvider：三个终态工具 + 产品 schema。
type TaskTerminalProvider struct {
	handler TaskTerminalHandler
}

func NewTaskTerminalProvider(handler TaskTerminalHandler) *TaskTerminalProvider {
	return &TaskTerminalProvider{handler: handler}
}

func (p *TaskTerminalProvider) ProviderName() string { return TaskTerminalProviderName }

func (p *TaskTerminalProvider) Tools() []tools.ToolEntry {
	if p == nil || p.handler == nil {
		return nil
	}
	return []tools.ToolEntry{
		{
			Definition: types.Tool{
				Type: "function",
				Function: types.ToolFunction{
					Name:        "task_complete",
					Description: "End the current task after delivering the requested result and evidence.",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"summary":         map[string]interface{}{"type": "string", "description": "User-facing delivery summary."},
							"completed_nodes": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
							"artifacts":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
							"evidence":        map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
							"remaining_risks": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
						},
						"required": []string{"summary"},
					},
				},
			},
			Handler: tools.HandlerFunc(p.handler("task_complete")),
		},
		{
			Definition: types.Tool{
				Type: "function",
				Function: types.ToolFunction{
					Name:        "task_check_node",
					Description: "Check off one node of the loaded task structure while working through the tasklist. Call it as soon as a node's work is finished, before moving on to the next node; it marks that node done in the frontend checklist and does not end the task.",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"node_id":  map[string]interface{}{"type": "string", "description": "ID of the node whose work is now finished."},
							"output":   map[string]interface{}{"type": "string", "description": "Brief summary of what this node delivered."},
							"evidence": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
						},
						"required": []string{"node_id"},
					},
				},
			},
			Handler: tools.HandlerFunc(p.handler("task_check_node")),
		},
		{
			Definition: types.Tool{
				Type: "function",
				Function: types.ToolFunction{
					Name:        "task_needs_user_decision",
					Description: "Pause the current task only when a user choice is required; include the exact question and options.",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"summary":           map[string]interface{}{"type": "string", "description": "Brief user-facing explanation of why a decision is required."},
							"decision_question": map[string]interface{}{"type": "string", "description": "The specific choice only the user can make."},
							"decision_options":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
							"evidence":          map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
							"partial_progress":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
						},
						"required": []string{"summary", "decision_question"},
					},
				},
			},
			Handler: tools.HandlerFunc(p.handler("task_needs_user_decision")),
		},
		{
			Definition: types.Tool{
				Type: "function",
				Function: types.ToolFunction{
					Name:        "task_failed",
					Description: "End the current task with bounded failure evidence; recommend replan only when facts require it.",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"summary":            map[string]interface{}{"type": "string", "description": "User-facing failure summary."},
							"failure_type":       map[string]interface{}{"type": "string", "enum": []string{"blocked", "verification_failed", "invalid_plan", "external_dependency"}},
							"failed_node":        map[string]interface{}{"type": "string"},
							"evidence":           map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
							"partial_progress":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
							"replan_recommended": map[string]interface{}{"type": "boolean"},
						},
						"required": []string{"summary", "failure_type"},
					},
				},
			},
			Handler: tools.HandlerFunc(p.handler("task_failed")),
		},
	}
}
