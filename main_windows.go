//go:build windows

package main

import (
	"syscall"
)

const stillActive = 259

func processExists(pid int) bool {
	h, err := syscall.OpenProcess(0x1000, false, uint32(pid)) // PROCESS_QUERY_LIMITED_INFORMATION
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var code uint32
	err = syscall.GetExitCodeProcess(h, &code)
	if err != nil {
		return false
	}
	return code == stillActive
}
