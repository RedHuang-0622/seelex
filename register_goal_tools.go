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
	doneSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{"type": "string", "enum": []string{"finish", "abort"}, "description": "finish = 目标达成收口（缺省），abort = 放弃/终止"},
			"reason": map[string]interface{}{"type": "string", "description": "收口理由（可选，进审计）"},
			"result": map[string]interface{}{"type": "string", "description": "最终结果/证据（可选，进审计）"},
		},
	}
	runtime.RegisterTool(
		"goal_begin",
		"注册并压栈一个会话 goal（目标看板的正文 + 完成条件；$goal 的工具形态）。目标由**提示词驱动的 leader 派活**推进（没有席位轮转）；收口走 goal_done（主代理即 TL，真收口）或 goal_propose_finish（送终态 gate）。",
		beginSchema,
		app.GoalBeginHandler,
	)
	runtime.RegisterTool(
		"goal_update",
		"向栈顶 active goal 追加一条进度/发现（只汇报，不直接收口）。权限：agent 面只能汇报进度；改 goal 定义（标题/正文/完成条件）只有 ADVISOR 裁决侧能发起，改定义反而会让评审依据失效。",
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
	runtime.RegisterTool(
		"goal_done",
		"直接收口当前栈顶 goal（main agent 的真收口面：主代理即 TL 角色，不再送 ADVISOR 提议）。action=finish|abort（缺省 finish），reason/result 进审计。员工/子代理看不到这个工具。",
		doneSchema,
		app.GoalDoneHandler,
	)
}
