//go:build !windows

package winhide

import "os/exec"

// Apply 在非 Windows 平台为空操作。
func Apply(_ *exec.Cmd) {}
