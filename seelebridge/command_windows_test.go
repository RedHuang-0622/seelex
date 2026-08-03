//go:build windows

package seelebridge

import (
	"os/exec"
	"testing"
)

func TestConfigureHiddenCommandHidesWindowsShell(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "exit", "0")
	configureHiddenCommand(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
		t.Fatal("Windows scoped shell must be started with HideWindow")
	}
}
