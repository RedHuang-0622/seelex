package tools

import (
	"testing"

	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
)

// permission_teammate_test.go — **teammate 越权**用例（2026-10-03 Work Item 重构）。
//
// 它钉住的是 leader 在 `team_plan` 里逐格下发的权责口径如何决定 teammate 的工具面：
//
//   - **位内直通**（赋权即免审批）：leader 给了写位，写文件/执行命令就照常走，
//     不再落回框架 rules 的"问人"——宿主主会话正忙着等这份产出，页面弹不出来就是死锁；
//   - **位外提权**（越权）：位缺 / 未分封 → 走执行选择页面，由人类给口令（单次放行）；
//   - **能力面断位**（ctl / adm / 共享外设）无论档位多高都不给员工——满档也不连带放行。
//
// 口径与 `seelebridge/runtime_role_turn.go` 的接线同源：
// `WithEmployeeGrant(ctx, role, toolsPolicy, permission_groups)`（逐格格子非空则以格子为准）。

// teammateCtx 的口径见本文件头注：teammate 工具回合的调度 ctx = 角色名 + 权责档 +
// 逐格权限格子（与生产接线同一条函数）。

func TestTeammateInGrantRunsDirectlyAndOutOfGrantGoesToTheEscalationPage(t *testing.T) {
	cases := []struct {
		name       string
		role       string
		policy     string
		groups     map[string]uint8
		tool       string
		wantPage   bool
		wantDenied bool
	}{
		{name: "readwrite teammate 写文件（位内 → 直通免审批）", role: "exec", policy: "readwrite", tool: "write_file"},
		{name: "readwrite teammate 跑命令（位内 → 直通）", role: "exec", policy: "readwrite", tool: "bash"},
		{name: "readonly teammate 读文件（位内）", role: "review", policy: "readonly", tool: "read_file"},
		{name: "readonly teammate 写文件（越权 → 提权页）", role: "review", policy: "readonly", tool: "write_file", wantPage: true},
		{name: "逐格格子只给 ro：写文件越权（格子优先于档位）", role: "audit", policy: "readwrite", groups: map[string]uint8{GroupRO: GroupBitRead}, tool: "write_file", wantPage: true},
		{name: "逐格格子给 rw：写文件位内（格子优先于档位）", role: "audit2", policy: "readonly", groups: map[string]uint8{GroupRO: GroupBitRead, GroupRW: GroupBitRead | GroupBitWrite}, tool: "write_file"},
		{name: "任何 teammate 碰 ctl（能力面断位 → 越权）", role: "exec", policy: "readwrite", tool: "task_complete", wantPage: true},
		{name: "任何 teammate 碰 adm（能力面断位 → 越权）", role: "exec", policy: "readwrite", tool: "switch_plugin", wantPage: true},
		{name: "任何 teammate 碰共享外设（断位 → 越权）", role: "exec", policy: "readwrite", tool: "computer_click", wantPage: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			approvals := 0
			state := sessionScopedGate("sess-teammate", DefaultPermissionConfig(),
				func(ctx *toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
					approvals++
					return &toolspermission.ApprovalResponse{RequestID: ctx.Request.ID, Choice: "allow"}, nil
				})
			ctx := WithEmployeeGrant(probeCtx("sess-teammate"), tc.role, tc.policy, tc.groups)

			err, ran := callTool(t, state, ctx, tc.tool)
			if tc.wantDenied {
				if err == nil {
					t.Fatalf("越权调用必须被拒")
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: err = %v", tc.tool, err)
			}
			if ran != 1 {
				t.Fatalf("%s: 放行后应当执行一次，ran=%d", tc.tool, ran)
			}
			if tc.wantPage && approvals != 1 {
				t.Fatalf("%s: 越权必须落到执行选择页面（人类口令），approvals=%d", tc.tool, approvals)
			}
			if !tc.wantPage && approvals != 0 {
				t.Fatalf("%s: 位内调用不该弹审批页（赋权即免审批），approvals=%d", tc.tool, approvals)
			}
		})
	}
}

// TestTeammateEscalationDeniedKeepsTheCallDenied：越权提权页面上选拒绝 → 调用被拒
// （提权是"单次口令"，不是自动放行）。
func TestTeammateEscalationDeniedKeepsTheCallDenied(t *testing.T) {
	state := sessionScopedGate("sess-teammate", DefaultPermissionConfig(),
		func(*toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
			return &toolspermission.ApprovalResponse{Choice: "deny"}, nil
		})
	ctx := WithEmployeeGrant(probeCtx("sess-teammate"), "review", "readonly", nil)
	err, ran := callTool(t, state, ctx, "write_file")
	if err == nil {
		t.Fatal("页面上选拒绝后越权调用必须被拒")
	}
	if ran != 0 {
		t.Fatalf("被拒的调用不该执行，ran=%d", ran)
	}
}

// TestTeammateCapabilityFaceStaysClosedUnderFullTier：满档（full）也不连带放行员工的
// 能力面调用（越权面不因档位而开）。
func TestTeammateCapabilityFaceStaysClosedUnderFullTier(t *testing.T) {
	approvals := 0
	state := sessionScopedGate("sess-teammate", DefaultPermissionConfig(),
		func(ctx *toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
			approvals++
			return &toolspermission.ApprovalResponse{RequestID: ctx.Request.ID, Choice: "allow"}, nil
		})
	// 主体条目由 ctx 里的员工身份**按构造**物化（emp_exec 的位来自这一轮下发的口径）。
	ctx := WithEmployeeGrant(probeCtx("sess-teammate"), "exec", "readwrite", nil)

	// 位内的写：直通。
	if err, ran := callTool(t, state, ctx, "write_file"); err != nil || ran != 1 {
		t.Fatalf("位内写文件应当直通：err=%v ran=%d", err, ran)
	}
	// 能力面：仍走提权页（full 档不得连带放行）。
	if err, _ := callTool(t, state, ctx, "fork_subagents"); err != nil {
		t.Fatalf("能力面调用应当经提权页处理而不是硬拒：%v", err)
	}
	if approvals != 1 {
		t.Fatalf("能力面越权应当恰好弹一次提权页，approvals=%d", approvals)
	}
}
