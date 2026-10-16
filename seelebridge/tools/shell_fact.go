package tools

import (
	"github.com/RedHuang-0622/seelex/backend/limit/policy"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/backend/limit/gate"
)

// shell_fact.go — "bash 工具实际落到哪个壳"的**单一事实源**（2026-10-15 审计 D4）。
//
// 现场：同一台机器上 `bash` 可能落到 Git Bash、WSL 的 bash（被 IsWSLBash 拒）或退化成
// PowerShell / cmd。这份结论此前**每次调用现算**（`scopedBashCommand`），而模型面（工具
// 描述、系统提示）与权限的免打断模式表都按 POSIX 写死——两者一旦不一致，模型写出的命令
// 就是"**过闸即死**"：白名单放行（模式是 POSIX 形态）、执行层跑的是另一个壳、命令失败，
// 模型拿到的只有一条执行错误（审计实测：479 次调用里 165 次折在这条缝上）。
//
// 因此：装配期解析一次（进程内缓存），执行层、模型面、权限表**读同一份结论**。

// ShellFact 是**唯一一份**实际壳结论：谁需要知道"命令会被谁执行"，都读它。
type ShellFact struct {
	Kind policy.ShellKind
	// Path 是壳的可执行文件路径。
	Path string
	// Name 是给人/模型看的名字（如 "Windows PowerShell 5.1"）。
	Name string
}

// shellFactOnce 保证"装配期解析一次"：结论在进程内不会变（探针只做文件存在性检查）。
var shellFactOnce struct {
	sync.Once
	fact ShellFact
}

// ResolveShellFact 解析（并缓存）本进程 bash 工具实际会用的壳。
func ResolveShellFact() ShellFact {
	shellFactOnce.Do(func() {
		shellFactOnce.fact = resolveShellFact()
	})
	return shellFactOnce.fact
}

// resolveShellFact 是探针本身：优先 Git Bash（模型写的绝大多数命令是 POSIX 语法），
// 再系统 bash，最后 PowerShell / cmd。参数与 `scopedBashCommand` 的历史口径一致——
// 这里只是把"每次现算"变成"装配期算一次"。
func resolveShellFact() ShellFact {
	if runtime.GOOS == "windows" {
		for _, bash := range []string{
			`C:\Program Files\Git\bin\bash.exe`,
			`C:\Program Files\Git\usr\bin\bash.exe`,
			`C:\Program Files (x86)\Git\bin\bash.exe`,
		} {
			if security.FileExists(bash) {
				return ShellFact{Kind: policy.ShellBash, Path: bash, Name: "Git Bash"}
			}
		}
		if bash, err := exec.LookPath("bash"); err == nil && !security.IsWSLBash(bash) {
			return ShellFact{Kind: policy.ShellBash, Path: bash, Name: "bash"}
		}
	}
	if _, err := os.Stat("/bin/bash"); err == nil {
		return ShellFact{Kind: policy.ShellBash, Path: "bash", Name: "bash"}
	}
	if powershell := `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`; security.FileExists(powershell) {
		// 路径里的 v1.0 是 Windows PowerShell 的安装版本号，实际是 5.x——查一次版本要
		// 起一个进程（几百毫秒），装配期不值得，这里按路径给名字。
		return ShellFact{Kind: policy.ShellPowerShell, Path: powershell, Name: "Windows PowerShell 5.x"}
	}
	if commandPrompt := `C:\Windows\System32\cmd.exe`; security.FileExists(commandPrompt) {
		return ShellFact{Kind: policy.ShellCmd, Path: commandPrompt, Name: "cmd.exe"}
	}
	return ShellFact{Kind: policy.ShellBash, Path: "sh", Name: "sh"}
}

// shellCommandArgs 按壳的方言把一条命令折成 argv 尾巴（唯一一处方言表）。
func shellCommandArgs(kind policy.ShellKind, command string) []string {
	switch kind {
	case policy.ShellPowerShell:
		return []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", command}
	case policy.ShellCmd:
		return []string{"/d", "/s", "/c", command}
	default:
		return []string{"-c", command}
	}
}

// DialectHint 是工具描述里那句"你写命令时该用什么语法"。
func (f ShellFact) DialectHint() string {
	switch f.Kind {
	case policy.ShellPowerShell:
		return "This host executes bash commands through " + f.Name +
			": POSIX-only commands/flags (`ls -la`, `head`, `tail`, `grep`, `which`, `printenv`) " +
			"do NOT work — use `Get-ChildItem`, `Get-Content`, `Select-String`, `Select-Object -First`, " +
			"`Where-Object`, `Test-Path`, and prefer `rg` for searching."
	case policy.ShellCmd:
		return "This host executes bash commands through cmd.exe: use cmd syntax " +
			"(`dir`, `type`, `findstr`, `where`); POSIX commands/flags do NOT work."
	default:
		return "This host executes bash commands through " + f.Name + ": POSIX syntax applies."
	}
}

// PromptSection 是系统提示词里的**环境段**（模型写命令前该知道的唯一一句事实）。
func (f ShellFact) PromptSection() string {
	builder := &strings.Builder{}
	builder.WriteString("## Environment\n\n")
	builder.WriteString("- Host shell for the `bash` / `bash_bg` tools: **" + f.Name + "** (`" + f.Path + "`).\n")
	switch f.Kind {
	case policy.ShellPowerShell:
		builder.WriteString("- Write those commands in **PowerShell** syntax: `ls -la`, `head`, `tail`, `grep`, " +
			"`which`, `printenv` are not available. Use `Get-ChildItem`, `Get-Content`, `Select-String`, " +
			"`Select-Object -First`, `Test-Path`; search with `rg`.\n")
		builder.WriteString("- Commands in the POSIX style will be accepted by the approval layer and then fail at " +
			"execution: the approval whitelist and this line come from the same resolved shell.\n")
	case policy.ShellCmd:
		builder.WriteString("- Write those commands in **cmd.exe** syntax (`dir`, `type`, `findstr`, `where`); " +
			"POSIX commands will fail at execution.\n")
	default:
		builder.WriteString("- POSIX syntax applies (`ls`, `cat`, `head`, `grep`, `which` all work).\n")
	}
	return builder.String()
}
