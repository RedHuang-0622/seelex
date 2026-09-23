package tools

import (
	"context"
	"errors"
	"strconv"
	"testing"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// permission_subject_test.go — 权责模型的主体口径验收：
//
//   - 子代理（sub）位缺/未分封 = 违权 → **直接拒绝**，永不落到执行选择页面；
//   - 子代理位齐 → 放行（无人可问，作用域另有 worktree / ProjectScope）；
//   - 员工（emp_*）位缺 = 违权 → **走执行选择页面提权**（人类 sudo 口令）；
//   - 主代理（root）判定不受主体类策略影响（另有 main 包的逐工具不回归用例）。

// subagentCtx 造一个子代理工具调度 ctx（plan kind:agent 节点 / fork 派生同形）。
func subagentCtx() context.Context {
	return model.WithNodeScope(context.Background(), model.NodeScope{
		NodeID: "node-1", Role: model.RoleSubAgent, BranchID: "node-1",
	})
}

// employeeGate 造一个员工会话的权限门：sessionID 是角色会话，解析器给出 ToolsPolicy。
// 会话归属按 ctx 解析（与 seelebridge 装配同形：SessionFromContext = ctx 里的会话）。
func employeeGate(sessionID, toolsPolicy string, approval toolspermission.ApprovalHandler) *PermissionGate {
	state := &PermissionGate{
		SessionFromContext: toolspermission.SessionIDFromContext,
	}
	state.Set(DefaultPermissionConfig(), approval)
	state.SetRoleSessionPolicyResolver(func(got string) (string, bool) {
		if got != sessionID {
			return "", false
		}
		return toolsPolicy, true
	})
	return state
}

// callTool 走一次完整工具调用（无参），返回错误与被调用次数。
func callTool(t *testing.T, state *PermissionGate, ctx context.Context, name string) (error, int) {
	t.Helper()
	return callToolArgs(t, state, ctx, name, `{}`)
}

// callToolArgs 走一次带参工具调用，返回错误与被调用次数。
func callToolArgs(t *testing.T, state *PermissionGate, ctx context.Context, name, argsJSON string) (error, int) {
	t.Helper()
	ran := 0
	handler := state.Middleware(0)(name, frameworktools.ToolMeta{},
		frameworktools.HandlerFunc(func(context.Context, string) (string, error) {
			ran++
			return "ok", nil
		}))
	_, err := handler.Execute(ctx, argsJSON)
	return err, ran
}

// TestSubagentOutOfAuthorityIsDeniedWithoutPrompt：子代理碰 ctl / adm / 共享外设
// （位缺 = 违权）与未分封工具，一律直接拒绝，且**不弹执行选择页面**。
func TestSubagentOutOfAuthorityIsDeniedWithoutPrompt(t *testing.T) {
	approvals := 0
	state := sessionScopedGate("sess-sub", DefaultPermissionConfig(),
		func(*toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
			approvals++
			return &toolspermission.ApprovalResponse{Choice: "allow"}, nil
		})

	cases := []struct {
		name string
		tool string
		want error
	}{
		{name: "ctl 断位（叫停 loop）", tool: "task_complete", want: frameworktools.ErrToolNotVisible},
		{name: "ctl 断位（派生/装载）", tool: "fork_subagents", want: frameworktools.ErrToolNotVisible},
		{name: "adm 断位（换能力面）", tool: "switch_plugin", want: frameworktools.ErrToolNotVisible},
		{name: "adm 断位（连 MCP）", tool: "mcp_load", want: frameworktools.ErrToolNotVisible},
		{name: "共享外设断位（桌面写）", tool: "computer_click", want: frameworktools.ErrToolNotVisible},
		{name: "会话 transcript 断位", tool: "compact_context", want: frameworktools.ErrToolNotVisible},
		{name: "未分封工具", tool: "no_such_tool", want: frameworktools.ErrPermissionDenied},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err, ran := callTool(t, state, subagentCtx(), tc.tool)
			if err == nil {
				t.Fatalf("%s: 子代理违权调用应当被拒绝", tc.tool)
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("%s: err = %v, want errors.Is(..., %v)", tc.tool, err, tc.want)
			}
			if ran != 0 {
				t.Fatalf("%s: 被拒绝的工具不应执行", tc.tool)
			}
		})
	}
	if approvals != 0 {
		t.Fatalf("子代理没有人类在环：违权必须直接拒绝，approvals=%d", approvals)
	}
}

// TestSubagentAuthorizedCallsRunWithoutPrompt：子代理位齐 → 放行，且"规则要求
// 问人"的调用也不弹页面（无人可问；位已经表达了它的授权范围）。
func TestSubagentAuthorizedCallsRunWithoutPrompt(t *testing.T) {
	approvals := 0
	state := sessionScopedGate("sess-sub", DefaultPermissionConfig(),
		func(*toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
			approvals++
			return &toolspermission.ApprovalResponse{Choice: "allow"}, nil
		})

	for _, tool := range []string{"read_file", "grep_search", "write_file", "edit_file", "bash", "todo_init", "read_tool_result"} {
		t.Run(tool, func(t *testing.T) {
			err, ran := callTool(t, state, subagentCtx(), tool)
			if err != nil {
				t.Fatalf("%s: 子代理位齐的调用应当放行，err = %v", tool, err)
			}
			if ran != 1 {
				t.Fatalf("%s: 放行的调用应当执行一次，ran=%d", tool, ran)
			}
		})
	}
	if approvals != 0 {
		t.Fatalf("子代理位齐的调用不该问人，approvals=%d", approvals)
	}
}

// TestSubagentHardDenyStaysDenied：显式 deny 规则对子代理同样硬（位齐也不放行）。
// patterns 匹配的是命令本体（policyArgsFor 归一化），不是工具入参 JSON。
func TestSubagentHardDenyStaysDenied(t *testing.T) {
	state := sessionScopedGate("sess-sub", DefaultPermissionConfig(), nil)

	ran := 0
	handler := state.Middleware(0)("bash", frameworktools.ToolMeta{},
		frameworktools.HandlerFunc(func(context.Context, string) (string, error) {
			ran++
			return "ok", nil
		}))
	_, err := handler.Execute(subagentCtx(), `{"command":"rm -rf /"}`)
	if err == nil {
		t.Fatal("显式 deny 规则对子代理同样硬：应当拒绝")
	}
	if !errors.Is(err, frameworktools.ErrPermissionDenied) {
		t.Fatalf("err = %v, want errors.Is(..., ErrPermissionDenied)", err)
	}
	if errors.Is(err, frameworktools.ErrToolNotVisible) {
		t.Fatalf("显式拒绝应以策略拒绝（EPERM）报出，而不是不可见：%v", err)
	}
	if ran != 0 {
		t.Fatalf("被拒绝的调用不应执行，ran=%d", ran)
	}
}

// TestBashPatternRulesAreLive：命令行工具的规则 patterns 按命令本体匹配（否则
// seele.yaml 里的"能力白名单 / 危险命令拒绝"整块失效）。
func TestBashPatternRulesAreLive(t *testing.T) {
	approvals := 0
	state := sessionScopedGate("sess-main", DefaultPermissionConfig(),
		func(*toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
			approvals++
			return &toolspermission.ApprovalResponse{Choice: "allow"}, nil
		})

	cases := []struct {
		name     string
		command  string
		wantErr  bool
		wantPage bool
	}{
		{name: "安全命令白名单 → 直接执行", command: "git status", wantPage: false},
		{name: "测试命令白名单 → 直接执行", command: "go test ./...", wantPage: false},
		{name: "越界命令 → 问人", command: "python build.py", wantPage: true},
		{name: "危险命令 → 直接拒绝", command: "rm -rf /", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := approvals
			err, ran := callToolArgs(t, state, context.Background(), "bash", `{"command":`+strconv.Quote(tc.command)+`}`)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("%s: 危险命令应当被拒绝", tc.command)
				}
				if !errors.Is(err, frameworktools.ErrPermissionDenied) {
					t.Fatalf("err = %v, want errors.Is(..., ErrPermissionDenied)", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: err = %v", tc.command, err)
			}
			if ran != 1 {
				t.Fatalf("%s: 应当执行一次，ran=%d", tc.command, ran)
			}
			if tc.wantPage && approvals != before+1 {
				t.Fatalf("%s: 应当问人一次", tc.command)
			}
			if !tc.wantPage && approvals != before {
				t.Fatalf("%s: 白名单命令不该问人", tc.command)
			}
		})
	}
}

// TestEmployeeMissingBitGoesToApprovalPage：员工位缺（违权）→ 走执行选择页面提权，
// 人类放行后本次调用执行（提权 = 本次放行 + 框架按 scope 记住）。
func TestEmployeeMissingBitGoesToApprovalPage(t *testing.T) {
	cases := []struct {
		name     string
		policy   string
		tool     string
		wantPage bool
	}{
		{name: "readwrite 员工碰 ctl", policy: "readwrite", tool: "task_complete", wantPage: true},
		{name: "readwrite 员工碰 adm", policy: "readwrite", tool: "switch_plugin", wantPage: true},
		{name: "readwrite 员工写文件（规则 ask）", policy: "readwrite", tool: "write_file", wantPage: true},
		{name: "readonly 员工写文件（位缺）", policy: "readonly", tool: "write_file", wantPage: true},
		{name: "readonly 员工读文件（位齐 + 组默认）", policy: "readonly", tool: "read_file", wantPage: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			approvals := 0
			state := employeeGate("role-session-1", tc.policy,
				func(ctx *toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
					approvals++
					return &toolspermission.ApprovalResponse{RequestID: ctx.Request.ID, Choice: "allow"}, nil
				})
			ctx := toolspermission.WithSessionID(context.Background(), "role-session-1")

			err, ran := callTool(t, state, ctx, tc.tool)
			if err != nil {
				t.Fatalf("%s: 员工调用应当经选择页面放行，err = %v", tc.tool, err)
			}
			if ran != 1 {
				t.Fatalf("%s: 放行后应当执行一次，ran=%d", tc.tool, ran)
			}
			if tc.wantPage && approvals != 1 {
				t.Fatalf("%s: 应当走一次执行选择页面，approvals=%d", tc.tool, approvals)
			}
			if !tc.wantPage && approvals != 0 {
				t.Fatalf("%s: 位齐且组默认 allow 的调用不该问人，approvals=%d", tc.tool, approvals)
			}
		})
	}
}

// TestEmployeePageDenialKeepsCallDenied：员工在页面上拒绝 → 调用被拒（提权不是自动放行）。
func TestEmployeePageDenialKeepsCallDenied(t *testing.T) {
	state := employeeGate("role-session-1", "readonly",
		func(*toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
			return &toolspermission.ApprovalResponse{Choice: "deny"}, nil
		})
	ctx := toolspermission.WithSessionID(context.Background(), "role-session-1")

	err, ran := callTool(t, state, ctx, "write_file")
	if err == nil {
		t.Fatal("页面上选拒绝后调用必须被拒")
	}
	if !errors.Is(err, frameworktools.ErrPermissionDenied) {
		t.Fatalf("err = %v, want errors.Is(..., ErrPermissionDenied)", err)
	}
	if ran != 0 {
		t.Fatalf("被拒绝的调用不应执行，ran=%d", ran)
	}
}

// TestEmployeeToolsPolicyMapsToSubjectClass：ToolsPolicy 口径 → 主体类。
func TestEmployeeToolsPolicyMapsToSubjectClass(t *testing.T) {
	cases := map[string]SubjectClass{
		"":          SubjectClassRoot,
		"readonly":  SubjectClassEmployeeRO,
		"readwrite": SubjectClassEmployeeRW,
		"full":      SubjectClassRoot,
		" inherit ": SubjectClassRoot,
		"unknown":   SubjectClassRoot,
	}
	for policy, want := range cases {
		if got := ClassForToolsPolicy(policy); got != want {
			t.Fatalf("ClassForToolsPolicy(%q) = %q, want %q", policy, got, want)
		}
	}
}

// TestSubjectClassFromContext：同一份配置下，主体类由 ctx（节点作用域 / 角色会话）
// 决定，且不越会话传播。
func TestSubjectClassFromContext(t *testing.T) {
	state := employeeGate("role-session-1", "readwrite", nil)

	if got := state.classFor(context.Background()); got != SubjectClassRoot {
		t.Fatalf("无 ctx 标记 = root，got %q", got)
	}
	if got := state.classFor(subagentCtx()); got != SubjectClassSub {
		t.Fatalf("子代理节点作用域 = sub，got %q", got)
	}
	if got := state.classFor(toolspermission.WithSessionID(context.Background(), "role-session-1")); got != SubjectClassEmployeeRW {
		t.Fatalf("员工角色会话 = emp_rw，got %q", got)
	}
	if got := state.classFor(toolspermission.WithSessionID(context.Background(), "other-session")); got != SubjectClassRoot {
		t.Fatalf("未登记会话 = root，got %q", got)
	}
}

// TestEmployeePolicyResolverCached：角色会话解析结果按 TTL 缓存，且换解析器即作废。
func TestEmployeePolicyResolverCached(t *testing.T) {
	calls := 0
	state := &PermissionGate{SessionFromContext: toolspermission.SessionIDFromContext}
	state.Set(DefaultPermissionConfig(), nil)
	state.SetRoleSessionPolicyResolver(func(string) (string, bool) {
		calls++
		return "readonly", true
	})
	ctx := toolspermission.WithSessionID(context.Background(), "role-session-1")
	for i := 0; i < 5; i++ {
		if got := state.classFor(ctx); got != SubjectClassEmployeeRO {
			t.Fatalf("员工角色会话 = emp_ro，got %q", got)
		}
	}
	if calls != 1 {
		t.Fatalf("同一会话的解析应当命中缓存，calls=%d", calls)
	}
	state.SetRoleSessionPolicyResolver(func(string) (string, bool) { return "readwrite", true })
	if got := state.classFor(ctx); got != SubjectClassEmployeeRW {
		t.Fatalf("换解析器后缓存应作废，got %q", got)
	}
}

// TestSubagentKeepsBitsUnderSessionFullAccess：子代理不因会话全权而获得 ctl/adm
// （全权是会话自己的用户决定，不该把无权主体变成有权主体）。
func TestSubagentKeepsBitsUnderSessionFullAccess(t *testing.T) {
	state := sessionScopedGate("sess-sub", DefaultPermissionConfig(), nil)
	state.SetFullAccessFor("sess-sub", true)

	err, ran := callTool(t, state, subagentCtx(), "task_complete")
	if err == nil {
		t.Fatal("子代理在会话全权下仍须保持 ctl 断位")
	}
	if !errors.Is(err, frameworktools.ErrToolNotVisible) {
		t.Fatalf("err = %v, want errors.Is(..., ErrToolNotVisible)", err)
	}
	if ran != 0 {
		t.Fatalf("被拒绝的调用不应执行，ran=%d", ran)
	}
}

// TestSubagentVisibleToolsAreAuthorized：可见性名单与位断言一致——子代理看得见的
// 每个工具都能真的调用（位齐），看不见的不必再试。防止"可见但一调就被拒"。
func TestSubagentVisibleToolsAreAuthorized(t *testing.T) {
	state := sessionScopedGate("sess-sub", DefaultPermissionConfig(), nil)
	visible := []string{
		"read_file", "read_plan", "read_tool_result", "read_compressed_turn", "search_history",
		"grep_search", "glob", "get_time", "web_search", "todo_status", "todolist_status",
		"plugins_list", "skills_list", "mcp_list", "plan_validate", "plan_export",
		"computer_screenshot", "computer_windows", "computer_scroll_targets", "computer_wait",
		"write_file", "edit_file", "bash",
		"todo_init", "todo_add", "todo_done", "todolist_init", "todolist_add", "todolist_done",
		"task_add", "taskadd", "project_refresh", "plugin_create", "skill_create",
	}
	for _, tool := range visible {
		ctx := subagentCtx()
		if err, ran := callTool(t, state, ctx, tool); err != nil || ran != 1 {
			t.Fatalf("%s 对子代理应当位齐可调用，err = %v, ran = %d", tool, err, ran)
		}
	}
}

// TestRoutePermissionGroupIsLastMatchWins：路由 LMRW（最后匹配胜出）与分组不重叠。
func TestRoutePermissionGroupIsLastMatchWins(t *testing.T) {
	groups := DefaultPermissionGroupList()
	cases := map[string]string{
		"read_file":           GroupRO,
		"write_file":          GroupRW,
		"compact_context":     GroupRWSession,
		"computer_click":      GroupRWDesktop,
		"computer_screenshot": GroupRO,
		"task_complete":       GroupCTL,
		"switch_plugin":       GroupADM,
	}
	for tool, want := range cases {
		group, ok := RoutePermissionGroup(groups, tool)
		if !ok {
			t.Fatalf("%s 未分封", tool)
		}
		if group.Name != want {
			t.Fatalf("%s 路由到 %q, want %q", tool, group.Name, want)
		}
	}
	if _, ok := RoutePermissionGroup(groups, "no_such_tool"); ok {
		t.Fatal("未分封工具不应路由出组")
	}
}
