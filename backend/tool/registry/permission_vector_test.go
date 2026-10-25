package registry

// permission_vector_test.go — 「权限向量接管判定」的验收面（2026-10-21）。
//
// 本轮把判定从「累加阶梯 + 共享 Subjects 表」改成「权限向量接管」，并收掉双判据：
//
//	旧的双判据（两条写共享表的路径）
//	  1. 装配期分配：SetEmployeePermissions 把 emp_<角色> 的派生授权写进 cfg.Subjects
//     并 rebuildLocked；Set(cfg) 再把 employeePermissions **重放**一遍写回新表
//	    （"配置刷新不能吃掉分配"）。
//	  2. 运行时补默认：ensureEmployeeSubject 在每次工具调用里发现主体缺条目就
//	     **写一条派生默认**进 cfg.Subjects 并 rebuildLocked。
//	  两条路都往同一张可变 map 写，且都能在并行会话/角色回合里被别的 goroutine 看见
//	  （共享 map 写 + 谁先跑谁定案）；同一件事（员工有什么能力）于是有两个答案。
//
//	现在唯一一条方向：**显式分配优先、运行时派生只读**。分配事实存在
//	employeePermissions（只读参与判定），判定的输入是一次调用局部算出来的权限向量
//	（callVector → policy.CellsForVector），cfg.Subjects 只承载配置本身。
//
// 本文件钉住四件事：
//   - ① (sessionID, role) 的向量被判据采用（登记优先、缺失走四级链路 → 这里是注入
//     的读面，语义等价）；
//   - ① 向量里没有的组 = 断位；`!tag` = 显式不给（不给提权以外的路）；
//   - ④ 两个不同角色**并发**调用各自生效、互不污染（禁止共享 map 写）；
//   - ④ 未登记角色的兜底与显式分配结论一致。

import (
	"context"
	"sync"
	"testing"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/seelex/backend/limit/policy"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// vectorGate 造一个装配了默认权责配置、并注入"读面"的探针门。
func vectorGate(t *testing.T, resolver func(sessionID, role string) ([]string, bool, error)) *PermissionGate {
	t.Helper()
	state := &PermissionGate{SessionFromContext: toolspermission.SessionIDFromContext}
	state.Set(policy.DefaultPermissionConfig(), nil)
	if resolver != nil {
		state.SetRolePermissionVectorResolver(resolver)
	}
	return state
}

// TestPermissionVectorTakesOverJudgement：① 判定前解析 (会话, 角色) 的向量，
// 向量授予的组直接放行、没授予的组断位（员工 → 提权页），与共享主体表无关。
func TestPermissionVectorTakesOverJudgement(t *testing.T) {
	// 读面：只给 auditor 这**一格**（rw），别的角色什么都不给（显式 `!`）。
	resolver := func(sessionID, role string) ([]string, bool, error) {
		switch role {
		case "auditor":
			return []string{dto.PermissionTagRW}, true, nil
		case "clerk":
			return []string{"!" + dto.PermissionTagRW, dto.PermissionTagRO}, true, nil
		}
		return nil, false, nil
	}
	state := vectorGate(t, resolver)

	auditor := WithEmployeeSubject(
		toolspermission.WithSessionID(context.Background(), "role-session-a"), "auditor", "readonly")
	clerk := WithEmployeeSubject(
		toolspermission.WithSessionID(context.Background(), "role-session-c"), "clerk", "readwrite")

	// auditor 的向量说"给 rw" → 写调用直接放行（即使 ToolsPolicy 是 readonly：
	// 授权的唯一来源是向量，档位只用来选分支）。
	if _, decided := state.Enforce(auditor, toolspermission.Subject("emp_auditor"),
		frameworktools.ToolMeta{}, "write_file", "{}"); !decided {
		t.Fatal("员工判定必须由向量接管（ok=false 会落回共享主体表）")
	} else if action, _ := state.Enforce(auditor, toolspermission.Subject("emp_auditor"),
		frameworktools.ToolMeta{}, "write_file", "{}"); action != toolspermission.ActionAllow {
		t.Fatalf("向量授予 rw 时写调用应放行，得到 %v", action)
	}

	// clerk 的向量显式 `!rw` → 不给规则层兜底的机会，直接走人类口令。
	if action, _ := state.Enforce(clerk, toolspermission.Subject("emp_clerk"),
		frameworktools.ToolMeta{}, "write_file", "{}"); action != toolspermission.ActionAsk {
		t.Fatalf("`!rw` 的写调用应落成提权，得到 %v", action)
	}
	// clerk 的 ro 仍在向量里 → 读免审。
	if action, _ := state.Enforce(clerk, toolspermission.Subject("emp_clerk"),
		frameworktools.ToolMeta{}, "read_file", "{}"); action != toolspermission.ActionAllow {
		t.Fatalf("向量授予 ro 时读调用应放行，得到 %v", action)
	}
}

func TestEmployeePermissionVectorUsesAccountRoleNotMemberName(t *testing.T) {
	resolvedRole := ""
	state := vectorGate(t, func(_ string, role string) ([]string, bool, error) {
		resolvedRole = role
		if role == string(dto.RoleSubAgent) {
			return []string{dto.PermissionTagRO, dto.PermissionGroupSSH}, true, nil
		}
		return nil, false, nil
	})
	ctx := WithEmployeeSubject(
		toolspermission.WithSessionID(context.Background(), "role-session-sub"),
		"impl_ui",
		"readonly",
	)
	ctx = WithEmployeeAccountRole(ctx, string(dto.RoleSubAgent))

	cells, _, ok := state.callVector(ctx)
	if !ok {
		t.Fatal("account-role 权限向量应接管员工判定")
	}
	if resolvedRole != string(dto.RoleSubAgent) {
		t.Fatalf("权限向量按成员名解析成 %q，want subagent", resolvedRole)
	}
	if !cells.Covers(policy.GroupSSH, policy.BitExecute) {
		t.Fatalf("subagent 的 ssh 权限未生效: %+v", cells)
	}
}

func TestMainAgentRolePermissionAllowSkipsApproval(t *testing.T) {
	state := vectorGate(t, func(_ string, role string) ([]string, bool, error) {
		if role == string(dto.RoleAgent) {
			return []string{dto.PermissionTagRW}, true, nil
		}
		return nil, false, nil
	})
	ctx := toolspermission.WithSessionID(context.Background(), "sess-main-agent")
	action, decided := state.Enforce(
		ctx,
		toolspermission.Subject("root"),
		frameworktools.ToolMeta{},
		"write_file",
		"{}",
	)
	if !decided || action != toolspermission.ActionAllow {
		t.Fatalf("mainagent 选中 rw 后应直接 allow，得到 action=%v decided=%v", action, decided)
	}
}

// TestMainAgentBashPermissionAllowSkipsApproval 钉住用户实际勾选 Bash 后的产品
// 语义：命令执行是赋权即免审批，不再落回 bash 规则层的 ask。
func TestMainAgentBashPermissionAllowSkipsApproval(t *testing.T) {
	state := vectorGate(t, func(_ string, role string) ([]string, bool, error) {
		if role == string(dto.RoleAgent) {
			return []string{dto.PermissionTagBash}, true, nil
		}
		return nil, false, nil
	})
	ctx := toolspermission.WithSessionID(context.Background(), "sess-main-bash")
	for _, tool := range []string{"bash", "bash_bg", "bg_manage"} {
		action, decided := state.Enforce(
			ctx,
			toolspermission.Subject("root"),
			frameworktools.ToolMeta{},
			tool,
			`{"command":"npm install"}`,
		)
		if !decided || action != toolspermission.ActionAllow {
			t.Fatalf("mainagent 选中 bash 后 %s 应直接 allow，得到 action=%v decided=%v", tool, action, decided)
		}
	}
}

// TestMainAgentWriteDoesNotGrantBashApproval 钉住两枚权限已经真正分开：只有
// write 时文件写入直通，但 shell 执行仍需 Bash 权限或审批。
func TestMainAgentWriteDoesNotGrantBashApproval(t *testing.T) {
	state := vectorGate(t, func(_ string, role string) ([]string, bool, error) {
		if role == string(dto.RoleAgent) {
			return []string{dto.PermissionTagRW}, true, nil
		}
		return nil, false, nil
	})
	ctx := toolspermission.WithSessionID(context.Background(), "sess-main-write")
	action, decided := state.Enforce(
		ctx,
		toolspermission.Subject("root"),
		frameworktools.ToolMeta{},
		"bash",
		`{"command":"npm install"}`,
	)
	if decided {
		t.Fatalf("只授予 write 时 bash 不应由权限向量直通，得到 action=%v", action)
	}
	result, handled := state.decideFor(
		toolspermission.Subject("root"),
		"bash",
		frameworktools.ToolMeta{},
		`{"command":"npm install"}`,
	)
	if !handled || result != toolspermission.ResultAsk {
		t.Fatalf("只授予 write 时 bash 应回落到规则层 ask，得到 result=%v handled=%v", result, handled)
	}
}

// TestLegacyAdminPermissionMigratesToBash 钉住旧配置兼容：历史角色向量里的
// admin 读取后等同于 bash，不能让已有用户的命令权限在升级后失效。
func TestLegacyAdminPermissionMigratesToBash(t *testing.T) {
	state := vectorGate(t, func(_ string, role string) ([]string, bool, error) {
		if role == string(dto.RoleAgent) {
			return []string{"admin"}, true, nil
		}
		return nil, false, nil
	})
	action, decided := state.Enforce(
		toolspermission.WithSessionID(context.Background(), "sess-legacy-admin"),
		toolspermission.Subject("root"),
		frameworktools.ToolMeta{},
		"bash",
		`{"command":"npm install"}`,
	)
	if !decided || action != toolspermission.ActionAllow {
		t.Fatalf("旧 admin 向量应迁移为 bash 并直接 allow，得到 action=%v decided=%v", action, decided)
	}
}

func TestMainAgentFullAccessBypassesBashApprovalEvenWithNarrowRoleVector(t *testing.T) {
	state := vectorGate(t, func(_ string, role string) ([]string, bool, error) {
		if role == string(dto.RoleAgent) {
			return []string{dto.PermissionTagRO}, true, nil
		}
		return nil, false, nil
	})
	const sessionID = "sess-full-bash"
	state.SetFullAccessFor(sessionID, true)
	action, decided := state.Enforce(
		toolspermission.WithSessionID(context.Background(), sessionID),
		toolspermission.Subject("root"),
		frameworktools.ToolMeta{},
		"bash",
		`{"command":"npm install"}`,
	)
	if !decided || action != toolspermission.ActionAllow {
		t.Fatalf("全权下 bash 必须直接 allow，得到 action=%v decided=%v", action, decided)
	}
}

// TestPermissionVectorClaimsDoNotTouchSharedTable：② 判定不再写共享主体表——
// 员工调用过之后，配置里的 Subjects 与调用前逐字节相同（没有派生条目、没有 rebuild）。
func TestPermissionVectorClaimsDoNotTouchSharedTable(t *testing.T) {
	state := vectorGate(t, func(sessionID, role string) ([]string, bool, error) {
		return []string{dto.PermissionTagRO, dto.PermissionTagRW}, true, nil
	})
	before := len(state.configSnapshot().Subjects)

	ctx := WithEmployeeSubject(
		toolspermission.WithSessionID(context.Background(), "role-session-w"), "worker", "readwrite")
	for _, name := range []string{"read_file", "write_file"} {
		state.Enforce(ctx, toolspermission.Subject("emp_worker"), frameworktools.ToolMeta{}, name, "{}")
		state.ToolFaceForContext(ctx, name)
	}

	if after := len(state.configSnapshot().Subjects); after != before {
		t.Fatalf("共享主体表被写入：%d → %d（双判据的第二条路又回来了）", before, after)
	}
	if _, exists := state.configSnapshot().Subjects["emp_worker"]; exists {
		t.Fatal("运行时不该为员工造主体条目（显式分配优先、派生只读）")
	}
}

// TestPermissionVectorConcurrentRolesDoNotPollute：④ 两个不同角色并发调用，
// 各自向量生效、互不污染（共享 map 写会在这种时序下互相踩）。
func TestPermissionVectorConcurrentRolesDoNotPollute(t *testing.T) {
	resolver := func(sessionID, role string) ([]string, bool, error) {
		if role == "writer" {
			return []string{dto.PermissionTagRO, dto.PermissionTagRW}, true, nil
		}
		return []string{dto.PermissionTagRO}, true, nil
	}
	state := vectorGate(t, resolver)

	writer := WithEmployeeSubject(
		toolspermission.WithSessionID(context.Background(), "role-session-w1"), "writer", "readwrite")
	reader := WithEmployeeSubject(
		toolspermission.WithSessionID(context.Background(), "role-session-r1"), "reader", "readonly")

	const rounds = 64
	var wait sync.WaitGroup
	failures := make(chan string, 2*rounds)
	check := func(ctx context.Context, subject toolspermission.Subject, name string, want toolspermission.Action, label string) {
		defer wait.Done()
		for index := 0; index < rounds; index++ {
			action, decided := state.Enforce(ctx, subject, frameworktools.ToolMeta{}, name, "{}")
			if !decided || action != want {
				failures <- label
				return
			}
			// 工具面读的是同一份向量，必须给出同一个结论。
			if face := state.ToolFaceForContext(ctx, name); face != (want == toolspermission.ActionAllow) {
				failures <- label + "（工具面与判定不一致）"
				return
			}
		}
	}
	wait.Add(4)
	go check(writer, toolspermission.Subject("emp_writer"), "write_file", toolspermission.ActionAllow, "writer 写")
	go check(reader, toolspermission.Subject("emp_reader"), "write_file", toolspermission.ActionAsk, "reader 写")
	go check(reader, toolspermission.Subject("emp_reader"), "read_file", toolspermission.ActionAllow, "reader 读")
	go check(writer, toolspermission.Subject("emp_writer"), "read_file", toolspermission.ActionAllow, "writer 读")
	wait.Wait()
	close(failures)
	for failure := range failures {
		t.Fatalf("并发判定被污染：%s（两个角色的向量互相踩 = 共享可变状态还在）", failure)
	}
}

// TestUnregisteredRoleFallbackMatchesExplicitAssignment：④ 读面没定过（未登记、
// 链路也没有一层表态）时，兜底结论与"显式分配同档位"一致。
func TestUnregisteredRoleFallbackMatchesExplicitAssignment(t *testing.T) {
	// 未接读面：只能按 ctx 身份派生。
	fallback := vectorGate(t, func(sessionID, role string) ([]string, bool, error) {
		return nil, false, nil // decided=false = 没定过
	})
	// 显式分配同档位（readonly）。
	assigned := vectorGate(t, nil)
	if err := assigned.SetEmployeePermissions([]EmployeePermission{{RoleName: "clerk", Policy: "readonly"}}); err != nil {
		t.Fatalf("SetEmployeePermissions: %v", err)
	}

	fallbackCtx := WithEmployeeSubject(
		toolspermission.WithSessionID(context.Background(), "role-session-u"), "clerk", "readonly")
	assignedCtx := WithEmployeeSubject(
		toolspermission.WithSessionID(context.Background(), "role-session-u"), "clerk", "readonly")

	for _, name := range []string{"read_file", "write_file", "bash"} {
		left, leftOK := fallback.Enforce(fallbackCtx, toolspermission.Subject("emp_clerk"), frameworktools.ToolMeta{}, name, "{}")
		right, rightOK := assigned.Enforce(assignedCtx, toolspermission.Subject("emp_clerk"), frameworktools.ToolMeta{}, name, "{}")
		if leftOK != rightOK || left != right {
			t.Fatalf("%s：未登记兜底 (%v,%v) 与显式分配 (%v,%v) 结论不一致", name, left, leftOK, right, rightOK)
		}
	}
}

// TestEmployeeReadwriteFallbackIncludesBash 钉住没有角色向量时的 readwrite
// 兼容口径：拆分 bash 组后，历史 readwrite 员工仍保留 README 承诺的写+执行面。
func TestEmployeeReadwriteFallbackIncludesBash(t *testing.T) {
	state := vectorGate(t, func(string, string) ([]string, bool, error) {
		return nil, false, nil
	})
	ctx := WithEmployeeSubject(
		toolspermission.WithSessionID(context.Background(), "role-session-rw"),
		"worker", "readwrite",
	)
	cells, _, ok := state.callVector(ctx)
	if !ok {
		t.Fatal("readwrite 员工应走主体类兜底向量")
	}
	if !cells.Covers(policy.GroupBash, policy.BitExecute) {
		t.Fatalf("readwrite 员工兜底向量缺少 bash 执行位: %+v", cells)
	}
}

// TestTierPresetIsVectorNotRulePruning：③ 档位是向量预设——执行面按 TierVector
// 展开的组位判定，规则表里没有任何"按档位剪过的副本"。
func TestTierPresetIsVectorNotRulePruning(t *testing.T) {
	cfg := policy.DefaultPermissionConfig()
	for _, tier := range dto.PermissionTierIDs() {
		cells := policy.TierCells(tier, cfg.Groups)
		for _, group := range policy.TierVector(tier) {
			if !cells.Covers(group, 0) {
				t.Fatalf("档位 %q 的向量含 %q，但展开后没有位（预设与格位表分叉）", tier, group)
			}
		}
	}
	// full 的向量覆盖全部公开权限标签；内部 adm 路由不再暴露为用户权限。
	if got := len(policy.TierVector(dto.PermissionTierFull)); got != len(dto.PermissionGroupNames()) {
		t.Fatalf("full 档向量应覆盖全部 %d 个公开组，得到 %d", len(dto.PermissionGroupNames()), got)
	}
	for _, tier := range []string{dto.PermissionTierManual, dto.PermissionTierEdit, dto.PermissionTierAuto} {
		for _, tag := range policy.TierVector(tier) {
			if tag == policy.GroupRWDesktop || tag == policy.GroupADM {
				t.Fatalf("档位 %q 不该在向量里含 %q（桌面/能力面只有 full）", tier, tag)
			}
		}
	}
}
