//go:build !windows

package terminal

import (
	"os"
)

// shellCandidates 是非 Windows 的默认 shell 优先级：用户登录 shell（$SHELL）
// → /bin/bash → /bin/sh。
func shellCandidates() []string {
	return []string{
		os.Getenv("SHELL"),
		"/bin/bash",
		"/bin/sh",
	}
}

// defaultShellArgs 不传参：登录 shell（-l）会把工作目录切到 $HOME，与"终端开在
// 工作区根"的语义冲突；stdin 是 TTY 时 shell 自己就会进交互模式。
func defaultShellArgs(string) []string {
	return nil
}
