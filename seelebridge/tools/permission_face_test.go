package tools

import (
	"context"
	"testing"

	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/Seele/types"
)

// permission_face_test.go — 员工工具面（"给员工按权限开放工具"）的验收：
//
//   - emp_ro（ToolsPolicy=readonly）面上只有 ro 组的工具；
//   - emp_rw（ToolsPolicy=readwrite）多出 rw 组（项目写位），ctl / adm / 会话 /
//     共享外设仍然不在面上；
//   - root（主代理 / 未登记会话）与 sub（子代理）**不受**本收窄影响（它们的可见性
//     口径仍在 tools/policy.go 的既有规则里）；
//   - 未分封的动态工具（MCP 等）不在员工面上（要开放先在权限配置里分封）；
//   - 未装配权责模型（只有旧 rules 的配置）时一律 fail-open，不给旧配置新增限制。
//
// 与执行侧的分工：本文件判的是"面上有没有"，"调用时放不放行"由
// permission_subject_test.go 的 Enforcer 用例覆盖（员工位缺 → 执行选择页面）。

// employeeFaceTools 是抽样工具清单：覆盖 ro / rw / rw_session / rw_desktop /
// ctl / adm 六个路由组，外加一个未分封的动态工具。
func employeeFaceTools() []string {
	return []string{
		// ro：读簇
		"read_file", "grep_search", "glob", "computer_screenshot", "todo_status", "goal_status",
		// rw：项目写簇
		"write_file", "edit_file", "bash", "todo_init", "task_add",
		// rw_session：本会话可变 transcript
		"compact_context",
		// rw_desktop：共享外设
		"computer_click",
		// ctl：循环控制流
		"task_complete", "plan_load", "goal_begin", "fork_subagents",
		// adm：能力面本身
		"switch_plugin", "plugins_reload",
		// 未分封：动态注册的工具（MCP 等）
		"mcp_custom_tool",
	}
}

func TestEmployeeToolFaceFollowsToolsPolicy(t *testing.T) {
	sessionID := "role-session-1"
	ctx := toolspermission.WithSessionID(context.Background(), sessionID)

	readOnly := employeeGate(sessionID, "readonly", nil)
	readWrite := employeeGate(sessionID, "readwrite", nil)

	roWant := map[string]bool{
		"read_file": true, "grep_search": true, "glob": true,
		"computer_screenshot": true, "todo_status": true, "goal_status": true,
	}
	rwOnlyWant := map[string]bool{
		"write_file": true, "edit_file": true, "bash": true, "todo_init": true, "task_add": true,
	}
	neverWant := map[string]bool{
		"compact_context": true, "computer_click": true, "task_complete": true,
		"plan_load": true, "goal_begin": true, "fork_subagents": true,
		"switch_plugin": true, "plugins_reload": true, "mcp_custom_tool": true,
	}

	for _, name := range employeeFaceTools() {
		class := readOnly.classFor(ctx)
		if class != SubjectClassEmployeeRO {
			t.Fatalf("classFor(%s) = %q, want %q", sessionID, class, SubjectClassEmployeeRO)
		}
		roVisible := readOnly.ToolFaceForContext(ctx, name)
		rwVisible := readWrite.ToolFaceForContext(ctx, name)
		switch {
		case roWant[name]:
			if !roVisible || !rwVisible {
				t.Errorf("只读簇工具 %s 应在两档员工面上：emp_ro=%v emp_rw=%v", name, roVisible, rwVisible)
			}
		case rwOnlyWant[name]:
			if roVisible {
				t.Errorf("项目写簇工具 %s 不该出现在只读员工面上", name)
			}
			if !rwVisible {
				t.Errorf("项目写簇工具 %s 应出现在读写员工面上", name)
			}
		case neverWant[name]:
			if roVisible || rwVisible {
				t.Errorf("无位工具 %s 不该出现在任何员工面上：emp_ro=%v emp_rw=%v", name, roVisible, rwVisible)
			}
		default:
			t.Fatalf("抽样清单缺少 %s 的期望口径", name)
		}
	}
}

func TestEmployeeToolFaceLeavesRootAndSubagentAlone(t *testing.T) {
	gate := employeeGate("role-session-1", "readonly", nil)

	// 主代理 / 未登记会话 → root：工具面不收窄（即使名字未分封）。
	mainCtx := toolspermission.WithSessionID(context.Background(), "sess-main")
	if class := gate.classFor(mainCtx); class != SubjectClassRoot {
		t.Fatalf("主会话主体类 = %q, want root", class)
	}
	for _, name := range employeeFaceTools() {
		if !gate.ToolFaceForContext(mainCtx, name) {
			t.Errorf("主代理工具面被收窄：%s", name)
		}
	}

	// 子代理 → sub：口径仍归 tools/policy.go 的节点作用域规则，本收窄不介入。
	subCtx := subagentCtx()
	if class := gate.classFor(subCtx); class != SubjectClassSub {
		t.Fatalf("子代理主体类 = %q, want sub", class)
	}
	for _, name := range employeeFaceTools() {
		if !gate.ToolFaceForContext(subCtx, name) {
			t.Errorf("子代理工具面被收窄：%s", name)
		}
	}
}

func TestEmployeeToolFaceFailsOpenWithoutPolicyModel(t *testing.T) {
	state := &PermissionGate{SessionFromContext: toolspermission.SessionIDFromContext}
	// 只有 rules 的旧配置：Groups / Subjects 为空（未装配权责模型）。
	state.Set(toolspermission.PermissionConfig{
		Mode:  toolspermission.ModeManual,
		Rules: DefaultPermissionRules(),
	}, nil)
	state.SetRoleSessionPolicyResolver(func(string) (string, bool) { return "readonly", true })

	ctx := toolspermission.WithSessionID(context.Background(), "role-session-1")
	for _, name := range employeeFaceTools() {
		if !state.ToolFaceForContext(ctx, name) {
			t.Errorf("未装配权责模型时不应收窄工具面：%s", name)
		}
	}
}

// TestPolicyFilterAppliesEmployeeFace 是跨层验收：可见性策略（tools/policy.go）
// 经 PolicyDeps.ToolFace 委托权限门，员工的工具列表真的少了无位的工具——覆盖
// "装配点接错 / 忘了传 ToolFace" 这类线断在这里的失效。
func TestPolicyFilterAppliesEmployeeFace(t *testing.T) {
	sessionID := "role-session-1"
	gate := employeeGate(sessionID, "readonly", nil)
	policy := NewPolicy(PolicyDeps{
		ToolFace: func(ctx context.Context, toolName string) bool {
			return gate.ToolFaceForContext(ctx, toolName)
		},
	})

	employeeCtx := toolspermission.WithSessionID(context.Background(), sessionID)
	got := policy.Filter(employeeCtx, toolList(
		"read_file", "grep_search", "computer_screenshot", "write_file", "edit_file",
		"bash", "compact_context", "computer_click", "task_complete", "switch_plugin",
	))
	want := []string{"read_file", "grep_search", "computer_screenshot"}
	if len(got) != len(want) {
		t.Fatalf("只读员工可见工具 = %v, want %v", names(got), want)
	}
	for index, name := range want {
		if got[index].Function.Name != name {
			t.Fatalf("只读员工可见工具 = %v, want %v", names(got), want)
		}
	}

	// 主代理：同一份策略、同一份工具清单，一个不少（含写工具与共享外设）。
	mainCtx := toolspermission.WithSessionID(context.Background(), "sess-main")
	mainGot := policy.Filter(mainCtx, toolList(
		"read_file", "write_file", "edit_file", "bash", "compact_context",
		"computer_click", "task_complete", "switch_plugin",
	))
	if len(mainGot) != 8 {
		t.Fatalf("主代理可见工具 = %v, want 全部可见", names(mainGot))
	}
}

// toolList 造工具清单（与 policy_test.go 的 planTool 同形，只是批量）。
func toolList(names ...string) []types.Tool {
	tools := make([]types.Tool, 0, len(names))
	for _, name := range names {
		tools = append(tools, planTool(name))
	}
	return tools
}
