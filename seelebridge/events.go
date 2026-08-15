// 事件体系双轨与关联字段（解耦方案 §02.3 / §04.7 的短期收敛）。
//
// 同一运行有两类事实，分别落两套存储/命名：
//
//  1. workplan 执行事实轨：frameworkevent.Sink
//     （seelebridge/plan/events.go 的 EventSink，planExecutor 持有）→
//     sessionstore 事件库（sessionstore/event_store.go）。
//     关联字段：event.Locations["agent.runtime"]["session_id"] +
//     event.Scope{PlanID, RunID, NodeID}。框架 runner 事件缺失 session_id
//     时 EventStore 跳过持久化（best-effort），见 sessionIDFromEvent。
//  2. 会话级 llm/tool 意图-效果轨：telemetry.Hook 装饰链
//     （LifecycleHook → DiagnosticHook → StageHook，runtime.go NewRuntime
//     装配）→ 内存 tracer。关联字段：sessionID（tracer 事件面）。
//
// 短期收敛（本文件）：收拢两个订阅接线的关联说明，并保证事实轨事件在
// 持久化前补上主会话 session_id——seelex 侧自补，无需 Seele 上游发版。
// 长期形态（已实施，见 events_unified.go）：B 类遥测以有界脱敏摘要
// （Source=seelex.telemetry.summary）与 A 类事实写入同一 sessionstore
// 事件库（存储不双写），统一查询按 sessionID/nodeID 关联两轨
// （Runtime.UnifiedEvents）。形态与取舍见
// docs/2026-08-14-decoupling/06-unified-event-store-decision.md。
package seelebridge

import (
	"context"

	frameworkevent "github.com/RedHuang-0622/Seele/event"
)

// eventSessionID 从事件的 agent.runtime Location 提取会话标识
// （与 sessionstore.sessionIDFromEvent 同一投影语义，避免跨包依赖）。
func eventSessionID(event frameworkevent.Event) string {
	for _, location := range event.Locations {
		if location.Kind != "agent.runtime" {
			continue
		}
		if sessionID := location.IDs["session_id"]; sessionID != "" {
			return sessionID
		}
	}
	return ""
}

// correlateMainSessionID 包装持久化钩子：事件缺失 session_id 关联时补上
// 当前主会话（agent.runtime Location）。框架 runner 事件本身不携带
// session_id，不补则 sessionstore 事件库跳过落库。
func correlateMainSessionID(
	mainSessionID func() string,
	next func(context.Context, frameworkevent.Event) error,
) func(context.Context, frameworkevent.Event) error {
	if mainSessionID == nil || next == nil {
		return next
	}
	return func(ctx context.Context, event frameworkevent.Event) error {
		if eventSessionID(event) == "" {
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
