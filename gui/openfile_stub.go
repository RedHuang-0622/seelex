//go:build !gui

package gui

// openfile_stub.go —— 无桌面宿主的构建里没有平台打开器。
//
// 显式报错而不是静默成功：静默返回 nil 会让前端以为"已经打开了"，用户看到的却
// 是"点了没反应"——把不支持说得像正常，比直接说不支持更坏。

import "errors"

// openWithSystemViewer 在无 GUI 构建里恒失败。
func openWithSystemViewer(string) error {
	return errors.New("打开图片失败：当前构建未包含桌面宿主")
}
