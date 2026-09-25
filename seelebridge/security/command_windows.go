//go:build windows

package security

import (
	"os/exec"
	"syscall"
)

// ConfigureHiddenCommand prevents a GUI build from flashing a console window
// for each scoped bash invocation.
//
// 只补 HideWindow 这一位：SysProcAttr 还有别的写者（winhide.Apply 的
// CREATE_NO_WINDOW、ConfigureProcessTree 的进程组位），整体赋值会把它们抹掉。
func ConfigureHiddenCommand(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
}
