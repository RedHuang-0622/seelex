//go:build gui && !windows

package gui

// openfile_unix.go —— macOS / Linux 的默认打开器：`open` / `xdg-open`。
//
// 与 Windows 分支同一条语义（交给系统默认应用、不等待），只是命令不同；平台差异
// 只出现在这个装配点上，调用方（openWithSystemViewer）不判平台。

import (
	"fmt"
	"os/exec"
	"runtime"
)

// launchSystemOpener 交给平台的默认打开器（用户配置的图片查看器）。
func launchSystemOpener(path string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	command := exec.Command(opener, path)
	if err := command.Start(); err != nil {
		return fmt.Errorf("打开图片失败：%w", err)
	}
	go func() { _ = command.Wait() }()
	return nil
}
