//go:build windows

package security

import (
	"os/exec"
	"syscall"
)

// ConfigureHiddenCommand prevents a GUI build from flashing a console window
// for each scoped bash invocation.
func ConfigureHiddenCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
