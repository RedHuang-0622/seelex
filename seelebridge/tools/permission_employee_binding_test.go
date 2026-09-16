package tools

// permission_employee_binding_test.go — 员工权限拦截的**承重面**：
//
// 拦截不能依赖"用户此刻看着哪个会话"，也不能依赖"角色会话归属索引已经热了"。
// 角色回合执行体在起手时按构造把主体类放进 ctx（WithEmployeeSubjectClass），
// 判定就必须按员工口径走：readonly 员工写文件 = 违权 → 顶一张执行选择页面
// （人类 sudo 口令），工具本身绝不执行。
//
// 对照组：同一个会话号、没有主体类（= 主代理/未登记会话的 root）时，主体类策略
// 不接管，判定交回框架的组/规则——即"不是所有调用都被员工口径收窄"。

import (
	"context"
	"testing"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
)

// TestEmployeeClassInContextInterceptsWrite：ctx 里的员工主体类是拦截的唯一依据，
// 解析器/索引一个都不用（按构造授权）。
func TestEmployeeClassInContextInterceptsWrite(t *testing.T) {
	approvals := 0
	state := &PermissionGate{SessionFromContext: toolspermission.SessionIDFromContext}
	state.Set(DefaultPermissionConfig(), func(*toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
		approvals++
		return &toolspermission.ApprovalResponse{Choice: "deny"}, nil
	})

	const roleSessionID = "goal-a2a-reviewer"
	ctx := WithEmployeeSubjectClass(
		toolspermission.WithSessionID(context.Background(), roleSessionID),
		"readonly",
	)
	if got := state.classFor(ctx); got != SubjectClassEmployeeRO {
		t.Fatalf("classFor = %q, want %q（ctx 里的员工主体类必须优先）", got, SubjectClassEmployeeRO)
	}

	err, ran := callToolArgs(t, state, ctx, "write_file", `{}`)
	if ran != 0 {
		t.Fatalf("readonly 员工的写调用不该执行（ran=%d）", ran)
	}
	if err == nil {
		t.Fatal("readonly 员工的写调用必须被拦下（顶执行选择页面/拒绝）")
	}
	if approvals == 0 {
		t.Fatal("员工违权应走执行选择页面提权（人类 sudo 口令），而不是静默放行或静默拒绝")
	}

	// 对照组：同一会话号、没有员工主体类 → 交回框架判定（主体类策略不接管）。
	rootCtx := toolspermission.WithSessionID(context.Background(), roleSessionID)
	if got := state.classFor(rootCtx); got != SubjectClassRoot {
		t.Fatalf("无主体类时应是 root，得到 %q", got)
	}
	if _, decided := state.Enforce(rootCtx, toolspermission.Subject(SubjectForClass(SubjectClassRoot)),
		frameworktools.ToolMeta{}, "write_file", "{}"); decided {
		t.Fatal("root 不该由员工口径接管（否则主代理的写调用会被无谓收窄）")
	}
}

// TestEmployeeClassInContextMatchesIndexPath：两条路（按构造注入 / 按会话号反查）
// 必须给出同一个结论——不一致就意味着"谁是调用者"有两个答案。
func TestEmployeeClassInContextMatchesIndexPath(t *testing.T) {
	state := employeeGate("goal-a2a-tl", "readwrite", nil)

	byIndex := toolspermission.WithSessionID(context.Background(), "goal-a2a-tl")
	byContext := WithEmployeeSubjectClass(byIndex, "readwrite")
	if left, right := state.classFor(byIndex), state.classFor(byContext); left != right {
		t.Fatalf("按反查 = %q，按构造注入 = %q：两条路必须一致", left, right)
	}
	if got := state.classFor(byIndex); got != SubjectClassEmployeeRW {
		t.Fatalf("readwrite 员工主体类 = %q, want %q", got, SubjectClassEmployeeRW)
	}
}
