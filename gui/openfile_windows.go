//go:build gui && windows

package gui

// openfile_windows.go —— Windows 的默认打开器。
//
// `rundll32 url.dll,FileProtocolHandler <path>` 就是资源管理器里双击同一套路由：
// 由注册表里该扩展名的默认程序接管。Start 而不等：查看器是独立窗口，宿主不等它关。

import (
	"fmt"
	"os/exec"
)

// launchSystemOpener 交给 Windows 的文件协议处理器（用户的默认图片查看器）。
func launchSystemOpener(path string) error {
	command := exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	if err := command.Start(); err != nil {
		return fmt.Errorf("打开图片失败：%w", err)
	}
	// Start 之后仍要回收子进程句柄，否则每点一次都留一个僵尸。
	go func() { _ = command.Wait() }()
	return nil
}
