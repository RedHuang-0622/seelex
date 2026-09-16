package tools

// permission_employee_grant_test.go — 「员工的权限像用户权限一样分配」的验收面。
//
// 口径（设计）：用户（root）的权限是权责表里的一条**主体条目**（主体 × 路由组 × 位）。
// 员工权限过去只有三档枚举（readonly/readwrite/full）映射到共享主体 emp_ro/emp_rw，
// 于是"给某个员工单独开一格能力"无处表达：要么新增档位、要么改判定代码。
//
// 现在一个员工 = 一个主体 emp_<角色名>，与 root 同表同形：
//   - 装配期可以用**逐组位**分配（EmployeePermission.Groups），未分配则按档位默认；
//   - 判定（Enforce）与工具面（ToolFaceForContext）都按这个员工自己的主体算；
//   - 同档位的两个员工可以有不同能力面——这是三档枚举做不到的，也是本文件的主断言。

import (
	"context"
	"strings"
	"testing"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
)

// employeeCtx 造一个"员工执行面"的工具调度 ctx：起手带着角色名与权责档
// （WithEmployeeSubject），与角色回合执行体的构造同形。
func employeeCtx(roleSessionID, roleName, policy string) context.Context {
	return WithEmployeeSubject(
		toolspermission.WithSessionID(context.Background(), roleSessionID),
		roleName,
		policy,
	)
}

// TestEmployeeSubjectNameNormalizes：主体名会写进配置文件当键，必须可读、稳定，
// 且**不能因为归一化把两个员工并成一个主体**（共享主体 = 权限互相泄漏）。
func TestEmployeeSubjectNameNormalizes(t *testing.T) {
	cases := []struct {
		name string
		role string
		want toolspermission.Subject
		ok   bool
	}{
		{name: "ASCII 角色名保持可读", role: "pm", want: "emp_pm", ok: true},
		{name: "大小写与下划线", role: "Test_Case", want: "emp_test_case", ok: true},
		{name: "连字符", role: "code-reviewer", want: "emp_code-reviewer", ok: true},
		{name: "空名不是员工", role: "", want: "", ok: false},
		{name: "纯空白不是员工", role: "   ", want: "", ok: false},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			subject, ok := EmployeeSubject(item.role)
			if ok != item.ok {
				t.Fatalf("EmployeeSubject(%q) ok = %v, want %v", item.role, ok, item.ok)
			}
			if ok && subject != item.want {
				t.Fatalf("EmployeeSubject(%q) = %q, want %q", item.role, subject, item.want)
			}
		})
	}

	// 非 ASCII 角色名：可辨认（保留 ASCII 残段）+ 不撞车（追加短哈希）。
	first, ok := EmployeeSubject("评审官 1")
	if !ok || !strings.HasPrefix(string(first), "emp_1_") {
		t.Fatalf("非 ASCII 角色名主体 = %q（应带 ASCII 残段与哈希后缀）", first)
	}
	second, _ := EmployeeSubject("工人 1")
	if first == second {
		t.Fatalf("不同角色名不能归一到同一主体（%q）：共享主体会让两个员工互相看得见权限", first)
	}
	if again, _ := EmployeeSubject("评审官 1"); again != first {
		t.Fatalf("同一角色名必须稳定：%q vs %q", again, first)
	}
	if pure, ok := EmployeeSubject("评审官"); !ok || pure == first || !strings.HasPrefix(string(pure), "emp_") {
		t.Fatalf("纯非 ASCII 角色名主体 = %q", pure)
	}
}

// TestEmployeePermissionsAssignCapabilitiesBeyondTiers：显式分组位可以给出档位
// 表达不了的能力面（readwrite 员工 + 共享外设位 = 允许用桌面），且**只对这一位员工**
// 生效——另一个同档位员工不受影响。
func TestEmployeePermissionsAssignCapabilitiesBeyondTiers(t *testing.T) {
	state := &PermissionGate{SessionFromContext: toolspermission.SessionIDFromContext}
	state.Set(DefaultPermissionConfig(), nil)
	err := state.SetEmployeePermissions([]EmployeePermission{
		{RoleName: "runner", Policy: "readwrite", Groups: map[string]uint8{
			GroupRO:        GroupBitRead,
			GroupRW:        GroupBitRead | GroupBitWrite,
			GroupRWDesktop: GroupBitRead | GroupBitWrite, // 档位给不出的那一格
			GroupCTL:       0,
			GroupADM:       0,
		}},
		{RoleName: "pm", Policy: "readwrite"},
	})
	if err != nil {
		t.Fatalf("SetEmployeePermissions: %v", err)
	}

	runner := employeeCtx("goal-a2a-runner", "runner", "readwrite")
	pm := employeeCtx("goal-a2a-pm", "pm", "readwrite")

	if !state.ToolFaceForContext(runner, "computer_click") {
		t.Fatal("runner 被分配了共享外设位，桌面写工具应在它的能力面上")
	}
	if state.ToolFaceForContext(pm, "computer_click") {
		t.Fatal("pm 没有共享外设位（档位默认），桌面写工具不该出现在它面上——同档位不等于同能力")
	}
	for _, tool := range []string{"read_file", "write_file"} {
		if !state.ToolFaceForContext(pm, tool) {
			t.Fatalf("pm（readwrite 档）的能力面应包含 %s", tool)
		}
	}
	// 分配结果可巡检（诊断面）。
	if len(state.EmployeePermissions()) != 2 {
		t.Fatalf("分配记录 = %+v", state.EmployeePermissions())
	}
}

// TestEmployeeAssignedGroupsOverrideTierOnEnforcement：分配的分组位在判定时生效
// ——档位说 readwrite，分配只给读位 → 写调用不是"按档放行"，而是走执行选择页面提权。
func TestEmployeeAssignedGroupsOverrideTierOnEnforcement(t *testing.T) {
	approvals := 0
	state := &PermissionGate{SessionFromContext: toolspermission.SessionIDFromContext}
	state.Set(DefaultPermissionConfig(), func(*toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
		approvals++
		return &toolspermission.ApprovalResponse{Choice: "deny"}, nil
	})
	if err := state.SetEmployeePermissions([]EmployeePermission{{
		RoleName: "auditor", Policy: "readwrite",
		Groups: map[string]uint8{GroupRO: GroupBitRead, GroupRW: 0, GroupCTL: 0, GroupADM: 0},
	}}); err != nil {
		t.Fatalf("SetEmployeePermissions: %v", err)
	}

	ctx := employeeCtx("goal-a2a-auditor", "auditor", "readwrite")
	err, ran := callToolArgs(t, state, ctx, "write_file", `{}`)
	if ran != 0 {
		t.Fatalf("分配只给读位，写调用不该执行（ran=%d）", ran)
	}
	if err == nil {
		t.Fatal("位缺（违权）必须被拦下")
	}
	if approvals == 0 {
		t.Fatal("员工位缺应走执行选择页面提权（人类 sudo 口令）")
	}

	// 读调用：位齐 → 放行（分配没有把整条员工链路变成拒绝）。
	if err, ran := callToolArgs(t, state, ctx, "read_file", `{}`); err != nil || ran != 1 {
		t.Fatalf("读调用应放行：err=%v ran=%d", err, ran)
	}
}

// TestEmployeeWithoutAssignmentFallsBackToTier：没被单独分配过的员工按档位默认
// （不回归）——否则"没分配"会变成"没有任何能力"。
func TestEmployeeWithoutAssignmentFallsBackToTier(t *testing.T) {
	state := &PermissionGate{SessionFromContext: toolspermission.SessionIDFromContext}
	state.Set(DefaultPermissionConfig(), nil)

	readonly := employeeCtx("goal-a2a-x", "reviewer", "readonly")
	if !state.ToolFaceForContext(readonly, "read_file") {
		t.Fatal("readonly 员工的只读工具应在面上")
	}
	if state.ToolFaceForContext(readonly, "write_file") {
		t.Fatal("readonly 员工的写工具不该在面上")
	}
	readwrite := employeeCtx("goal-a2a-y", "worker", "readwrite")
	if !state.ToolFaceForContext(readwrite, "write_file") {
		t.Fatal("readwrite 员工的写工具应在面上（按档位默认派生主体条目）")
	}
}

// TestSetEmployeePermissionsValidatesInput：装配路径宁可显式失败，也不要静默写一条
// 无意义的授权（那会变成"看起来分配了、其实按宿主默认判"）。
func TestSetEmployeePermissionsValidatesInput(t *testing.T) {
	state := &PermissionGate{}
	state.Set(DefaultPermissionConfig(), nil)
	cases := []struct {
		name       string
		permission EmployeePermission
	}{
		{name: "缺角色名", permission: EmployeePermission{Policy: "readonly"}},
		{name: "继承宿主默认口径不可当员工档分配", permission: EmployeePermission{RoleName: "pm", Policy: "full"}},
		{name: "空口径且无分组位", permission: EmployeePermission{RoleName: "pm"}},
		{name: "未识别口径", permission: EmployeePermission{RoleName: "pm", Policy: "sudo"}},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if err := state.SetEmployeePermissions([]EmployeePermission{item.permission}); err == nil {
				t.Fatalf("应报错：%+v", item.permission)
			}
		})
	}
	// 校验失败不留残留。
	if len(state.EmployeePermissions()) != 0 {
		t.Fatalf("校验失败不该留下分配记录：%+v", state.EmployeePermissions())
	}
}

// TestAssignedEmployeePermissionsSurviveConfigRefresh：装配是运行时行为，配置可能在
// 装配之后才被 Set（CLI/GUI 切权限档）。分配过的员工权限必须还在——否则一次配置刷新
// 就静默吃掉分配结果。
func TestAssignedEmployeePermissionsSurviveConfigRefresh(t *testing.T) {
	state := &PermissionGate{SessionFromContext: toolspermission.SessionIDFromContext}
	state.Set(DefaultPermissionConfig(), nil)
	if err := state.SetEmployeePermissions([]EmployeePermission{{
		RoleName: "runner", Policy: "readwrite",
		Groups: map[string]uint8{GroupRO: GroupBitRead, GroupRW: GroupBitRead | GroupBitWrite,
			GroupRWDesktop: GroupBitRead | GroupBitWrite},
	}}); err != nil {
		t.Fatalf("SetEmployeePermissions: %v", err)
	}
	state.Set(DefaultPermissionConfig(), nil) // 配置刷新

	ctx := employeeCtx("goal-a2a-runner", "runner", "readwrite")
	if !state.ToolFaceForContext(ctx, "computer_click") {
		t.Fatal("配置刷新后，装配期分配的共享外设位应仍然生效")
	}
}

// TestInheritHostDefaultEmployeeKeepsRootJudgement：空口径（继承宿主默认）的员工不写
// 主体条目，判定完全落回宿主口径（root）——"继承"必须真的是继承。
func TestInheritHostDefaultEmployeeKeepsRootJudgement(t *testing.T) {
	state := &PermissionGate{SessionFromContext: toolspermission.SessionIDFromContext}
	state.Set(DefaultPermissionConfig(), nil)

	ctx := toolspermission.WithSessionID(context.Background(), "goal-a2a-owner")
	if class := state.classFor(WithEmployeeSubject(ctx, "owner", "")); class != SubjectClassRoot {
		t.Fatalf("空口径员工主体类 = %q, want root（继承宿主默认）", class)
	}
	if _, decided := state.Enforce(ctx, toolspermission.Subject(SubjectForClass(SubjectClassRoot)),
		frameworktools.ToolMeta{}, "write_file", "{}"); decided {
		t.Fatal("继承宿主默认的员工不该由员工口径接管")
	}
}
