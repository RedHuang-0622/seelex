//go:build !windows

package security

import "os/exec"

// ConfigureHiddenCommand is a no-op on non-Windows platforms.
func ConfigureHiddenCommand(_ *exec.Cmd) {}
