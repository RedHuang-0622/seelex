//go:build windows

package security

import (
	"os/exec"
	"testing"
)

func TestConfigureHiddenCommandHidesWindowsShell(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "exit", "0")
	ConfigureHiddenCommand(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
		t.Fatal("Windows scoped shell must be started with HideWindow")
	}
}
