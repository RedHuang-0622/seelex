package main

// main.go —— MVP 驱动：十组场景全 PASS 才 exit 0。
//
//	S1  只给 {ssh}：能连不能动，且 lint 报警但判定不变
//	S2  {rw, ssh}：远端能写不能读、本地也不可读（"不合理但要支持"的那一格）
//	S3  {ro, ssh}：项目根内可读，越界读被拒（缺 ssh.userspace）
//	S4  {ro, ssh, ssh.userspace}：越界读放行
//	S5  空装配：缺格 = deny（默认拒绝）
//	S6  ask 覆盖整组：一次审批，不逐格问
//	S7  lint 不改变判定
//	S8  老档位预设单调不减（manual ≤ edit ≤ auto ≤ full）
//	S9  装配 JSON 往返保真（契约稳定性）
//	S10 中间件短路：deny 不进 handler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
)

func main() {
	scenarios := []struct {
		name string
		run  func() error
	}{
		{"S1 只给 {ssh}：能连不能动", scenarioSSHOnly},
		{"S2 {rw, ssh}：远端能写不能读", scenarioWritePlusSSH},
		{"S3 {ro, ssh}：越界读被拒（缺 ssh.userspace）", scenarioUserSpaceDenied},
		{"S4 {ro, ssh, ssh.userspace}：越界读放行", scenarioUserSpaceAllowed},
		{"S5 空装配：缺格 = deny", scenarioDenyByDefault},
		{"S6 ask 覆盖整组（一次审批）", scenarioAskCoversGroup},
		{"S7 lint 不改变判定", scenarioLintDoesNotChangeEffect},
		{"S8 老档位预设单调不减", scenarioTierPresetsMonotonic},
		{"S9 装配 JSON 往返保真", scenarioAssemblyRoundTrip},
		{"S10 中间件短路：deny 不进 handler", scenarioMiddlewareShortCircuit},
	}

	failed := 0
	for _, scenario := range scenarios {
		if err := scenario.run(); err != nil {
			failed++
			fmt.Printf("[FAIL] %s\n        %v\n", scenario.name, err)
			continue
		}
		fmt.Printf("[PASS] %s\n", scenario.name)
	}
	fmt.Printf("\n%d/%d 场景通过\n", len(scenarios)-failed, len(scenarios))
	if failed > 0 {
		os.Exit(1)
	}
}

// assemblyWith 造一份只有一个组的装配。
func assemblyWith(group GroupID, grants map[PermType]Effect) Assembly {
	cells := make(map[PermType]Grant, len(grants))
	for perm, effect := range grants {
		cells[perm] = Grant{Effect: effect}
	}
	return Assembly{Version: 1, Cells: map[GroupID]map[PermType]Grant{group: cells}}
}

// decide 是场景里常用的判定封装。
func decide(assembly Assembly, group GroupID, req Requirement) (Decision, error) {
	return Resolver{Assembly: assembly}.Decide(context.Background(), group, req)
}

func scenarioSSHOnly() error {
	assembly := assemblyWith(GroupRoot, map[PermType]Effect{PermSSH: EffectAllow})

	// 本地读：缺 ro → deny
	local, err := decide(assembly, GroupRoot, Requirement{Tool: "read_file", Path: "README.md"})
	if err != nil {
		return err
	}
	if local.Effect != EffectDeny {
		return fmt.Errorf("本地读 = %q, want deny（只给 ssh 不该带出 ro）", local.Effect)
	}
	// 远端命令：缺 rw → deny（ssh 是通道能力，不是操作能力）
	remote, err := decide(assembly, GroupRoot, Requirement{Tool: "bash", Remote: true})
	if err != nil {
		return err
	}
	if remote.Effect != EffectDeny {
		return fmt.Errorf("远端 bash = %q, want deny（缺 rw）", remote.Effect)
	}
	if !containsPerm(remote.Required, PermSSH) {
		return fmt.Errorf("远端需求里应含 ssh：%v", remote.Required)
	}
	// lint 必须报"能连不能动"，但不改变 Effect
	if len(remote.Warnings) == 0 {
		return fmt.Errorf("期望 assembly_ssh_only 告警，实际无告警")
	}
	return nil
}

func scenarioWritePlusSSH() error {
	// 用户明确要的"只给写 + ssh"：不合理，但必须可表达。
	assembly := assemblyWith(GroupEmpRW, map[PermType]Effect{
		PermRW:  EffectAllow,
		PermSSH: EffectAllow,
	})

	remoteWrite, err := decide(assembly, GroupEmpRW, Requirement{Tool: "write_file", Remote: true, Path: "src/a.go"})
	if err != nil {
		return err
	}
	if remoteWrite.Effect != EffectAllow {
		return fmt.Errorf("远端写 = %q, want allow（缺项：%v）", remoteWrite.Effect, remoteWrite.Missing)
	}

	remoteRead, err := decide(assembly, GroupEmpRW, Requirement{Tool: "read_file", Remote: true, Path: "src/a.go"})
	if err != nil {
		return err
	}
	if remoteRead.Effect != EffectDeny {
		return fmt.Errorf("远端读 = %q, want deny（没给 ro 就是不能读）", remoteRead.Effect)
	}

	localWrite, err := decide(assembly, GroupEmpRW, Requirement{Tool: "write_file", Path: "src/a.go"})
	if err != nil {
		return err
	}
	if localWrite.Effect != EffectAllow {
		return fmt.Errorf("本地写 = %q, want allow", localWrite.Effect)
	}

	localRead, err := decide(assembly, GroupEmpRW, Requirement{Tool: "read_file", Path: "src/a.go"})
	if err != nil {
		return err
	}
	if localRead.Effect != EffectDeny {
		return fmt.Errorf("本地读 = %q, want deny", localRead.Effect)
	}
	// lint：有 rw 无 ro
	if !hasWarning(localRead.Warnings, "assembly_write_without_read") {
		return fmt.Errorf("期望 write_without_read 告警，实际：%v", localRead.Warnings)
	}
	return nil
}

func scenarioUserSpaceDenied() error {
	assembly := assemblyWith(GroupRoot, map[PermType]Effect{PermRO: EffectAllow, PermSSH: EffectAllow})

	inRoot, err := decide(assembly, GroupRoot, Requirement{Tool: "read_file", Remote: true, Path: "src/a.go"})
	if err != nil {
		return err
	}
	if inRoot.Effect != EffectAllow {
		return fmt.Errorf("根内远端读 = %q, want allow", inRoot.Effect)
	}

	outRoot, err := decide(assembly, GroupRoot, Requirement{Tool: "read_file", Remote: true, Path: "~/.ssh/id_rsa", OutOfRoot: true})
	if err != nil {
		return err
	}
	if outRoot.Effect != EffectDeny {
		return fmt.Errorf("越界远端读 = %q, want deny（缺 ssh.userspace）", outRoot.Effect)
	}
	if !containsPerm(outRoot.Missing, PermSSHUserSpace) {
		return fmt.Errorf("缺项里应含 ssh.userspace：%v", outRoot.Missing)
	}
	if !containsPerm(outRoot.Required, PermSSHUserSpace) {
		return fmt.Errorf("越界时需求里应自动追加 ssh.userspace：%v", outRoot.Required)
	}
	return nil
}

func scenarioUserSpaceAllowed() error {
	assembly := assemblyWith(GroupRoot, map[PermType]Effect{
		PermRO:           EffectAllow,
		PermSSH:          EffectAllow,
		PermSSHUserSpace: EffectAllow,
	})
	outRoot, err := decide(assembly, GroupRoot, Requirement{Tool: "read_file", Remote: true, Path: "~/.ssh/id_rsa", OutOfRoot: true})
	if err != nil {
		return err
	}
	if outRoot.Effect != EffectAllow {
		return fmt.Errorf("给了 userspace 后应放行，实际 %q（缺项 %v）", outRoot.Effect, outRoot.Missing)
	}
	return nil
}

func scenarioDenyByDefault() error {
	empty := Assembly{Version: 1, Cells: map[GroupID]map[PermType]Grant{}}
	cases := []Requirement{
		{Tool: "read_file", Path: "a"},
		{Tool: "write_file", Path: "a"},
		{Tool: "bash"},
		{Tool: "bash", Remote: true},
		{Tool: "unknown_tool"}, // 未登记工具 fail-closed
	}
	for _, req := range cases {
		decision, err := decide(empty, GroupRoot, req)
		if err != nil {
			return err
		}
		if decision.Effect != EffectDeny {
			return fmt.Errorf("%+v 空装配下 = %q, want deny", req, decision.Effect)
		}
	}
	// 未登记工具应当要求 adm（最贵），而不是"没登记所以放行"
	decision, _ := decide(empty, GroupRoot, Requirement{Tool: "unknown_tool"})
	if !containsPerm(decision.Required, PermADM) {
		return fmt.Errorf("未登记工具的需求应含 adm：%v", decision.Required)
	}
	return nil
}

func scenarioAskCoversGroup() error {
	assembly := assemblyWith(GroupRoot, map[PermType]Effect{
		PermRO:  EffectAsk,
		PermSSH: EffectAsk,
	})
	approveCalls := 0
	terminalCalls := 0
	handler := PermissionMiddleware(
		Resolver{Assembly: assembly},
		func(context.Context) GroupID { return GroupRoot },
		func(_ context.Context, call Call) Requirement {
			return Requirement{Remote: call.Args["remote"] == "true"}
		},
		func(context.Context, Decision) (bool, error) { approveCalls++; return true, nil },
	)("read_file", func(context.Context, Call) (Result, error) {
		terminalCalls++
		return Result{Reached: true}, nil
	})

	res, err := handler(context.Background(), Call{Name: "read_file", Args: map[string]string{"remote": "true"}})
	if err != nil {
		return fmt.Errorf("审批通过后不应报错：%w", err)
	}
	if approveCalls != 1 {
		return fmt.Errorf("审批次数 = %d, want 1（整组一次，不逐格问）", approveCalls)
	}
	if terminalCalls != 1 || !res.Reached {
		return fmt.Errorf("审批通过后应进 handler：terminal=%d res=%+v", terminalCalls, res)
	}
	return nil
}

func scenarioLintDoesNotChangeEffect() error {
	// 有 ssh.userspace 无 ssh：lint 报警，但判定照常（该格授予无效而已）
	assembly := assemblyWith(GroupRoot, map[PermType]Effect{
		PermRO:           EffectAllow,
		PermSSHUserSpace: EffectAllow,
	})
	decision, err := decide(assembly, GroupRoot, Requirement{Tool: "read_file", Path: "a"})
	if err != nil {
		return err
	}
	if decision.Effect != EffectAllow {
		return fmt.Errorf("lint 不应改变判定：Effect = %q", decision.Effect)
	}
	if !hasWarning(decision.Warnings, "assembly_userspace_without_ssh") {
		return fmt.Errorf("期望 userspace_without_ssh 告警，实际：%v", decision.Warnings)
	}
	return nil
}

func scenarioTierPresetsMonotonic() error {
	tiers := []string{"manual", "edit", "auto", "full"}
	requirements := []Requirement{
		{Tool: "read_file", Path: "a"},
		{Tool: "write_file", Path: "a"},
		{Tool: "bash"},
		{Tool: "bash_bg_control_probe"}, // 未登记 → adm
		{Tool: "read_file", Remote: true, OutOfRoot: true},
	}
	groups := []GroupID{GroupRoot, GroupSub, GroupEmpRO, GroupEmpRW}

	for _, group := range groups {
		for _, req := range requirements {
			previous := -1
			for _, tier := range tiers {
				decision, err := decide(PresetForTier(tier), group, req)
				if err != nil {
					return err
				}
				rank := decision.Effect.rank()
				if rank < previous {
					return fmt.Errorf("档位不单调：group=%s tier=%s req=%+v effect=%s < 上一档",
						group, tier, req, decision.Effect)
				}
				previous = rank
			}
			// full 必须全放行
			full, _ := decide(PresetForTier("full"), group, req)
			if full.Effect != EffectAllow {
				return fmt.Errorf("full 档应放行：group=%s req=%+v effect=%s", group, req, full.Effect)
			}
		}
	}

	// 预设不放大主体能力：emp_ro 在 full 档也不该拿到 rw 之外的东西（全 allow 仅限在册类型）
	if decision, _ := decide(PresetForTier("manual"), GroupEmpRO, Requirement{Tool: "write_file", Path: "a"}); decision.Effect != EffectDeny {
		return fmt.Errorf("emp_ro + manual 应当写不了，实际 %q", decision.Effect)
	}
	return nil
}

func scenarioAssemblyRoundTrip() error {
	original := assemblyWith(GroupEmpRW, map[PermType]Effect{
		PermRW:           EffectAllow,
		PermSSH:          EffectAllow,
		PermSSHUserSpace: EffectDeny,
	})
	original.Cells[GroupRoot] = map[PermType]Grant{PermADM: {Effect: EffectAsk, Bits: 7}}

	raw, err := json.Marshal(original)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	var restored Assembly
	if err := json.Unmarshal(raw, &restored); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	if !reflect.DeepEqual(original, restored) {
		return fmt.Errorf("往返不保真：\n original=%+v\n restored=%+v", original, restored)
	}
	return nil
}

func scenarioMiddlewareShortCircuit() error {
	assembly := assemblyWith(GroupSub, map[PermType]Effect{PermRO: EffectDeny})
	terminalCalls := 0
	handler := PermissionMiddleware(
		Resolver{Assembly: assembly},
		func(context.Context) GroupID { return GroupSub },
		func(_ context.Context, _ Call) Requirement { return Requirement{} },
		func(context.Context, Decision) (bool, error) { return true, nil },
	)("read_file", func(context.Context, Call) (Result, error) {
		terminalCalls++
		return Result{Reached: true}, nil
	})

	_, err := handler(context.Background(), Call{Name: "read_file"})
	if err == nil {
		return fmt.Errorf("deny 应当返回错误")
	}
	if terminalCalls != 0 {
		return fmt.Errorf("deny 时 handler 被调用了 %d 次（短路失效）", terminalCalls)
	}
	var denied *ErrDenied
	if !asDenied(err, &denied) {
		return fmt.Errorf("期望 ErrDenied，实际 %T：%v", err, err)
	}
	return nil
}

// ── 小工具 ────────────────────────────────────────────────────────────

func containsPerm(perms []PermType, want PermType) bool {
	for _, perm := range perms {
		if perm == want {
			return true
		}
	}
	return false
}

func containsStr(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hasWarning(warnings []string, prefix string) bool {
	for _, warning := range warnings {
		if len(warning) >= len(prefix) && warning[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func asDenied(err error, target **ErrDenied) bool {
	for err != nil {
		if denied, ok := err.(*ErrDenied); ok {
			*target = denied
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}
