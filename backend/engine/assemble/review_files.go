package assemble

// review_files.go 是 **show_files 工具的宿主注册面**。
//
// 落点沿革（P21-4，plan §2.5 的 `review_files` → `backend/tool/review`）：工具**形状**
// （名字 / 描述 / schema / 参数校验 / 返回值）搬进域包
// [`backend/tool/review/show_files.go`](../tool/review/show_files.go)；这里只留
// "把域包给的三件套 + handler 注册进工具面"这一句装配（与 `RegisterTaskTerminalTools` 同款形状）。
//
// 分工不变：工具面（模型看得见的那一层）在域包，**落点**（会话快照与沙盒边界）由宿主实现
// `ShowFilesPort` 注入——装配点（`wiring/registry`）传 `app`，逐份过 workspace 域裁决。

import (
	"github.com/RedHuang-0622/seelex/backend/tool/review"
)

// ReviewFilesPort 是落点端口的**同身份别名**（既有调用方与用例零改动）。
type ReviewFilesPort = review.ShowFilesPort

// RegisterReviewFilesTool 把 show_files 注册进 Runtime 工具面（形状见域包）。
func RegisterReviewFilesTool(runtime *Runtime, port ReviewFilesPort) {
	if runtime == nil || port == nil {
		return
	}
	runtime.RegisterTool(
		review.ShowFilesToolName,
		review.ShowFilesDescription(),
		review.ShowFilesSchema(),
		review.ShowFilesHandler(port),
	)
}
