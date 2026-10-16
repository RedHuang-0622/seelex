package core

import (
	view "github.com/RedHuang-0622/seelex/middle/view"
)

// RenderDiag 构建诊断文本。由 /diag 命令调用。
//
// 实现位于中间层读数面 `middle/view`（plan §2.4：`diagnostics.go` →
// `middle/view/`）；这里只保留兼容缝，使 `/diag` 命令与门面签名不变。
func RenderDiag(snap Snapshot) string {
	return view.RenderDiag(snap)
}
