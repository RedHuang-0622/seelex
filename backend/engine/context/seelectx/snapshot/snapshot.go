// Package snapshot 保留会话上下文快照的既有名字与入口；形状的唯一事实源
// 已下沉 `contract/dto`（P3：契约层不依赖引擎上下文包）。
//
// 这里只做同身份别名：类型、Builder 方法、Validate 与 Truncate 全部来自 dto，
// 调用点与类型身份零改动。
package snapshot

import "github.com/RedHuang-0622/seelex/contract/dto"

type (
	ContextSnapshot = dto.ContextSnapshot
	Decision        = dto.Decision
	EscapeInfo      = dto.EscapeInfo
	ValidationError = dto.ValidationError
)

// Truncate 是 dto.Truncate 的函数值别名（既有调用点零改动）。
var Truncate = dto.Truncate
