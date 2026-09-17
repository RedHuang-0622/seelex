//go:build windows

package terminal

import (
	"os"
	"path/filepath"
	"strings"
)

// shellCandidates 是 Windows 的默认 shell 优先级：PowerShell 7 → Windows
// PowerShell → COMSPEC（通常是 cmd.exe）→ cmd.exe 兜底。与 VS Code 的默认
// 取向一致（优先现代 PowerShell，但任何 Windows 上都至少能起 cmd）。
func shellCandidates() []string {
	return []string{
		"pwsh.exe",
		"powershell.exe",
		strings.TrimSpace(os.Getenv("COMSPEC")),
		"cmd.exe",
	}
}

// defaultShellArgs 给 PowerShell 关掉版权横幅（-NoLogo）；cmd.exe 不需要参数。
// 刻意不加 -NoProfile：终端应当与用户手开 shell 的环境一致。
func defaultShellArgs(shell string) []string {
	switch strings.ToLower(filepath.Base(shell)) {
	case "pwsh.exe", "powershell.exe", "pwsh", "powershell":
		return []string{"-NoLogo"}
	default:
		return nil
	}
}
