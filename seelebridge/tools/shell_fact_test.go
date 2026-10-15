package tools

import (
	"strings"
	"testing"

	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
)

// 实际壳的**单一事实源**判据（2026-10-15 审计 D4）。
//
// 缺陷本体：执行层每次调用现算"用哪个壳"，而模型面（工具描述/系统提示）与权限免打断表
// 都按 POSIX 写死。三者一旦不一致，模型写出的命令就"过闸即死"——白名单放行（模式是
// POSIX 形态）、执行层跑的是另一个壳、命令失败，模型只拿到一条执行错误。

// TestShellFactIsResolvedOnceAndDrivesExecution：解析一次、执行层与它同源。
func TestShellFactIsResolvedOnceAndDrivesExecution(t *testing.T) {
	first := ResolveShellFact()
	second := ResolveShellFact()
	if first != second {
		t.Fatalf("解析结果必须稳定（进程内缓存）：%+v vs %+v", first, second)
	}
	if first.Kind == "" || first.Path == "" || first.Name == "" {
		t.Fatalf("壳结论必须完整（kind/path/name）：%+v", first)
	}
	// 执行层读同一份结论：起命令位与 argv 尾巴都由它派生。
	path, args := scopedBashCommand("echo fact")
	if path != first.Path {
		t.Fatalf("执行层用的壳 %q 与事实源 %q 不一致", path, first.Path)
	}
	wantArgs := shellCommandArgs(first.Kind, "echo fact")
	if strings.Join(args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("argv 与方言表不一致：%v vs %v", args, wantArgs)
	}
}

// TestShellDialectIsCarriedToTheModel：模型面必须拿到**方言**（工具描述 + 系统提示环境段）——
// 不然它会照着 POSIX 写，而本机壳可能根本不认那些命令。
func TestShellDialectIsCarriedToTheModel(t *testing.T) {
	cases := []struct {
		kind         ShellKind
		hintWants    []string
		sectionWants []string
		hintAbsent   []string
	}{
		{
			kind:         ShellPowerShell,
			hintWants:    []string{"Get-ChildItem", "Select-String", "do NOT work"},
			sectionWants: []string{"PowerShell", "Get-ChildItem"},
			hintAbsent:   []string{"POSIX syntax applies"},
		},
		{
			kind:         ShellBash,
			hintWants:    []string{"POSIX syntax applies"},
			sectionWants: []string{"POSIX syntax applies"},
			hintAbsent:   []string{"do NOT work"},
		},
		{
			kind:         ShellCmd,
			hintWants:    []string{"cmd.exe", "dir"},
			sectionWants: []string{"cmd.exe", "dir"},
			hintAbsent:   []string{"POSIX syntax applies"},
		},
	}
	for _, testCase := range cases {
		fact := ShellFact{Kind: testCase.kind, Path: "x", Name: "test-shell"}
		hint := fact.DialectHint()
		section := fact.PromptSection()
		for _, want := range testCase.hintWants {
			if !strings.Contains(hint, want) {
				t.Fatalf("%s：工具描述缺方言标记 %q：%q", testCase.kind, want, hint)
			}
		}
		for _, want := range testCase.sectionWants {
			if !strings.Contains(section, want) {
				t.Fatalf("%s：环境段缺 %q：%q", testCase.kind, want, section)
			}
		}
		for _, unwanted := range testCase.hintAbsent {
			if strings.Contains(hint, unwanted) {
				t.Fatalf("%s：工具描述不该说 %q：%q", testCase.kind, unwanted, hint)
			}
		}
		// 环境段必须点名**这个壳**（模型据此决定写哪种方言）。
		if !strings.Contains(section, "test-shell") {
			t.Fatalf("%s：环境段没点名实际壳：%q", testCase.kind, section)
		}
	}
}

// TestPermissionAllowListFollowsTheShellDialect：免打断白名单按方言分派，其余层只有一份。
func TestPermissionAllowListFollowsTheShellDialect(t *testing.T) {
	posix := PermissionRulesForShell(ShellBash)
	powershell := PermissionRulesForShell(ShellPowerShell)

	// 只有"免打断读命令"那一层的 patterns 变；ask / deny 层逐条相同（一份判据）。
	if len(posix) != len(powershell) {
		t.Fatalf("两组规则条数不同（层结构变了）：%d vs %d", len(posix), len(powershell))
	}
	for index := range posix {
		if posix[index].ToolName != powershell[index].ToolName || posix[index].Action != powershell[index].Action {
			t.Fatalf("第 %d 条规则的工具/动作不该随方言变：%+v vs %+v", index, posix[index], powershell[index])
		}
		if posix[index].Action != toolspermission.ActionAllow {
			if strings.Join(posix[index].Patterns, ",") != strings.Join(powershell[index].Patterns, ",") {
				t.Fatalf("第 %d 条是 ask/deny 层，不该随方言变：%v vs %v", index, posix[index].Patterns, powershell[index].Patterns)
			}
		}
	}

	powershellAllow := bashAllowListForShell(ShellPowerShell)
	for _, dead := range []string{"ls *", "cat *", "head *", "tail *", "which *", "df *", "du *", "printenv"} {
		if containsString(powershellAllow, dead) {
			t.Fatalf("PowerShell 宿主不该放行 POSIX 形态 %q：它会过闸即死（审计 D4）", dead)
		}
	}
	for _, live := range []string{"Get-ChildItem*", "Select-String *", "Test-Path *"} {
		if !containsString(powershellAllow, live) {
			t.Fatalf("PowerShell 宿主应放行它的方言形态 %q", live)
		}
	}
	// 共享槽（两种壳下都成立）与壳无关，必须两边都在。
	for _, shared := range []string{"git *", "go test *"} {
		if !containsString(powershellAllow, shared) || !containsString(bashAllowListForShell(ShellBash), shared) {
			t.Fatalf("共享模式 %q 在两种方言下都该放行", shared)
		}
	}
	// 默认入口保持 POSIX 口径（用例与宿主机器无关）。
	if got := PermissionRulesForShell(ShellBash); len(got) != len(DefaultPermissionRules()) {
		t.Fatalf("默认规则必须等价于 POSIX 方言")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
