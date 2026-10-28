package obs

// event_correlation.go 是**事件体系双轨的关联面**（P21-9「域方法随类型走」：从
// `backend/engine/assemble/events.go` 归位到观测域，与 `unified_events.go` 同属"事件怎么落库、
// 怎么关联"这一族；plan §2.5 把 `seelebridge/events.go` 列为 `backend/engine/obs/` 的目标）。
//
// 同一运行有两类事实，分别落两套存储/命名：
//
//  1. 执行事实轨：frameworkevent.Sink（子代理/作业事件）→ store 事件库
//     （`store/event_store.go`）。关联字段：
//     `event.Locations["agent.runtime"]["session_id"]` + `event.Scope{PlanID, RunID, NodeID}`。
//     框架 runner 事件缺失 session_id 时 EventStore 跳过持久化（best-effort），
//     判据见 `store.SessionIDFromEvent`。
//  2. 会话级 llm/tool 意图-效果轨：telemetry.Hook 装饰链（LifecycleHook → DiagnosticHook →
//     StageHook，`assemble.NewRuntime` 装配）→ 内存 tracer。关联字段：sessionID（tracer 事件面）。
//
// 短期收敛（本文件）：`CorrelateMainSessionID` 保证事实轨事件在持久化前补上主会话 session_id——
// Seelex 侧自补，无需 Seele 上游发版。
// 长期形态（已实施）：B 类遥测以有界脱敏摘要（`SummaryEventSource`）与 A 类事实写入同一 store
// 事件库（存储不双写），统一查询按 sessionID/nodeID 关联两轨（`UnifiedEvents`）。形态与取舍见
// docs/2026-08-14-decoupling/06-unified-event-store-decision.md。

import (
	"context"

	frameworkevent "github.com/RedHuang-0622/Seele/event"

	storesession "github.com/RedHuang-0622/seelex/store/session"
)

// CorrelateMainSessionID 包装持久化钩子：事件缺失 session_id 关联时补上当前主会话
// （agent.runtime Location）。框架 runner 事件本身不携带 session_id，不补则 store 事件库
// 跳过落库（见 store.SessionIDFromEvent）。
//
// 两格入参都是函数值：主会话读数由宿主闭包给（会话可能尚未绑定），下游钩子原样转调。
// mainSessionID 或 next 为空时**原样返回 next**（没有可补的东西就不包装，绝不吞掉下游钩子）。
func CorrelateMainSessionID(
	mainSessionID func() string,
	next func(context.Context, frameworkevent.Event) error,
) func(context.Context, frameworkevent.Event) error {
	if mainSessionID == nil || next == nil {
		return next
	}
	return func(ctx context.Context, event frameworkevent.Event) error {
		if storesession.SessionIDFromEvent(event) == "" {
			if sessionID := mainSessionID(); sessionID != "" {
				event.Locations = append(event.Locations, frameworkevent.Location{
					Kind: "agent.runtime",
					IDs:  map[string]string{"session_id": sessionID},
				})
			}
		}
		return next(ctx, event)
	}
}
