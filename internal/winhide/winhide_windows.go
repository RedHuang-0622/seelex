//go:build windows

package winhide

import (
	"os/exec"
	"syscall"
)

// CreateNoWindow 是 Windows CREATE_NO_WINDOW 标志（0x08000000）。导出是为了让
// 其它写 SysProcAttr 的包能断言同一位，不必各自重复魔数。
const CreateNoWindow = 0x08000000

// Apply 隐藏子进程控制台窗口（Windows）；其它平台无副作用。
//
// 只补自己这两位：SysProcAttr 可能已被调用方设过别的 flag（如进程组位），
// 整体赋值会把它们抹掉。
func Apply(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= CreateNoWindow
}
