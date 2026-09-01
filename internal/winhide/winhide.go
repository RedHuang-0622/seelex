// Package winhide 隐藏 Windows 子进程控制台窗口（CREATE_NO_WINDOW）：
// GUI 应用在加载项目（git）、执行 bash 工具或沙箱命令时，避免弹出黑色
// 终端窗口；非 Windows 平台为空操作。
package winhide

import (
	"os/exec"
	"runtime"
	"syscall"
)

// createNoWindow 是 Windows CREATE_NO_WINDOW 标志（0x08000000）。
const createNoWindow = 0x08000000

// Apply 隐藏子进程控制台窗口（Windows）；其它平台无副作用。
func Apply(cmd *exec.Cmd) {
	if cmd == nil || runtime.GOOS != "windows" {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
