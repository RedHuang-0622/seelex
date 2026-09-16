package tools

// permission_inherit_test.go — 「继承宿主默认」口径的端到端验收面。
//
// 用户口径（逐字）：**如果是继承的话，在会话的工具中继承全量的工具，通过中间件的
// 权限审查来获取他有没有使用这个工具的权限，并且对于需要越权使用的工具提供人为
// 审批面板（直接用现有的）**。
//
// 本文件把这句话钉成三条可证伪的断言：
//  1. 继承口径的员工（ToolsPolicy 空）**工具面 = 全量**（不收窄成只读/读写簇）；
//  2. 调用时**由中间件判定**：位齐/规则命中 → 放行，规则要求问人 → 走审批页，
//     显式 deny → 直接拒绝（"面上有"不等于"随便用"）；
//  3. 审批请求按**宿主主会话**归属（RoleSessionOwner 折算），因此落在**现有**审批
//     面板上——角色会话自己没有会话单元/视图单格，不折算就等于没有人能点头。

import (
	"context"
	"testing"

	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
)

// inheritEmployeeCtx 造一个"继承宿主默认"的员工回合 ctx：与角色回合执行体
// （seelebridge.runRoleRound）同形——会话号 + 角色身份（档位空 = 继承）。
func inheritEmployeeCtx(roleSessionID, roleName string) context.Context {
	return WithEmployeeGrant(
		toolspermission.WithSessionID(context.Background(), roleSessionID),
		roleName, "",
		nil,
	)
}

// TestInheritEmployeeInheritsFullToolFace：继承口径的员工工具面 = 全量
// （与宿主 root 同一张面），不被收窄。
func TestInheritEmployeeInheritsFullToolFace(t *testing.T) {
	const roleSessionID = "goal-a2a-owner"
	state := employeeGate(roleSessionID, "", nil)
	ctx := inheritEmployeeCtx(roleSessionID, "owner")

	if class := state.classFor(ctx); class != SubjectClassRoot {
		t.Fatalf("继承口径员工主体类 = %q, want root（继承宿主默认）", class)
	}
	for _, name := range employeeFaceTools() {
		if !state.ToolFaceForContext(ctx, name) {
			t.Fatalf("继承口径员工的工具面不该被收窄：%s 不在面上", name)
		}
	}
	// 对照：显式只读档**必须**被收窄，否则"收窄"这件事没有被证明存在。
	readOnly := employeeGate(roleSessionID, "readonly", nil)
	readonlyCtx := toolspermission.WithSessionID(context.Background(), roleSessionID)
	if readOnly.ToolFaceForContext(readonlyCtx, "write_file") {
		t.Fatal("只读档员工的写工具不该在面上（对照组失败=口径没有分支）")
	}
}

// TestInheritEmployeeCallsGoThroughMiddlewareReview：继承口径下"面上有"不等于
// "随便用"——每一次调用都由中间件按位/规则判定。
func TestInheritEmployeeCallsGoThroughMiddlewareReview(t *testing.T) {
	approvals := 0
	state := employeeGate("goal-a2a-owner", "",
		func(ctx *toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
			approvals++
			return &toolspermission.ApprovalResponse{RequestID: ctx.Request.ID, Choice: "allow"}, nil
		})
	ctx := inheritEmployeeCtx("goal-a2a-owner", "owner")

	// 读工具：位齐 + 组默认 allow → 直接执行，不问人。
	if err, ran := callToolArgs(t, state, ctx, "read_file", `{}`); err != nil || ran != 1 {
		t.Fatalf("读工具应放行：err=%v ran=%d", err, ran)
	}
	if approvals != 0 {
		t.Fatalf("位齐且组默认 allow 的调用不该问人，approvals=%d", approvals)
	}

	// 规则要求问人的工具（write_file）：走审批页；人类放行后执行。
	err, ran := callToolArgs(t, state, ctx, "write_file", `{}`)
	if err != nil || ran != 1 {
		t.Fatalf("write_file 应经审批页放行：err=%v ran=%d", err, ran)
	}
	if approvals != 1 {
		t.Fatalf("write_file 应走一次审批页，approvals=%d", approvals)
	}

	// 危险命令：显式 deny 硬，不因"继承全量"而变成可放开。
	err, ran = callToolArgs(t, state, ctx, "bash", `{"command":"rm -rf /"}`)
	if err == nil {
		t.Fatal("危险命令必须直接拒绝（继承全量不改变 deny 的硬度）")
	}
	if ran != 0 {
		t.Fatalf("被拒绝的调用不应执行，ran=%d", ran)
	}
}

// TestEmployeeApprovalAttributedToOwnerSession：审批请求按宿主主会话归属
// （折算前是角色会话号——用户看不见，等于没有面板）。
func TestEmployeeApprovalAttributedToOwnerSession(t *testing.T) {
	const (
		roleSessionID = "goal-a2a-owner"
		mainSessionID = "sess-main"
	)
	cases := []struct {
		name      string
		resolver  func(string) (string, bool)
		wantOwner string
	}{
		{
			name:      "注入折算：角色会话 → 宿主主会话",
			resolver:  func(id string) (string, bool) { return mainSessionID, id == roleSessionID },
			wantOwner: mainSessionID,
		},
		{
			name:      "未命中：按调用会话原样归属（不冒充别人的会话）",
			resolver:  func(string) (string, bool) { return "", false },
			wantOwner: roleSessionID,
		},
		{
			name:      "未注入折算：旧行为",
			resolver:  nil,
			wantOwner: roleSessionID,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var seen []string
			state := employeeGate(roleSessionID, "",
				func(ctx *toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
					seen = append(seen, ctx.Request.SessionID)
					return &toolspermission.ApprovalResponse{RequestID: ctx.Request.ID, Choice: "allow"}, nil
				})
			state.SetRoleSessionOwnerResolver(tc.resolver)

			err, ran := callToolArgs(t, state, inheritEmployeeCtx(roleSessionID, "owner"), "write_file", `{}`)
			if err != nil || ran != 1 {
				t.Fatalf("write_file 应经审批页放行：err=%v ran=%d", err, ran)
			}
			if len(seen) != 1 {
				t.Fatalf("审批请求数 = %d, want 1", len(seen))
			}
			if seen[0] != tc.wantOwner {
				t.Fatalf("审批归属 = %q, want %q", seen[0], tc.wantOwner)
			}
		})
	}
}

// TestEmployeeApprovalOwnershipDoesNotChangeJudgement：折算只改"审批弹在哪"，
// 不改"按谁判"——只读员工折算到宿主会话后，写调用**仍然**走审批页，不因归属变化
// 被静默放行。
func TestEmployeeApprovalOwnershipDoesNotChangeJudgement(t *testing.T) {
	approvals := 0
	state := employeeGate("goal-a2a-reviewer", "readonly",
		func(ctx *toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
			approvals++
			return &toolspermission.ApprovalResponse{RequestID: ctx.Request.ID, Choice: "deny"}, nil
		})
	state.SetRoleSessionOwnerResolver(func(id string) (string, bool) { return "sess-main", true })

	ctx := toolspermission.WithSessionID(context.Background(), "goal-a2a-reviewer")
	err, ran := callToolArgs(t, state, ctx, "write_file", `{}`)
	if err == nil {
		t.Fatal("只读员工折算到宿主会话后，写调用仍必须被拦（归属不改变判定）")
	}
	if ran != 0 {
		t.Fatalf("被拒绝的调用不应执行，ran=%d", ran)
	}
	if approvals != 1 {
		t.Fatalf("违权调用应走一次审批页（折算只改归属），approvals=%d", approvals)
	}
	// 主体仍是这个员工自己：折算不把判定降级成宿主 root。
	employeeCtx := WithEmployeeGrant(
		toolspermission.WithSessionID(context.Background(), "goal-a2a-reviewer"),
		"reviewer", "readonly", nil,
	)
	if subject := state.resolveEmployeeSubject(employeeCtx, SubjectClassEmployeeRO); subject != "emp_reviewer" {
		t.Fatalf("折算后员工主体 = %q, want emp_reviewer", subject)
	}
}
