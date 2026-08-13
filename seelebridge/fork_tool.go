package seelebridge

import (
	"context"
	"fmt"

	"github.com/RedHuang-0622/seelex/seelebridge/fork"
)

// fork_tool.go 只保留 Runtime 门面：fork_subagents 工具注册与委托。
// 执行编排（B6 task 幂等登记 → 结果复用 → fork DAG 构造 → 子代理树登记 →
// planExecutor 执行）已迁入 seelebridge/fork.Tool（Deps 注入）。

// registerForkTool 注册 fork_subagents（RegisterBuiltins 内调用）。
func (r *Runtime) registerForkTool() {
	r.RegisterTool("fork_subagents",
		"Fork N isolated subagents in parallel (worktree-isolated) and return their structured outputs."+fork.SubagentsContractDescription,
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"subagents": map[string]interface{}{
					"type":        "array",
					"description": "Subagent specs: unique id + natural-language goal.",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"id":   map[string]interface{}{"type": "string"},
							"goal": map[string]interface{}{"type": "string"},
						},
						"required": []string{"id", "goal"},
					},
				},
				"max_concurrency": map[string]interface{}{"type": "integer", "minimum": 1},
			},
			"required": []string{"subagents"},
		},
		r.forkSubagentsHandler)
}

// forkSubagentsHandler 是 fork_subagents 的执行入口（委托 fork.Tool.Handle）。
func (r *Runtime) forkSubagentsHandler(ctx context.Context, argsJSON string) (string, error) {
	if r == nil || r.forkTool == nil {
		return "", fmt.Errorf("fork_subagents: fork tool is not configured")
	}
	return r.forkTool.Handle(ctx, argsJSON)
}
