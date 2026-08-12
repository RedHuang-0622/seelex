package seelebridge

import (
	seeletelemetry "github.com/RedHuang-0622/Seele/telemetry"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
)

// ── 遥测域兼容别名（trace.go 已迁 seelebridge/internal/telemetry）────────

// NewTracer 返回内存遥测追踪器（internal/telemetry.NewTracer）。
func NewTracer() *seeletelemetry.MemoryTracer {
	return telemetry.NewTracer()
}

// NewLifecycleHook 为内存追踪器创建生命周期钩子（internal/telemetry）。
func NewLifecycleHook(tracer *seeletelemetry.MemoryTracer) (seeletelemetry.Hook, error) {
	return telemetry.NewLifecycleHook(tracer)
}
