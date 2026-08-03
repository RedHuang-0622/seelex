package seelebridge

import "github.com/RedHuang-0622/Seele/telemetry"

// NewTracer 返回内存遥测追踪器（telemetry.NewMemoryTracer）。
// 会话生命周期事件（llm/tool intent-effect）经 NewLifecycleHook 写入；
// GUI/TUI 的 trace 视图经 tracer.Query 读取（trace 视图 API 语义保持，
// 见 docs/arch/seele-v2-runtime-architecture.md §遥测）。
func NewTracer() *telemetry.MemoryTracer {
	return telemetry.NewMemoryTracer()
}

// NewLifecycleHook 为内存追踪器创建生命周期钩子
// （llm/tool intent-effect 事件，见 Seele telemetry README）。
// 钩子失败默认隔离（best-effort），不中断被插桩的执行路径。
func NewLifecycleHook(tracer *telemetry.MemoryTracer) (telemetry.Hook, error) {
	return telemetry.NewLifecycleHook(tracer)
}
