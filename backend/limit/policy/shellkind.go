package policy

// shellkind.go —— 执行壳类别的**词表**（权限表按它分派方言）。
//
// 2026-10-16 从 seelebridge/tools/shell_fact.go 的判据段归位（M2-C4，行为零变化）：
// ShellKind 是"权限表怎么分派 bash 免打断模式"的判据，因此与权责表同处一包；
// 壳的**实际解析结论**（ShellFact / ResolveShellFact）仍留在 backend/tool/registry。

// ShellKind 是执行壳的类别：模型面与权限表按它分派方言。
type ShellKind string

const (
	// ShellBash 是 POSIX 方言的壳（Git Bash / 系统 bash / sh）。
	ShellBash ShellKind = "bash"
	// ShellPowerShell 是 Windows PowerShell（本机 bash 不可用时的兜底之一）。
	ShellPowerShell ShellKind = "powershell"
	// ShellCmd 是 cmd.exe（最后兜底）。
	ShellCmd ShellKind = "cmd"
)
