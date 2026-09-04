//go:build windows

package winhide

import (
	"os/exec"
	"syscall"
)

// createNoWindow 是 Windows CREATE_NO_WINDOW 标志（0x08000000）。
const createNoWindow = 0x08000000

// Apply 隐藏子进程控制台窗口（Windows）；其它平台无副作用。
func Apply(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
