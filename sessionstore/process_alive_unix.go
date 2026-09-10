//go:build !windows

package sessionstore

import (
	"errors"
	"os"
	"syscall"
)

// processAlive 用 signal 0 探测 pid。EPERM 说明进程存在但不属于当前用户，
// 同样按存活处理（否则会把别人的活跃会话数据根误判为陈旧锁）。
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	defer process.Release()
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
