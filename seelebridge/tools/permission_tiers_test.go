package tools

import (
	"context"
	"errors"
	"testing"
	"time"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// permission_tiers_test.go — 主会话权限档位（tier）的判定矩阵、主体边界与会话隔离。
//
// 三件事必须同时成立（任一被破坏都是产品口径破口）：
//  1. 逐档位对 root 的判定与设计表一致（edit 只放写文件、auto 连命令一起放、
//     full 短路；危险 deny 在任何档位下都硬拦）；
//  2. **主体边界**：档位只改 root 的"问不问"，不改员工/子代理的"有没有位"
//     （emp_rw 在 auto 下写文件仍走审批提权——需求 3 的落点）；
//  3. **会话隔离**：A 切 full 不替 B 放行，B 的起点同步不关掉 A。

// tierMatrixConfig 是判定矩阵用的权责配置（默认分组 + 主体 + 规则；不叠加
// seele.yaml，避免外部配置漂移影响断言）。
func tierMatrixConfig() toolspermission.PermissionConfig {
	return DefaultPermissionConfig()
}

// TestTierDecisionMatrix 逐档位钉住 root 的代表性判定（checker 级）。
func TestTierDecisionMatrix(t *testing.T) {
	cfg := tierMatrixConfig()
	root := toolspermission.SubjectRoot
	cases := []struct {
		tier    string
		bash    bool
		target  string
		want    toolspermission.CheckResult
		comment string
	}{
		// manual：与 base 完全一致（写文件/命令/能力面都问人，危险命令拒绝）。
		{dto.PermissionTierManual, false, "write_file", toolspermission.ResultAsk, "manual 写文件问人"},
		{dto.PermissionTierManual, false, "computer_click", toolspermission.ResultAsk, "manual 桌面问人"},
		{dto.PermissionTierManual, true, "python x.py", toolspermission.ResultAsk, "manual 命令问人"},
		{dto.PermissionTierManual, true, "rm -rf /", toolspermission.ResultDeny, "manual 危险命令拒绝"},

		// edit：只放写文件；命令/桌面/能力面仍问人。
		{dto.PermissionTierEdit, false, "write_file", toolspermission.ResultAllow, "edit 写文件放行"},
		{dto.PermissionTierEdit, false, "edit_file", toolspermission.ResultAllow, "edit 编辑放行"},
		{dto.PermissionTierEdit, false, "plugin_create", toolspermission.ResultAsk, "edit 扩能力仍问人"},
		{dto.PermissionTierEdit, true, "python x.py", toolspermission.ResultAsk, "edit 命令仍问人"},
		{dto.PermissionTierEdit, false, "computer_click", toolspermission.ResultAsk, "edit 桌面仍问人"},
		{dto.PermissionTierEdit, true, "rm -rf /", toolspermission.ResultDeny, "edit 危险命令仍拒绝"},

		// auto：文件写 + 任意命令直跑；危险命令仍拒绝；桌面/能力面仍问人。
		{dto.PermissionTierAuto, false, "write_file", toolspermission.ResultAllow, "auto 写文件放行"},
		{dto.PermissionTierAuto, true, "python x.py", toolspermission.ResultAllow, "auto 任意命令放行"},
		{dto.PermissionTierAuto, true, "make build", toolspermission.ResultAllow, "auto 危险段命令放行"},
		{dto.PermissionTierAuto, true, "rm -rf /", toolspermission.ResultDeny, "auto 危险命令仍拒绝"},
		{dto.PermissionTierAuto, true, "dd if=/dev/zero of=/dev/sda", toolspermission.ResultDeny, "auto dd 仍拒绝"},
		{dto.PermissionTierAuto, false, "computer_click", toolspermission.ResultAsk, "auto 桌面仍问人"},
		{dto.PermissionTierAuto, false, "mcp_load", toolspermission.ResultAsk, "auto 能力面仍问人"},
	}
	for _, test := range cases {
		checker := toolspermission.NewPermissionChecker(ApplyTier(cfg, test.tier))
		name, args := test.target, `{}`
		if test.bash {
			name, args = "bash", test.target
		}
		result, visible := checker.DecideForMeta(root, name, frameworktools.ToolMeta{}, args)
		if !visible {
			t.Fatalf("[%s] %s: root 全位不该不可见", test.tier, test.comment)
		}
		if result != test.want {
			t.Fatalf("[%s] %s: 判定 = %v, want %v", test.tier, test.comment, result, test.want)
		}
	}
}

// TestApplyTierKeepsManualAndFullUnchanged 钉住覆盖的两条不变量：
//   - manual 与 full 返回原配置（full 由 Enforce 短路，规则面保持安全兜底）；
//   - 未识别档位按 manual 处理（防御；写入侧 NormalizePermissionTier 已挡）。
func TestApplyTierKeepsManualAndFullUnchanged(t *testing.T) {
	cfg := tierMatrixConfig()
	for _, tier := range []string{dto.PermissionTierManual, dto.PermissionTierFull, "", "unknown-tier"} {
		if got := ApplyTier(cfg, tier); len(got.Rules) != len(cfg.Rules) {
			t.Fatalf("ApplyTier(%q) 改动了规则数量：%d → %d", tier, len(cfg.Rules), len(got.Rules))
		}
	}
}

// TestTierCatalogMirrorsDTO 钉住档位目录（id 序 + 标签齐全），前端按它渲染列表。
func TestTierCatalogMirrorsDTO(t *testing.T) {
	tiers := DefaultPermissionTiers()
	want := []string{dto.PermissionTierManual, dto.PermissionTierEdit, dto.PermissionTierAuto, dto.PermissionTierFull}
	if len(tiers) != len(want) {
		t.Fatalf("档位数量 = %d, want %d", len(tiers), len(want))
	}
	for index, info := range tiers {
		if info.ID != want[index] {
			t.Fatalf("档位[%d] = %q, want %q", index, info.ID, want[index])
		}
		if info.Label == "" || info.Short == "" || info.Description == "" {
			t.Fatalf("档位 %q 的展示字段不完整：%+v", info.ID, info)
		}
	}
}

// TestSetPermissionTierRejectsUnknown 钉住写入侧校验：非法档位报错且不改变当前档。
func TestSetPermissionTierRejectsUnknown(t *testing.T) {
	state := &PermissionGate{}
	if err := state.SetPermissionTierFor("s1", "superuser"); err == nil {
		t.Fatal("未知档位应当报错")
	}
	if got := state.PermissionTierFor("s1"); got != dto.PermissionTierManual {
		t.Fatalf("非法档位写入后当前档 = %q, want manual（不被改变）", got)
	}
	if err := state.SetPermissionTierFor("s1", dto.PermissionTierAuto); err != nil {
		t.Fatalf("合法档位写入失败: %v", err)
	}
	if got := state.PermissionTierFor("s1"); got != dto.PermissionTierAuto {
		t.Fatalf("写入 auto 后档位 = %q", got)
	}
}

// tierProbe 构造带会话归属 + 员工角色识别的探针门（默认权责配置）。
func tierProbe(approvals *int, ran *int) *PermissionGate {
	state := &PermissionGate{SessionFromContext: func(ctx context.Context) string {
		sessionID, _ := ctx.Value(probeSessionKey{}).(string)
		return sessionID
	}}
	handler := func(ctx *toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
		if approvals != nil {
			*approvals++
		}
		return &toolspermission.ApprovalResponse{RequestID: ctx.Request.ID, Choice: "allow"}, nil
	}
	state.Set(DefaultPermissionConfig(), handler)
	state.SetRoleSessionPolicyResolver(func(sessionID string) (string, bool) {
		if sessionID == "role-1" {
			return "readwrite", true
		}
		return "", false
	})
	return state
}

// runTierTool 执行一次工具调用，返回错误并统计真正执行次数。
func runTierTool(state *PermissionGate, ctx context.Context, name, args string, ran *int) error {
	_, err := state.Middleware(time.Minute)(name, frameworktools.ToolMeta{},
		frameworktools.HandlerFunc(func(context.Context, string) (string, error) {
			*ran++
			return "ok", nil
		})).Execute(ctx, args)
	return err
}

// TestTierRootAutoRunsWithoutApproval 钉住 auto 档对 root 的效果：命令直跑、
// 不产生审批请求；写文件也不打断。
func TestTierRootAutoRunsWithoutApproval(t *testing.T) {
	approvals, ran := new(int), new(int)
	state := tierProbe(approvals, ran)
	if err := state.SetPermissionTierFor("main-1", dto.PermissionTierAuto); err != nil {
		t.Fatal(err)
	}
	ctx := probeCtx("main-1")
	if err := runTierTool(state, ctx, "bash", `{"command":"python x.py"}`, ran); err != nil {
		t.Fatalf("auto 档命令应直跑: %v", err)
	}
	if err := runTierTool(state, ctx, "write_file", `{"path":"a.txt"}`, ran); err != nil {
		t.Fatalf("auto 档写文件应直跑: %v", err)
	}
	if err := runTierTool(state, ctx, "bash", `{"command":"rm -rf /"}`, ran); err == nil {
		t.Fatal("auto 档危险命令必须仍被拒绝")
	}
	if *approvals != 0 {
		t.Fatalf("auto 档不该产生审批请求，approvals=%d", *approvals)
	}
	if *ran != 2 {
		t.Fatalf("真正执行数 = %d, want 2", *ran)
	}
}

// TestTierDoesNotBypassEmployeeBoundary 钉住**主体边界**（需求 3 的核心）：
// 即便 root 档位是 full，员工的越权写文件仍走审批提权——档位只改 root 的
// "问不问"，不改员工的"有没有位"，也绝不允许 full 档连带放行员工越权。
func TestTierDoesNotBypassEmployeeBoundary(t *testing.T) {
	approvals, ran := new(int), new(int)
	state := tierProbe(approvals, ran)
	// root 会话 + 员工会话各自切到 full 档（旧实现下 full 会连带放行员工）。
	if err := state.SetPermissionTierFor("main-1", dto.PermissionTierFull); err != nil {
		t.Fatal(err)
	}
	if err := state.SetPermissionTierFor("role-1", dto.PermissionTierFull); err != nil {
		t.Fatal(err)
	}
	// 员工（readwrite）会话调用写工具：必须走执行选择页面提权，放行后才执行。
	empCtx := probeCtx("role-1")
	if err := runTierTool(state, empCtx, "write_file", `{"path":"a.txt"}`, ran); err != nil {
		t.Fatalf("员工越权（写文件）应当走审批页并可按放行执行: %v", err)
	}
	if *approvals != 1 {
		t.Fatalf("员工越权必须落实到审批页，approvals=%d, want 1（full 档不得连带放行）", *approvals)
	}
	// 员工碰 adm（能力面）：同样走审批页，不静默放行。
	if err := runTierTool(state, empCtx, "switch_plugin", `{}`, ran); err != nil {
		t.Fatalf("员工碰能力面应当走审批页: %v", err)
	}
	if *approvals != 2 {
		t.Fatalf("员工碰能力面的审批数 = %d, want 2", *approvals)
	}
}

// TestTierDoesNotBypassSubagentBoundary 钉住子代理边界：档位不改变子代理的
// ctl/adm 断位——违权直接拒绝（不可路由），不弹页面。
func TestTierDoesNotBypassSubagentBoundary(t *testing.T) {
	approvals, ran := new(int), new(int)
	state := tierProbe(approvals, ran)
	if err := state.SetPermissionTierFor("main-1", dto.PermissionTierFull); err != nil {
		t.Fatal(err)
	}
	subCtx := withSubjectClass(context.Background(), SubjectClassSub)
	_, err := state.Middleware(time.Minute)("task_complete", frameworktools.ToolMeta{},
		frameworktools.HandlerFunc(func(context.Context, string) (string, error) {
			*ran++
			return "ok", nil
		})).Execute(subCtx, `{}`)
	if !errors.Is(err, frameworktools.ErrToolNotVisible) {
		t.Fatalf("子代理违权应直接拒绝（ErrToolNotVisible），err = %v", err)
	}
	if *approvals != 0 || *ran != 0 {
		t.Fatalf("子代理违权不该弹页面/执行：approvals=%d ran=%d", *approvals, *ran)
	}
}

// TestTierSessionIsolation 钉住会话隔离：A 切 full 不替 B 放行；显式 deny 规则
// 在 B 上仍然生效。
func TestTierSessionIsolation(t *testing.T) {
	state := &PermissionGate{SessionFromContext: func(ctx context.Context) string {
		sessionID, _ := ctx.Value(probeSessionKey{}).(string)
		return sessionID
	}}
	state.Set(toolspermission.PermissionConfig{
		Mode:  toolspermission.ModeManual,
		Rules: []toolspermission.PermissionRule{{ToolName: "bash", Action: toolspermission.ActionDeny}},
	}, nil)
	handler := state.Middleware(time.Minute)("bash", frameworktools.ToolMeta{},
		frameworktools.HandlerFunc(func(context.Context, string) (string, error) { return "ok", nil }))

	if err := state.SetPermissionTierFor("session-a", dto.PermissionTierFull); err != nil {
		t.Fatal(err)
	}
	if _, err := handler.Execute(probeCtx("session-a"), "{}"); err != nil {
		t.Fatalf("A 切 full 后自己的调用应放行: %v", err)
	}
	if _, err := handler.Execute(probeCtx("session-b"), "{}"); err == nil {
		t.Fatal("污染：A 的 full 档放行了 B 会话（B 应被 deny 规则拦下）")
	}
	if _, err := handler.Execute(probeCtx(""), "{}"); err == nil {
		t.Fatal("污染：无会话归属的调用继承了某会话的 full 档（应回退进程默认 manual）")
	}
	if got := state.PermissionTierFor("session-a"); got != dto.PermissionTierFull {
		t.Fatalf("PermissionTierFor(A) = %q, want full", got)
	}
	if got := state.PermissionTierFor("session-b"); got != dto.PermissionTierManual {
		t.Fatalf("PermissionTierFor(B) = %q, want manual", got)
	}
}

// TestTierSurvivesConfigReinstall 钉住 Set 重建 checker 不丢会话档位
// （档位是会话选择，不是 checker 的内容）。
func TestTierSurvivesConfigReinstall(t *testing.T) {
	state := &PermissionGate{SessionFromContext: func(ctx context.Context) string {
		sessionID, _ := ctx.Value(probeSessionKey{}).(string)
		return sessionID
	}}
	state.Set(toolspermission.PermissionConfig{
		Mode:  toolspermission.ModeManual,
		Rules: []toolspermission.PermissionRule{{ToolName: "bash", Action: toolspermission.ActionDeny}},
	}, nil)
	if err := state.SetPermissionTierFor("session-a", dto.PermissionTierFull); err != nil {
		t.Fatal(err)
	}
	state.Set(toolspermission.PermissionConfig{
		Mode:  toolspermission.ModeManual,
		Rules: []toolspermission.PermissionRule{{ToolName: "bash", Action: toolspermission.ActionDeny}},
	}, nil)
	if got := state.PermissionTierFor("session-a"); got != dto.PermissionTierFull {
		t.Fatalf("Set 之后会话档位 = %q, want full（不得被配置刷新吃掉）", got)
	}
}
