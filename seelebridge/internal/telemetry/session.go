package telemetry

import (
	"context"

	frameworktelemetry "github.com/RedHuang-0622/Seele/telemetry"
)

// 会话级 trace（thin-wrapper mbd-models.md §1，M1）：
//   - T_i = 会话 i 的 trace 存储（span 集合）；
//   - INV-T1: ∀ i≠j: T_i ∩ T_j = ∅（trace 按会话隔离）；
//   - INV-T2: 查询返回的 trace 只含该会话 span。
//
// MemoryTracer 是全局共享面；隔离通过把会话 ID 以 attribute
// session_id 打在 span/event 上、查询时按 attribute 过滤实现
// （frameworktelemetry.Query.Attributes 精确匹配）。

// SessionAttributeKey 是 span/event 上承载会话 ID 的 attribute 键。
const SessionAttributeKey = "session_id"

// sessionIDKey 是本包上下文中的会话路由键（注入/读取由本包独占；
// 应用层装配时把 runChat ctx 的会话 ID 转写为本键，见 SessionTagHook）。
type sessionIDKey struct{}

// WithSessionID 把会话 ID 注入 ctx（供 SessionTagHook 在钩子边界读取）。
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionIDKey{}, sessionID)
}

// SessionIDFromContext 返回 ctx 中的会话 ID；未注入时返回 ""。
func SessionIDFromContext(ctx context.Context) string {
	sessionID, _ := ctx.Value(sessionIDKey{}).(string)
	return sessionID
}

// SessionTagHook 是会话标签装饰器（Wrapper）：把 ctx 中的会话 ID 打到
// action/invocation/effect 的 attributes 上，透传给下一个钩子；未注入
// 会话 ID 时原样透传（不改变控制流）。链序：H = h_n ∘ … ∘ h_1。
func SessionTagHook(next frameworktelemetry.Hook) frameworktelemetry.Hook {
	if next == nil {
		return nil
	}
	return sessionTagHook{next: next}
}

type sessionTagHook struct {
	next frameworktelemetry.Hook
}

func (hook sessionTagHook) Before(ctx context.Context, action frameworktelemetry.Action) (context.Context, frameworktelemetry.Invocation, error) {
	if sessionID := SessionIDFromContext(ctx); sessionID != "" {
		action.Attributes = withSessionAttribute(action.Attributes, sessionID)
	}
	return hook.next.Before(ctx, action)
}

func (hook sessionTagHook) After(ctx context.Context, invocation frameworktelemetry.Invocation, effect frameworktelemetry.Effect) error {
	if sessionID := SessionIDFromContext(ctx); sessionID != "" {
		effect.Attributes = withSessionAttribute(effect.Attributes, sessionID)
	}
	return hook.next.After(ctx, invocation, effect)
}

func (hook sessionTagHook) OnError(ctx context.Context, name string, err error, attributes frameworktelemetry.Attributes) error {
	if errorHook, ok := hook.next.(frameworktelemetry.ErrorHook); ok {
		if sessionID := SessionIDFromContext(ctx); sessionID != "" {
			attributes = withSessionAttribute(attributes, sessionID)
		}
		return errorHook.OnError(ctx, name, err, attributes)
	}
	return nil
}

func withSessionAttribute(attributes frameworktelemetry.Attributes, sessionID string) frameworktelemetry.Attributes {
	if attributes == nil {
		attributes = frameworktelemetry.Attributes{}
	}
	if _, exists := attributes[SessionAttributeKey]; exists {
		return attributes
	}
	attributes[SessionAttributeKey] = sessionID
	return attributes
}

// SessionTracer 是内存追踪器的会话过滤查询面（INV-T2）：查询只返回
// 目标会话的 span/event/metric/audit；不写共享 tracer。
type SessionTracer struct {
	tracer *frameworktelemetry.MemoryTracer
}

// NewSessionTracer 包装内存追踪器。
func NewSessionTracer(tracer *frameworktelemetry.MemoryTracer) *SessionTracer {
	return &SessionTracer{tracer: tracer}
}

// QueryBySession 返回目标会话的 trace 视图（按 session_id attribute
// 过滤；空会话 ID 返回空视图）。
func (tracer *SessionTracer) QueryBySession(ctx context.Context, sessionID string) (frameworktelemetry.ViewModel, error) {
	if tracer == nil || tracer.tracer == nil || sessionID == "" {
		return frameworktelemetry.ViewModel{}, nil
	}
	return tracer.tracer.Query(ctx, frameworktelemetry.Query{
		Attributes: map[string]string{SessionAttributeKey: sessionID},
	})
}

// QueryBySessionLimit 是带上限的会话过滤查询（trace 满限不溢出；T1.4）。
func (tracer *SessionTracer) QueryBySessionLimit(ctx context.Context, sessionID string, limit int) (frameworktelemetry.ViewModel, error) {
	if limit <= 0 {
		return tracer.QueryBySession(ctx, sessionID)
	}
	if tracer == nil || tracer.tracer == nil || sessionID == "" {
		return frameworktelemetry.ViewModel{}, nil
	}
	return tracer.tracer.Query(ctx, frameworktelemetry.Query{
		Attributes: map[string]string{SessionAttributeKey: sessionID},
		Limit:      limit,
	})
}
