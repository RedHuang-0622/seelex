//go:build !windows

package seelebridge

import "os/exec"

func configureHiddenCommand(_ *exec.Cmd) {}
