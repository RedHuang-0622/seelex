package assemble

// runtime_visibility.go 是 **actor 消息边界**（应用 → Runtime 单向发布）的两个投影与它们的
// 写入口（R11-S6，契约 §2.3：`ports.go` 归零片的最后一段）。
//
// 两个别名是跨层契约的形状（`contract/dto` 与 `internal/model`），调用方（GUI/TUI 桥、
// teammate 路径）用它们构造消息；两个 setter 只做无返回的发布，没有反向回调。

import (
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/model"
)

// RuntimeVisibilityProjection is an immutable application-to-runtime message.
type RuntimeVisibilityProjection = dto.RuntimeVisibilityProjection

// ParentEvidenceProjection is the minimal application-owned data needed for a
// Runtime-local parent evidence snapshot. Runtime adds its own telemetry and
// stores the resulting immutable snapshot for subagents to read.
type ParentEvidenceProjection = model.ParentEvidenceProjection

// SetRuntimeVisibilityProjection publishes a value copy from Application. It
// has no synchronous reverse callback and is safe from Runtime tool hooks.
func (r *Runtime) SetRuntimeVisibilityProjection(projection RuntimeVisibilityProjection) {
	if r == nil {
		return
	}
	copy := projection
	r.visibilityProjection.Store(&copy)
}

// SetParentEvidenceProjection turns the application projection into a
// Runtime-owned immutable snapshot. A blank session clears stale evidence.
func (r *Runtime) SetParentEvidenceProjection(projection ParentEvidenceProjection) {
	if r == nil {
		return
	}
	r.subagentContext.SetParentEvidenceProjection(projection)
}
