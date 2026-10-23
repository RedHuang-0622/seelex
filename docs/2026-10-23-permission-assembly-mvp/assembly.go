package main

// assembly.go —— 矩阵判定、需求集合、lint、老档位预设。

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// ── 需求集合：工具 + 执行面 → 所需权限类型（**唯一一份**）──────────────

// toolBasePerms 是"工具 → 本地所需权限类型"的唯一清单。
//
// 与 backend/tool/registry/scoped_names.go 的项目作用域名单同构：新增工具必须
// 在这里登记所需权限，未登记的工具一律 **fail-closed**（当作需要 adm），不允许
// "没登记所以放行"。
var toolBasePerms = map[string][]PermType{
	"read_file":   {PermRO},
	"grep_search": {PermRO},
	"glob":        {PermRO},
	"write_file":  {PermRW},
	"edit_file":   {PermRW},
	"bash":        {PermRW},
	"bg_manage":   {PermRW},
}

// RequiredPerms 展开一次调用所需权限类型集合。
func RequiredPerms(req Requirement) []PermType {
	base, known := toolBasePerms[req.Tool]
	out := make([]PermType, 0, len(base)+2)
	if !known {
		// 未登记工具：fail-closed，要求最贵的权限，绝不默认放行。
		out = append(out, PermADM)
	} else {
		out = append(out, base...)
	}
	if req.Remote {
		out = append(out, PermSSH)
		if req.OutOfRoot {
			out = append(out, PermSSHUserSpace)
		}
	}
	return out
}

// ── 判定 ──────────────────────────────────────────────────────────────

// Resolver 是按装配判定权限的 Gate 实现。
type Resolver struct {
	Assembly Assembly
}

var _ Gate = Resolver{}

// Decide 实现 Gate：任一格 deny/缺格 ⇒ deny；任一 ask ⇒ ask（一次审批覆盖整组）；
// 全 allow ⇒ allow。
func (r Resolver) Decide(_ context.Context, group GroupID, req Requirement) (Decision, error) {
	required := RequiredPerms(req)
	decision := Decision{Required: required, Effect: EffectAllow}
	if len(required) == 0 {
		return decision, nil
	}

	cells := r.Assembly.Cells[group]
	asked := false
	for _, perm := range required {
		grant, ok := cells[perm]
		if !ok {
			// 缺格 = deny：默认拒绝，不是默认允许。
			decision.Missing = append(decision.Missing, perm)
			continue
		}
		if grant.Effect == EffectDeny {
			decision.Missing = append(decision.Missing, perm)
			continue
		}
		if grant.Effect == EffectAsk {
			asked = true
		}
	}

	// lint 只进 Warnings，**不改变 Effect**（见 §5：不替用户否决"不合理但明确要的"装配）。
	decision.Warnings = Lint(r.Assembly, group)

	switch {
	case len(decision.Missing) > 0:
		decision.Effect = EffectDeny
		decision.Reason = fmt.Sprintf("缺少权限：%s", joinPerms(decision.Missing))
	case asked:
		decision.Effect = EffectAsk
		decision.Reason = "需要审批（整组一次）"
	default:
		decision.Effect = EffectAllow
	}
	return decision, nil
}

func joinPerms(perms []PermType) string {
	parts := make([]string, len(perms))
	for i, perm := range perms {
		parts[i] = string(perm)
	}
	return strings.Join(parts, "+")
}

// ── lint：可表达但可疑的装配，只报警 ──────────────────────────────────

// Lint 返回该组装配的告警（不参与判定）。
func Lint(assembly Assembly, group GroupID) []string {
	cells := assembly.Cells[group]
	granted := func(perm PermType) bool {
		g, ok := cells[perm]
		return ok && g.Effect != EffectDeny
	}
	warnings := make([]string, 0, 4)

	anyOp := false
	for _, perm := range []PermType{PermRO, PermRW, PermRWSession, PermRWDesktop, PermCTL, PermADM} {
		if granted(perm) {
			anyOp = true
			break
		}
	}
	if granted(PermSSH) && !anyOp {
		warnings = append(warnings, "assembly_ssh_only：能连不能动（没有任何操作权限类型）")
	}
	if granted(PermRW) && !granted(PermRO) {
		warnings = append(warnings, "assembly_write_without_read：能写不能读回校验")
	}
	if granted(PermSSHUserSpace) && !granted(PermSSH) {
		warnings = append(warnings, "assembly_userspace_without_ssh：授予无效（没有 ssh 权限）")
	}
	if granted(PermADM) && !granted(PermCTL) {
		warnings = append(warnings, "assembly_admin_without_control：能改能力面却停不了循环")
	}
	return warnings
}

// ── 老档位预设（迁移用）──────────────────────────────────────────────

// legacyGroupDefaults 是**当前**组表的口径（backend/limit/policy 的
// DefaultPermissionGroupList）：只用于生成保守预设，不代表最终事实。
func legacyGroupDefaults() map[PermType]Grant {
	return map[PermType]Grant{
		PermRO:        {Effect: EffectAllow},
		PermRW:        {Effect: EffectAllow},
		PermRWSession: {Effect: EffectAllow},
		PermRWDesktop: {Effect: EffectAsk},
		PermCTL:       {Effect: EffectAsk},
		PermADM:       {Effect: EffectAsk},
		// 新增的两类在老档位里一律 deny：老配置没有"远端"概念，不能凭空放开。
		PermSSH:          {Effect: EffectDeny},
		PermSSHUserSpace: {Effect: EffectDeny},
	}
}

// PresetForTier 把老档位翻译成矩阵预设。
//
// 阶梯语义保留（单调不减），但表达方式从"删 ask 规则"变成"改格子的 Effect"：
// 这样矩阵成为唯一判定面，`full` 也不必再依赖 BypassAll 短路。
func PresetForTier(tier string) Assembly {
	cells := legacyGroupDefaults()
	switch tier {
	case "edit":
		// 写盘不再打断：rw 已 allow，本档在矩阵里与 manual 同形（ask 规则属工具级，
		// 迁移期要逐条对照，见文档 §4 的风险点）。
	case "auto":
		// 任意命令直跑：把 ask 的三类降为 allow，但 deny 型（危险命令）不在矩阵里，
		// 仍由既有规则表硬拦。
		for _, perm := range []PermType{PermRWDesktop, PermCTL, PermADM} {
			cells[perm] = Grant{Effect: EffectAllow}
		}
	case "full":
		for perm := range cells {
			cells[perm] = Grant{Effect: EffectAllow}
		}
	}
	out := Assembly{Version: 1, Cells: map[GroupID]map[PermType]Grant{}}
	for _, group := range []GroupID{GroupRoot, GroupSub, GroupEmpRO, GroupEmpRW} {
		copied := make(map[PermType]Grant, len(cells))
		for perm, grant := range cells {
			copied[perm] = grant
		}
		// 主体的既有收窄（sub 无 ctl/adm；emp_ro 只读）仍然成立——预设不放大主体能力。
		switch group {
		case GroupSub:
			copied[PermCTL] = Grant{Effect: EffectDeny}
			copied[PermADM] = Grant{Effect: EffectDeny}
			copied[PermRWDesktop] = Grant{Effect: EffectDeny}
		case GroupEmpRO:
			copied[PermRW] = Grant{Effect: EffectDeny}
			copied[PermRWSession] = Grant{Effect: EffectDeny}
			copied[PermRWDesktop] = Grant{Effect: EffectDeny}
			copied[PermCTL] = Grant{Effect: EffectDeny}
			copied[PermADM] = Grant{Effect: EffectDeny}
		case GroupEmpRW:
			copied[PermRWDesktop] = Grant{Effect: EffectDeny}
			copied[PermCTL] = Grant{Effect: EffectDeny}
			copied[PermADM] = Grant{Effect: EffectDeny}
		}
		if tier == "full" {
			for perm := range copied {
				copied[perm] = Grant{Effect: EffectAllow}
			}
		}
		out.Cells[group] = copied
	}
	return out
}

// sortedPerms 让 Decision 输出稳定（测试与轨迹可读）。
func sortedPerms(perms []PermType) []PermType {
	out := append([]PermType(nil), perms...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
