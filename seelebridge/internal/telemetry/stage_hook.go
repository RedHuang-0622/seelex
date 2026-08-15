package telemetry

import (
	"context"
	"strings"

	frameworktelemetry "github.com/RedHuang-0622/Seele/telemetry"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// StageRecorder 是 node 第一视角阶段日志的记录面（由 session.SubagentSessions
// 实现；接口化避免 internal/telemetry 反向依赖 session 域）。
type StageRecorder interface {
	RecordStage(nodeID string, log model.NodeStageLog)
}

// stagePreviewMax 阶段日志预览的有界长度。
const stagePreviewMax = 200

// StageHook 复用现有 telemetry 边界记录 node 第一视角阶段日志：node 会话的
// ctx 携带 NodeScope，llm/tool 事件在此被投影为同节点的阶段日志；主会话
// （无 NodeScope）与其余事件原样透传。best-effort，绝不改变执行路径。
type StageHook struct {
	next     frameworktelemetry.Hook
	recorder StageRecorder
}

// NewStageHook 构造阶段记录观察面（Wrapper 形态，由 Chain 负责透传）；
// next 为 nil 时降级为透传空实现。
func NewStageHook(recorder StageRecorder) Wrapper {
	return func(next frameworktelemetry.Hook) frameworktelemetry.Hook {
		if next == nil {
			next = noopHook{}
		}
		return &StageHook{next: next, recorder: recorder}
	}
}

// Before 实现 telemetry.Hook：按 NodeScope 投影 turn/tool 阶段日志后透传。
func (hook *StageHook) Before(ctx context.Context, action frameworktelemetry.Action) (context.Context, frameworktelemetry.Invocation, error) {
	if hook.recorder != nil {
		if scope, ok := model.NodeScopeFromContext(ctx); ok && scope.NodeID != "" {
			switch action.Type {
			case frameworktelemetry.EventLLMBefore:
				hook.record(scope.NodeID, model.NodeStageTurn, action)
			case frameworktelemetry.EventToolBefore:
				hook.record(scope.NodeID, model.NodeStageTool, action)
			}
		}
	}
	return hook.next.Before(ctx, action)
}

// After 实现 telemetry.Hook：透传。
func (hook *StageHook) After(ctx context.Context, invocation frameworktelemetry.Invocation, effect frameworktelemetry.Effect) error {
	return hook.next.After(ctx, invocation, effect)
}

func (hook *StageHook) record(nodeID, stage string, action frameworktelemetry.Action) {
	preview := strings.TrimSpace(action.Name)
	if modelAttr, ok := action.Attributes[frameworktelemetry.AttributeGenAIRequestModel]; ok {
		if modelName, ok := modelAttr.(string); ok && modelName != "" {
			preview = strings.TrimSpace(preview + " (" + modelName + ")")
		}
	}
	if len(preview) > stagePreviewMax {
		preview = preview[:stagePreviewMax] + "…"
	}
	hook.recorder.RecordStage(nodeID, model.NodeStageLog{Stage: stage, Preview: preview})
}

// noopHook 是 next 为 nil 时的降级实现。
type noopHook struct{}

func (noopHook) Before(ctx context.Context, action frameworktelemetry.Action) (context.Context, frameworktelemetry.Invocation, error) {
	return ctx, frameworktelemetry.Invocation{}, nil
}

func (noopHook) After(context.Context, frameworktelemetry.Invocation, frameworktelemetry.Effect) error {
	return nil
}
