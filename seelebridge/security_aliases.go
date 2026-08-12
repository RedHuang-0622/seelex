package seelebridge

import (
	"os/exec"

	"github.com/RedHuang-0622/seelex/seelebridge/security"
)

// ── security 域 API 兼容别名 ────────────────────────────────────────────
// sandbox 域已迁入 seelebridge/security；本文件在根包重导出公开类型，
// 保证既有调用面（Runtime 装配、外部接线）不因拆包而破坏。

// SandboxCapabilities 报告实际实施的隔离能力（bash 工具返回给审计/UI）。
type SandboxCapabilities = security.SandboxCapabilities

// CommandSandbox 是 shell 执行的隔离接口（安全域定义）。
type CommandSandbox = security.CommandSandbox

// NewNativeProjectCWD 构造默认 CommandSandbox 实现（项目 cwd 门禁 + 凭据清洗）。
func NewNativeProjectCWD() CommandSandbox { return security.NewNativeProjectCWD() }

// ScrubEnvironment 清洗凭据类环境变量（供根包命令执行路径复用）。
func ScrubEnvironment(environ []string) []string { return security.ScrubEnvironment(environ) }

// FileExists 报告路径是否存在（供根包 shell 探测复用）。
func FileExists(path string) bool { return security.FileExists(path) }

// ConfigureHiddenCommand 防止 GUI 构建为每个 scoped bash 调用闪现控制台窗口。
func ConfigureHiddenCommand(cmd *exec.Cmd) { security.ConfigureHiddenCommand(cmd) }
