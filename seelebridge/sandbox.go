package seelebridge

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ── CommandSandbox 端口（docs/2026-07-28-project-session-scope/sandbox-research.md）──
// 可替换的 shell 执行隔离接口：当前实现 NativeProjectCWD（项目路径门禁 +
// 凭据环境变量清洗 + 超时），明确标注"非 OS 级隔离"；isobox/agentbox 成熟后
// 经 adapter 接入同一接口（IsoboxAdapter 等），业务代码不直接依赖其 CLI。
//
// fail-fast 语义：接口返回实际实施的能力（SandboxCapabilities）；当配置要求
// 的能力不可实施时调用方必须拒绝，而不是悄悄降级成普通 exec.Command。

// SandboxCapabilities 报告实际实施的隔离能力（bash 工具返回给审计/UI）。
type SandboxCapabilities struct {
	Isolation     string // "cwd-gate"（仅项目 cwd，非 OS 级）| "os"（OS 级沙箱）
	EnvScrubbed   bool   // 凭据环境变量已清洗
	NetworkPolicy string // "allowed"（未限制）
	TimeoutSec    int    // 命令超时（0 = 无限制）
}

// CommandSandbox 是 shell 执行的隔离接口。
// Implementations must be concurrency-safe (shared across agents).
type CommandSandbox interface {
	// Prepare 构造隔离后的执行命令：注入项目根与超时，返回进程句柄与能力报告。
	// 返回错误 = 该能力组合不可实施（调用方必须 fail-fast，不降级）。
	Prepare(ctx context.Context, root string, command string, timeoutSec int) (*exec.Cmd, SandboxCapabilities, error)
}

// nativeProjectCWD 是默认实现：项目 cwd 门禁 + 凭据环境变量清洗。
// 非 OS 级隔离（与现状一致，sandbox-research.md 结论）；等 isobox 适配后
// 生产可切换 IsoboxAdapter，接口不变。
type nativeProjectCWD struct{}

func newNativeProjectCWD() CommandSandbox { return &nativeProjectCWD{} }

// Prepare 构造命令：cwd = 项目根（调用方已解析 worktree/项目根）、
// 环境变量清洗（凭据类不传给子进程）、超时由调用方 ctx 控制。
func (s *nativeProjectCWD) Prepare(ctx context.Context, root string, command string, timeoutSec int) (*exec.Cmd, SandboxCapabilities, error) {
	cmd := exec.CommandContext(ctx, commandShell(), commandShellArgs(command)...)
	cmd.Dir = root
	configureHiddenCommand(cmd)
	cmd.Env = scrubEnvironment(os.Environ())
	return cmd, SandboxCapabilities{
		Isolation: "cwd-gate", EnvScrubbed: true, NetworkPolicy: "allowed", TimeoutSec: timeoutSec,
	}, nil
}

// scrubEnvironment 清洗凭据类环境变量（子代理/主代理 bash 不得读取
// API key/secret/token）：名字包含敏感模式（大小写不敏感）的变量剔除，
// 其余保留（PATH/SystemRoot 等基础变量不变，Windows 兼容）。
func scrubEnvironment(environ []string) []string {
	scrubbed := make([]string, 0, len(environ))
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if isSensitiveEnvName(name) {
			continue
		}
		scrubbed = append(scrubbed, entry)
	}
	return scrubbed
}

// isSensitiveEnvName 判断环境变量名是否携带凭据（API key/secret/token/
// password/credential；如 OPENAI_API_KEY、ANTHROPIC_API_KEY、AWS_SECRET_ACCESS_KEY）。
func isSensitiveEnvName(name string) bool {
	upper := strings.ToUpper(name)
	for _, marker := range []string{"API_KEY", "SECRET", "TOKEN", "PASSWORD", "CREDENTIAL", "AWS_ACCESS"} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}

// commandShell 返回平台 shell（与 scopedBash 的探测逻辑一致：
// bash → PowerShell → cmd）。Windows 固定 Git 路径探测后回退 PATH 中的
// bash（自定义安装路径），避免直接跳到 PowerShell 拒绝 bash 语法。
func commandShell() string {
	if runtime.GOOS == "windows" {
		for _, bash := range []string{
			`C:\Program Files\Git\bin\bash.exe`,
			`C:\Program Files\Git\usr\bin\bash.exe`,
			`C:\Program Files (x86)\Git\bin\bash.exe`,
		} {
			if fileExists(bash) {
				return bash
			}
		}
		if bash, err := exec.LookPath("bash"); err == nil {
			return bash
		}
	}
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "bash"
	}
	if powershell := `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`; fileExists(powershell) {
		return powershell
	}
	if commandPrompt := `C:\Windows\System32\cmd.exe`; fileExists(commandPrompt) {
		return commandPrompt
	}
	return "sh"
}

// commandShellArgs 返回对应 shell 的参数前缀（与 scopedBash 一致）。
// LookPath 可能返回完整路径形式的 bash.exe，按 basename 识别。
func commandShellArgs(command string) []string {
	shell := commandShell()
	switch {
	case shell == "bash", shell == "sh", strings.HasSuffix(strings.ToLower(shell), "bash.exe"):
		return []string{"-c", command}
	case shell == `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`:
		return []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", command}
	default:
		return []string{"/d", "/s", "/c", command}
	}
}
