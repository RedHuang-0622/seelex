//go:build !gui

package gui

import "errors"

// PickDirectory stub：非 GUI 构建不支持原生对话框。
func (bridge *Bridge) PickDirectory() (string, error) {
	return "", errors.New("目录选择仅支持桌面 GUI 模式，请使用 TUI 或指定路径参数")
}
