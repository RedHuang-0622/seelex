//go:build gui

package gui

import "github.com/wailsapp/wails/v2/pkg/runtime"

// PickDirectory 打开原生目录选择对话框，返回所选路径。
func (bridge *Bridge) PickDirectory() (string, error) {
	return runtime.OpenDirectoryDialog(bridge.requestContext(), runtime.OpenDialogOptions{
		Title: "选择工作区目录",
	})
}
