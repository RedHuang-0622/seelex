package terminal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ShellEnvName 是覆盖默认 shell 的环境变量（本地个人化配置，不进代码契约）：
// 例如 PowerShell 7 / Git Bash / WSL 用户可指定完整路径。
const ShellEnvName = "SEELEX_TERMINAL_SHELL"

// ShellFor 解析本平台要拉起的 shell：显式指定 > 环境变量 > 平台候选清单
// （见 shell_windows.go / shell_other.go）。返回的可执行文件已通过
// exec.LookPath 解析为绝对路径——PTY 上启动失败往往是"找不到命令"，提前解析
// 能把错误在 Open 阶段就报出来，而不是留一个立刻退出的空终端。
func ShellFor(explicit string, args []string) (string, []string, error) {
	requested := strings.TrimSpace(explicit)
	if requested == "" {
		requested = strings.TrimSpace(os.Getenv(ShellEnvName))
	}
	if requested != "" {
		path, err := exec.LookPath(requested)
		if err != nil {
			return "", nil, fmt.Errorf("terminal: shell %q not found: %w", requested, err)
		}
		return path, append([]string(nil), args...), nil
	}
	for _, candidate := range shellCandidates() {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		path, err := exec.LookPath(candidate)
		if err != nil {
			continue
		}
		return path, defaultShellArgs(path), nil
	}
	return "", nil, errors.New("terminal: no usable shell found on this host")
}
