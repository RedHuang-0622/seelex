package assemble

// review_files.go 是 **show_files 工具注册面的 D1 兼容入口**。
//
// 工具形状、唯一注册 helper 与落点端口都在
// [`backend/tool/review`](../tool/review/show_files.go)；生产装配已改由
// wiring/registry 直接调用 domain helper。这里保留旧签名，正文只做一行转发。
//
// 删除里程碑：BM-007（旧 assemble Runtime 路径退场时）。

import (
	"github.com/RedHuang-0622/seelex/backend/tool/review"
)

// ReviewFilesPort 是落点端口的**同身份别名**（既有调用方与用例零改动）。
type ReviewFilesPort = review.ShowFilesPort

var _ review.ToolRegistrar = (*Runtime)(nil)

// RegisterReviewFilesTool 把 show_files 注册进 Runtime 工具面（形状见域包）。
func RegisterReviewFilesTool(runtime *Runtime, port ReviewFilesPort) {
	if runtime == nil || port == nil {
		return
	}
	review.RegisterShowFilesTool(runtime, port)
}
