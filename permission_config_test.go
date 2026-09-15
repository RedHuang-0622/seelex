package main

import (
	"context"
	"errors"
	"testing"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	seenode "github.com/RedHuang-0622/seelex/seelebridge/node"
	seeltools "github.com/RedHuang-0622/seelex/seelebridge/tools"
)

// permission_config_test.go — 权责系统的装配验收：默认配置 + config/seele.yaml 合并后，
//
//  1. 工具面里**每一个**注册工具都能被机械归组（不产生"未分封"漏点）；
//  2. 主代理（root）逐工具的判定与本系统接线前的产品口径一致（改动见 want 表注释）；
//  3. 子代理（sub）违权直接拒绝、员工（emp_*) 违权走执行选择页面 —— 走真实合并配置。

// permissionToolNames 是当前注册工具面的全量名单（含 deprecated 别名）。
// 新工具加入时这份名单与 DefaultPermissionGroupList 一起更新；测试会钉住"不许漏分封"。
var permissionToolNames = []string{
	// ro 读簇
	"read_file", "read_plan", "read_tool_result", "read_compressed_turn", "search_history",
	"grep_search", "glob", "get_time", "web_search",
	"todo_status", "todolist_status", "plan_status", "goal_status",
	"plugins_list", "skills_list", "mcp_list", "plan_validate", "plan_export",
	"computer_screenshot", "computer_windows", "computer_wait",
	// rw 写簇（项目）
	"write_file", "edit_file", "bash",
	"todo_init", "todo_add", "todo_done", "todolist_init", "todolist_add", "todolist_done",
	"task_add", "taskadd", "project_refresh", "plugin_create", "skill_create",
	// rw 写簇（会话工作台）
	"compact_context",
	// rw 写簇（共享外设）
	"computer_click", "computer_move", "computer_drag", "computer_scroll",
	"computer_type", "computer_keys", "computer_focus",
	// ctl 叫停 loop 簇
	"task_complete", "task_failed", "task_needs_user_decision", "task_check_node",
	"ask_approve", "fork_subagents", "plan_load", "plan_clear", "plan_run",
	"goal_begin", "goal_update", "goal_propose_finish",
	// adm 属主簇
	"switch_plugin", "switch_mode", "skill_activate", "plugins_reload", "mcp_create", "mcp_load",
}

// mergedPermissionConfig 复刻组合根的装配：默认权责配置 + config/seele.yaml 覆盖。
func mergedPermissionConfig(t *testing.T) toolspermission.PermissionConfig {
	t.Helper()
	base := seeltools.DefaultPermissionConfig()
	fileCfg, err := loadPermissionConfig("config/seele.yaml")
	if err != nil {
		t.Fatalf("loadPermissionConfig: %v", err)
	}
	return mergePermissionConfig(base, fileCfg)
}

// TestEveryRegisteredToolIsRouted 钉住"任一工具都能被机械归组"：没有未分封漏点，
// 否则子代理侧会把它当成违权直接拒绝，员工侧会变成每次提权。
func TestEveryRegisteredToolIsRouted(t *testing.T) {
	cfg := mergedPermissionConfig(t)
	if len(cfg.Groups) == 0 || len(cfg.Subjects) == 0 {
		t.Fatalf("分组/主体表必须已装配：groups=%d subjects=%d", len(cfg.Groups), len(cfg.Subjects))
	}
	for _, name := range permissionToolNames {
		group, ok := seeltools.RoutePermissionGroup(cfg.Groups, name)
		if !ok {
			t.Fatalf("%s 未分封：注册工具面出现漏点", name)
		}
		if group.Mode == 0 {
			t.Fatalf("%s 归组 %q 但没有位（mode=0）：位判定会失效", name, group.Name)
		}
	}
}

// TestMainAgentToolDecisions 是主代理逐工具判定表（root 主体）。
//
// 与接线前的差异只有三处（都是有意的收口，方向是"该放行的不再逐次弹窗，
// 该问人的仍然问人，危险的仍然拒绝"）：
//   - ctl 簇（收口/打点/派生/装载）与 ro/rw 的常规工具：组默认 allow（旧配置里
//     它们落在 `"*" → ask` 兜底上）；
//   - mcp_load：从旧配置的显式 allow 收紧成 adm 组默认 ask（连 MCP = 扩能力面）；
//   - bash 的 args 级白名单/危险模式真正生效（旧实现拿工具入参 JSON 去匹配命令
//     模式，patterns 全不命中）。
func TestMainAgentToolDecisions(t *testing.T) {
	cfg := mergedPermissionConfig(t)
	checker := toolspermission.NewPermissionChecker(cfg)
	root := toolspermission.SubjectRoot

	allow := map[string]bool{
		"read_file": true, "glob": true, "grep_search": true, "get_time": true, "web_search": true,
		"read_plan": true, "read_tool_result": true, "read_compressed_turn": true, "search_history": true,
		"computer_wait": true, "compact_context": true,
		"todo_status": true, "todolist_status": true, "plan_status": true, "goal_status": true,
		"plugins_list": true, "skills_list": true, "mcp_list": true,
		"plan_validate": true, "plan_export": true,
		"todo_init": true, "todo_add": true, "todo_done": true,
		"todolist_init": true, "todolist_add": true, "todolist_done": true,
		"task_add": true, "taskadd": true, "project_refresh": true,
		"task_complete": true, "task_failed": true, "task_needs_user_decision": true,
		"task_check_node": true, "ask_approve": true, "fork_subagents": true,
		"plan_load": true, "plan_clear": true, "plan_run": true,
		"goal_begin": true, "goal_update": true, "goal_propose_finish": true,
	}
	ask := map[string]bool{
		"write_file": true, "edit_file": true, "plugin_create": true, "skill_create": true,
		"computer_screenshot": true, "computer_windows": true,
		"computer_click": true, "computer_move": true, "computer_drag": true,
		"computer_scroll": true, "computer_type": true, "computer_keys": true, "computer_focus": true,
		"switch_plugin": true, "switch_mode": true, "skill_activate": true,
		"plugins_reload": true, "mcp_create": true, "mcp_load": true,
	}

	for _, name := range permissionToolNames {
		// bash 的动作取决于命令本体（args 级能力白名单），由
		// TestMainAgentBashArgPolicy 单独钉住。
		if name == "bash" {
			continue
		}
		result, visible := checker.DecideForMeta(root, name, frameworktools.ToolMeta{}, `{}`)
		if !visible {
			t.Fatalf("%s: root 全位，不该不可见", name)
		}
		switch {
		case allow[name]:
			if result != toolspermission.ResultAllow {
				t.Fatalf("%s: root 判定 = %v, want allow", name, result)
			}
		case ask[name]:
			if result != toolspermission.ResultAsk {
				t.Fatalf("%s: root 判定 = %v, want ask", name, result)
			}
		default:
			t.Fatalf("%s 未在期望表里登记（新工具加入时要显式决定它的动作）", name)
		}
	}
}

// TestMainAgentBashArgPolicy 钉住 bash 的 args 级能力白名单：命令本体参与模式
// 匹配（见 tools.policyArgsFor），安全命令直接执行、越界问人、危险拒绝。
func TestMainAgentBashArgPolicy(t *testing.T) {
	cfg := mergedPermissionConfig(t)
	checker := toolspermission.NewPermissionChecker(cfg)
	root := toolspermission.SubjectRoot

	cases := map[string]toolspermission.CheckResult{
		"git status":      toolspermission.ResultAllow,
		"go test ./...":   toolspermission.ResultAllow,
		"python x.py":     toolspermission.ResultAsk,
		"npm install foo": toolspermission.ResultAsk,
		"rm -rf /":        toolspermission.ResultDeny,
	}
	for command, want := range cases {
		result, visible := checker.DecideForMeta(root, "bash", frameworktools.ToolMeta{}, command)
		if !visible {
			t.Fatalf("bash %q: 不该不可见", command)
		}
		if result != want {
			t.Fatalf("bash %q 判定 = %v, want %v", command, result, want)
		}
	}
}

// TestMergedConfigSubagentAndEmployee 走真实合并配置验收两条产品口径：
// 子代理违权 → 直接拒绝（不弹页面）；员工违权 → 执行选择页面提权。
func TestMergedConfigSubagentAndEmployee(t *testing.T) {
	cfg := mergedPermissionConfig(t)

	// 子代理：ctl / adm 断位 → ErrToolNotVisible，且不产生审批请求。
	approvals := 0
	subGate := &seeltools.PermissionGate{SessionFromContext: toolspermission.SessionIDFromContext}
	subGate.Set(cfg, func(*toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
		approvals++
		return &toolspermission.ApprovalResponse{Choice: "allow"}, nil
	})
	subCtx := seenode.WithNodeScope(context.Background(), seenode.NodeScope{
		NodeID: "node-1", Role: dto.RoleSubAgent,
	})
	for _, tool := range []string{"task_complete", "fork_subagents", "switch_plugin", "computer_click", "compact_context"} {
		_, err := subGate.Middleware(0)(tool, frameworktools.ToolMeta{},
			frameworktools.HandlerFunc(func(context.Context, string) (string, error) { return "ok", nil })).
			Execute(subCtx, `{}`)
		if !errors.Is(err, frameworktools.ErrToolNotVisible) {
			t.Fatalf("%s: 子代理违权应当直接拒绝（ErrToolNotVisible），err = %v", tool, err)
		}
	}
	if approvals != 0 {
		t.Fatalf("子代理违权不该弹执行选择页面，approvals=%d", approvals)
	}

	// 员工（readwrite）：碰 adm → 走执行选择页面；人类放行后本次执行。
	empApprovals := 0
	empGate := &seeltools.PermissionGate{SessionFromContext: toolspermission.SessionIDFromContext}
	empGate.Set(cfg, func(ctx *toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
		empApprovals++
		return &toolspermission.ApprovalResponse{RequestID: ctx.Request.ID, Choice: "allow"}, nil
	})
	empGate.SetRoleSessionPolicyResolver(func(sessionID string) (string, bool) {
		if sessionID == "role-1" {
			return "readwrite", true
		}
		return "", false
	})
	empCtx := toolspermission.WithSessionID(context.Background(), "role-1")
	ran := 0
	_, err := empGate.Middleware(0)("switch_plugin", frameworktools.ToolMeta{},
		frameworktools.HandlerFunc(func(context.Context, string) (string, error) {
			ran++
			return "ok", nil
		})).
		Execute(empCtx, `{}`)
	if err != nil {
		t.Fatalf("员工违权应当走选择页面并可按放行执行，err = %v", err)
	}
	if empApprovals != 1 || ran != 1 {
		t.Fatalf("员工提权路径：approvals=%d ran=%d, want 1/1", empApprovals, ran)
	}
}
