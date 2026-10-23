package registry

import (
	"context"
	"errors"
	"github.com/RedHuang-0622/seelex/backend/limit/policy"
	"testing"
	"time"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// permission_tiers_test.go — 主会话权限档位（tier）的判定矩阵、主体边界与会话隔离。
//
// 三件事必须同时成立（任一被破坏都是产品口径破口）：
//  1. 逐档位对 root 的判定与设计表一致（edit 只放写文件、auto 连命令一起放、
//     full 短路；危险 deny 在任何档位下都硬拦）；
//  2. **主体边界**：档位只改 root 的"问不问"，不改员工/子代理的"有没有位"。
//     员工口径自 2026-10-03 起是**赋权即免审批**：位齐即直通（emp_rw 写文件不再弹页），
//     位缺/未分封才走执行选择页面提权；满档（full）不得连带放行员工的位外调用；
//  3. **会话隔离**：A 切 full 不替 B 放行，B 的起点同步不关掉 A。

// tierMatrixConfig 是判定矩阵用的权责配置（默认分组 + 主体 + 规则；不叠加
// seele.yaml，避免外部配置漂移影响断言）。
func tierMatrixConfig() toolspermission.PermissionConfig {
	return policy.DefaultPermissionConfig()
}

// TestTierDecisionMatrix 逐档位钉住 root 的代表性判定（执行门级）。
//
// 判据自 2026-10-21 起是**向量预设**（policy.TierVector → policy.TierCells）：
//
//	向量里有的组 → 该族免审批；没有的组 → 断位（执行选择页面提权）；
//	危险命令的 deny 规则在任何档位下都硬拦（含 full）。
func TestTierDecisionMatrix(t *testing.T) {
	cfg := tierMatrixConfig()
	root := toolspermission.Subject(policy.SubjectForClass(policy.SubjectClassRoot))
	cases := []struct {
		tier    string
		bash    bool
		target  string
		want    toolspermission.Action
		comment string
	}{
		// manual：只给 ro / rw_session / ctl。项目写、命令、桌面与能力面一律断位。
		{dto.PermissionTierManual, false, "write_file", toolspermission.ActionAsk, "manual 写文件提权"},
		{dto.PermissionTierManual, false, "computer_click", toolspermission.ActionAsk, "manual 桌面提权"},
		{dto.PermissionTierManual, true, "python x.py", toolspermission.ActionAsk, "manual 命令提权"},
		{dto.PermissionTierManual, false, "read_file", toolspermission.ActionAllow, "manual 只读免审"},
		{dto.PermissionTierManual, true, "rm -rf /", toolspermission.ActionDeny, "manual 危险命令拒绝"},

		// edit：再授予 rw（项目写 + 命令同属 rw 组）。
		{dto.PermissionTierEdit, false, "write_file", toolspermission.ActionAllow, "edit 写文件免审"},
		{dto.PermissionTierEdit, false, "edit_file", toolspermission.ActionAllow, "edit 编辑免审"},
		{dto.PermissionTierEdit, false, "computer_click", toolspermission.ActionAsk, "edit 桌面仍提权"},
		{dto.PermissionTierEdit, false, "mcp_load", toolspermission.ActionAsk, "edit 能力面仍提权"},
		{dto.PermissionTierEdit, true, "rm -rf /", toolspermission.ActionDeny, "edit 危险命令仍拒绝"},

		// auto：当前与 edit 同一份向量预设（命令与项目写同属 rw 组，见 D-tier 说明）。
		{dto.PermissionTierAuto, false, "write_file", toolspermission.ActionAllow, "auto 写文件免审"},
		{dto.PermissionTierAuto, true, "python x.py", toolspermission.ActionAllow, "auto 任意命令免审"},
		{dto.PermissionTierAuto, true, "rm -rf /", toolspermission.ActionDeny, "auto 危险命令仍拒绝"},
		{dto.PermissionTierAuto, true, "dd if=/dev/zero of=/dev/sda", toolspermission.ActionDeny, "auto dd 仍拒绝"},
		{dto.PermissionTierAuto, false, "computer_click", toolspermission.ActionAsk, "auto 桌面仍提权"},

		// full：全部六组，只剩"危险命令"这一道硬拦。
		{dto.PermissionTierFull, false, "write_file", toolspermission.ActionAllow, "full 写文件免审"},
		{dto.PermissionTierFull, false, "computer_click", toolspermission.ActionAllow, "full 桌面免审"},
		{dto.PermissionTierFull, false, "mcp_load", toolspermission.ActionAllow, "full 能力面免审"},
		{dto.PermissionTierFull, true, "rm -rf /", toolspermission.ActionDeny, "full 危险命令仍拒绝"},
	}
	for _, test := range cases {
		state := &PermissionGate{SessionFromContext: toolspermission.SessionIDFromContext}
		state.Set(cfg, nil)
		if err := state.SetPermissionTierFor("matrix-session", test.tier); err != nil {
			t.Fatalf("[%s] SetPermissionTierFor: %v", test.tier, err)
		}
		name, args := test.target, `{}`
		if test.bash {
			name, args = "bash", test.target
		}
		ctx := toolspermission.WithSessionID(context.Background(), "matrix-session")
		// 判据是**组合**的：向量授予 → 执行门直接给动作；向量外 → 交回框架的规则层
		// （ok=false 是刻意的回落口，不是漏判），所以这里断言组合后的结论。
		action, decided := state.Enforce(ctx, root, frameworktools.ToolMeta{}, name, args)
		if !decided {
			result, _ := state.decideFor(root, name, frameworktools.ToolMeta{}, args)
			switch result {
			case toolspermission.ResultAllow:
				action = toolspermission.ActionAllow
			case toolspermission.ResultDeny:
				action = toolspermission.ActionDeny
			default:
				action = toolspermission.ActionAsk
			}
		}
		if action != test.want {
			t.Fatalf("[%s] %s: 判定 = %v, want %v", test.tier, test.comment, action, test.want)
		}
	}
}

// TestTierVectorPresetsMonotone 钉住预设的两条不变量：
//   - 档位升序时授予的组**只增不减**（否则"切到更宽的档"会静默收回能力）；
//   - 共享桌面（rw_desktop）与能力面（adm）只在 full 档的向量里（产品安全底线）；
//   - 未识别 / 空档位按 manual（最窄）处理。
func TestTierVectorPresetsMonotone(t *testing.T) {
	order := []string{dto.PermissionTierManual, dto.PermissionTierEdit, dto.PermissionTierAuto, dto.PermissionTierFull}
	var previous map[string]bool
	for _, tier := range order {
		current := make(map[string]bool, len(order))
		for _, tag := range policy.TierVector(tier) {
			current[tag] = true
			if tier != dto.PermissionTierFull && (tag == policy.GroupRWDesktop || tag == policy.GroupADM) {
				t.Fatalf("档位 %q 不该授予共享桌面/能力面（只有 full 才在向量里）", tier)
			}
		}
		for tag := range previous {
			if !current[tag] {
				t.Fatalf("档位 %q 比它更窄的档少授予了 %q（切档会静默收权）", tier, tag)
			}
		}
		previous = current
	}
	for _, tier := range []string{"", "unknown-tier"} {
		if got := policy.TierVector(tier); len(got) != len(policy.TierVector(dto.PermissionTierManual)) {
			t.Fatalf("未识别档位 %q 应回落到 manual，得到 %v", tier, got)
		}
	}
}

// TestTierCatalogMirrorsDTO 钉住档位目录（id 序 + 标签齐全），前端按它渲染列表。
func TestTierCatalogMirrorsDTO(t *testing.T) {
	tiers := policy.DefaultPermissionTiers()
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
	state.Set(policy.DefaultPermissionConfig(), handler)
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

// TestTierDoesNotBypassEmployeeBoundary 钉住**主体边界**：
//  1. 员工在**权限范围内**的调用（readwrite 写文件）**直通执行**——放行依据是 leader
//     下发的位，不是 root 的档位（赋权即免审批，2026-10-03 裁决）；
//  2. 员工**越出范围**的调用（ctl/adm）仍走执行选择页面——即便 root 与员工会话都切到
//     full 档，full 也绝不连带放行员工的位外能力。
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
	// 员工（readwrite）在权限范围内写文件：直接执行，不弹页（放行靠位，不靠档）。
	empCtx := probeCtx("role-1")
	if err := runTierTool(state, empCtx, "write_file", `{"path":"a.txt"}`, ran); err != nil {
		t.Fatalf("员工位内的写文件应当直通执行: %v", err)
	}
	if *approvals != 0 {
		t.Fatalf("员工位内调用不该弹审批页，approvals=%d, want 0", *approvals)
	}
	if *ran != 1 {
		t.Fatalf("员工位内调用应当真的执行一次，ran=%d, want 1", *ran)
	}
	// 员工碰 adm（能力面）：位外 → 走审批页，不静默放行。
	if err := runTierTool(state, empCtx, "switch_plugin", `{}`, ran); err != nil {
		t.Fatalf("员工碰能力面应当走审批页: %v", err)
	}
	if *approvals != 1 {
		t.Fatalf("员工碰能力面的审批数 = %d, want 1（full 档不得连带放行）", *approvals)
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
	subCtx := withSubjectClass(context.Background(), policy.SubjectClassSub)
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
// 在 B 上仍然生效（full 档免审批，但不免 deny——后者见 TestTierDecisionMatrix）。
func TestTierSessionIsolation(t *testing.T) {
	state := &PermissionGate{SessionFromContext: func(ctx context.Context) string {
		sessionID, _ := ctx.Value(probeSessionKey{}).(string)
		return sessionID
	}}
	state.Set(toolspermission.PermissionConfig{
		Mode:  toolspermission.ModeManual,
		Rules: []toolspermission.PermissionRule{{ToolName: "bash", Action: toolspermission.ActionAsk}},
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
		Rules: []toolspermission.PermissionRule{{ToolName: "bash", Action: toolspermission.ActionAsk}},
	}, nil)
	if err := state.SetPermissionTierFor("session-a", dto.PermissionTierFull); err != nil {
		t.Fatal(err)
	}
	state.Set(toolspermission.PermissionConfig{
		Mode:  toolspermission.ModeManual,
		Rules: []toolspermission.PermissionRule{{ToolName: "bash", Action: toolspermission.ActionAsk}},
	}, nil)
	if got := state.PermissionTierFor("session-a"); got != dto.PermissionTierFull {
		t.Fatalf("Set 之后会话档位 = %q, want full（不得被配置刷新吃掉）", got)
	}
}
