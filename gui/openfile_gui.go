//go:build gui

package gui

// openfile_gui.go —— 「用系统默认查看器打开原图」的宿主侧入口（含 GUI 构建）。
//
// 三个判据，只有三个：路径非空、文件在、交给平台默认打开器。刻意不在这里再判一次
// "是不是图片"——类型白名单已经在 media.AttachUserImage 判过，第二份判据必然漂移
// （AGENTS.md §8：同一判据只许一处）。

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// openWithSystemViewer 把文件交给系统默认程序打开（图片 → 默认图片查看器）。
func openWithSystemViewer(path string) error {
	target := strings.TrimSpace(path)
	if target == "" {
		return errors.New("打开图片失败：路径为空")
	}
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("打开图片失败：%w", err)
	}
	return launchSystemOpener(target)
}
