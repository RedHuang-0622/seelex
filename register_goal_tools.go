package main

// register_goal_tools.go — main agent goal 工具族注册（P1）。
//
// 与 registerTaskTerminalTools 同款组合根模式：Runtime 只负责注册，
// handler 路由到 application.Service 的 goal 方法面（按执行 ctx 会话）。
// 工具可见性门控见 seelebridge/tools/policy.go（isGoalTool + GoalActive）。

import (
	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/seelebridge"
)

func registerGoalTools(runtime *seelebridge.Runtime, app *application.Service) {
	beginSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"title":        map[string]interface{}{"type": "string", "description": "goal 标题"},
			"statement":    map[string]interface{}{"type": "string", "description": "目标正文（可选）"},
			"acceptance":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "完成条件（可选）"},
			"out_of_scope": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "非目标范围（可选）"},
		},
		"required": []string{"title"},
	}
	updateSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"title":            map[string]interface{}{"type": "string", "description": "标题（有值才改）"},
			"statement":        map[string]interface{}{"type": "string", "description": "正文（有值才改）"},
			"progress_kind":    map[string]interface{}{"type": "string", "description": "milestone|finding|decision|risk"},
			"progress_content": map[string]interface{}{"type": "string", "description": "进度/发现内容"},
		},
	}
	finishSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"reason": map[string]interface{}{"type": "string", "description": "收口理由（可选）"},
			"result": map[string]interface{}{"type": "string", "description": "最终结果/证据（可选）"},
		},
	}
	runtime.RegisterTool(
		"goal_begin",
		"注册并压栈一个会话 goal（#goal 的工具形态；goal 治理启动入口）。压栈后由 ADVISOR 回合制评审，只有终态裁决可收口。",
		beginSchema,
		app.GoalBeginHandler,
	)
	runtime.RegisterTool(
		"goal_update",
		"向栈顶 active goal 追加一条进度/发现或更新标题/正文/完成条件（推进 goal 治理，不直接收口）。",
		updateSchema,
		app.GoalUpdateHandler,
	)
	runtime.RegisterTool(
		"goal_status",
		"读取当前会话 goal 栈全量视图（栈顶 + 下层状态；无 goal 返回空栈）。",
		map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		app.GoalStatusHandler,
	)
	runtime.RegisterTool(
		"goal_propose_finish",
		"提议收口当前栈顶 goal（mainAgent 只能提议；终态裁决由 ADVISOR gate 给出：verdict_done 收口 / verdict_not_done 纠偏 / TL 缺席直连收口）。",
		finishSchema,
		app.GoalProposeFinishHandler,
	)
}
