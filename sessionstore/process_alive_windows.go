//go:build windows

package sessionstore

import (
	"errors"
	"syscall"
)

const (
	// processQueryLimitedInformation / stillActive 见 Win32 常量表；Go 的 syscall
	// 包没有导出这两个。
	processQueryLimitedInformation = 0x1000
	stillActive                    = 259
)

// processAlive 判断 pid 是否仍属于某个存活进程。
//
// 不能用 os.FindProcess + Release：它们只做一次 OpenProcess。Windows 的进程对象在
// 「进程已经终结、但还有别的进程握着它的句柄」时依然可以被打开（PID 也仍被占用），
// 于是被强杀的上一份 GUI 会被误判成活持有者，数据根锁永远不会判陈旧
// （2026-09-18 事故：dist/seelex-gui-dev 启动即退出，报 ErrDataRootLocked）。
//
// 正确口径是问内核「这个进程终结了没」——OpenProcess(SYNCHRONIZE) 之后
// WaitForSingleObject(handle, 0)：
//   - WAIT_TIMEOUT → 还在跑；
//   - WAIT_OBJECT_0 → 已终结（句柄/PID 尚未回收也算死）；
//   - OpenProcess 拒绝访问 → 属于别的用户或更高完整性的进程，按「活着」保守处理
//     （与 unix 的 EPERM 一致：宁可拒绝启动，也不抢别人正在写的同一数据根）；
//   - 其余错误（如 pid 不存在）→ 死。
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := syscall.OpenProcess(
		processQueryLimitedInformation|syscall.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return errors.Is(err, syscall.ERROR_ACCESS_DENIED)
	}
	defer syscall.CloseHandle(handle)

	status, _ := syscall.WaitForSingleObject(handle, 0)
	switch status {
	case syscall.WAIT_TIMEOUT:
		return true
	case syscall.WAIT_OBJECT_0:
		return false
	}
	// WAIT_FAILED 等异常：退回退出码判断，只有 STILL_ACTIVE 才算活着。
	var code uint32
	if err := syscall.GetExitCodeProcess(handle, &code); err != nil {
		return true
	}
	return code == stillActive
}
