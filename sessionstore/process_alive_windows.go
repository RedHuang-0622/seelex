//go:build windows

package sessionstore

import "os"

// processAlive 判断 pid 是否仍属于某个进程。
//
// Windows 没有 signal 0 探测：os.FindProcess 内部 OpenProcess 只在 pid 已退出
// 时返回错误（见 Go 的 os/exec_windows.go findProcess），因此「查得到」即视为
// 存活。pid 复用带来的误判由 owner 记录里的 renewed_at 陈旧判定兜住。
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	release := process.Release
	return release() == nil
}
