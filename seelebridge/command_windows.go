//go:build windows

package seelebridge

import (
	"os/exec"
	"syscall"
)

// configureHiddenCommand prevents a GUI build from flashing a console window
// for each scoped bash invocation.
func configureHiddenCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
