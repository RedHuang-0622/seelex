package assemble

// events_unified.go 是**统一事件库**的宿主转发面。
//
// 落点沿革（P21-6，plan §2.5 的 `events_unified.go` → `backend/engine/obs/`）：B 类摘要日志
// （`SummaryLog`）与统一事件查询（`UnifiedEventReader` / `UnifiedEventView` / 两个读法 /
// 话题映射 / 过滤）都搬进观测域
// [`backend/engine/obs/unified_events.go`](../obs/unified_events.go)；这里只留**同身份别名**
// （既有调用方与用例零改动）与两个公开读法的薄转发。
//
// 宿主读数只有两处，仍留在本层：store 事件库路由（`HistoryRouter`）与内存 tracer 的查询口
// （以函数值端口交给域包，域包因此不必认识 tracer 的具体类型）。

import (
	"context"
	"fmt"

	frameworktelemetry "github.com/RedHuang-0622/Seele/telemetry"

	"github.com/RedHuang-0622/seelex/backend/engine/obs"
)

// 同身份别名（调用方与既有用例零改动）。
type (
	SummaryLog         = obs.SummaryLog
	UnifiedEventView   = obs.UnifiedEventView
	UnifiedEventReader = obs.UnifiedEventReader
)

// NewSummaryLog 创建摘要统一日志（域包实现）。
func NewSummaryLog() *SummaryLog { return obs.NewSummaryLog() }

// unifiedEventsRouterMissing 与域包的唯一文案处同值（错误值身份不变）。
var errUnifiedEventsRouterMissing = obs.ErrUnifiedEventsRouterMissing

// unifiedEvents 装配一次查询的域句柄：Router 与 Live 都在调用时取读数，
// 保持"Router 可能后装配"与"tracer 可能未接线"的既有语义。
func (r *Runtime) unifiedEvents() *obs.UnifiedEvents {
	return obs.NewUnifiedEvents(obs.UnifiedEventsDeps{
		Load:      r.loadUnifiedEvents,
		LoadRange: r.loadUnifiedEventRange,
		Live: func(ctx context.Context, limit int) (frameworktelemetry.ViewModel, error) {
			if r.tracer == nil {
				return frameworktelemetry.ViewModel{}, fmt.Errorf("unified events: tracer is unavailable")
			}
			return r.tracer.Query(ctx, frameworktelemetry.Query{Limit: limit})
		},
	})
}

// UnifiedEvents 按 sessionID/nodeID 查询统一事件视图（域包 `UnifiedEvents.Query`）。
func (r *Runtime) UnifiedEvents(ctx context.Context, sessionID, nodeID string, limit int) (UnifiedEventView, error) {
	if r == nil {
		return UnifiedEventView{}, fmt.Errorf("unified events: runtime is nil")
	}
	return r.unifiedEvents().Query(ctx, sessionID, nodeID, limit)
}

// UnifiedEventsRange 按 Seq 区间查询统一事件视图（域包 `UnifiedEvents.QueryRange`）。
func (r *Runtime) UnifiedEventsRange(ctx context.Context, sessionID, nodeID string, fromSeq, toSeq uint64, limit int) (UnifiedEventView, error) {
	if r == nil {
		return UnifiedEventView{}, fmt.Errorf("unified events: runtime is nil")
	}
	return r.unifiedEvents().QueryRange(ctx, sessionID, nodeID, fromSeq, toSeq, limit)
}
