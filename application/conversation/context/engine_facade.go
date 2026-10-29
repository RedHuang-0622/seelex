package context

import (
	"github.com/RedHuang-0622/seelex/contract"
	contextruntime "github.com/RedHuang-0622/seelex/kernel/context"
	"github.com/RedHuang-0622/seelex/kernel/state"
)

type (
	EngineCoordinator        = contextruntime.Coordinator
	EngineHistoryCoordinator = contextruntime.HistoryCoordinator
	EngineDeps               = contextruntime.Deps
	EngineCompactResult      = contextruntime.CompactResult
	CompactionIndexPort      = contextruntime.CompactionIndexPort
)

const AutonomousCompactionPrefix = contextruntime.AutonomousCompactionPrefix

func NewEngineCoordinator(deps EngineDeps) *EngineCoordinator {
	return contextruntime.NewCoordinator(deps)
}

func NewEngineHistoryCoordinator(core *state.Core) *EngineHistoryCoordinator {
	return contextruntime.NewHistoryCoordinator(core)
}

func IsProviderOnlyHistoryContent(content string) bool {
	return contextruntime.IsProviderOnlyHistoryContent(content)
}

func IsTaskContextCheckpoint(content string) bool {
	return contextruntime.IsTaskContextCheckpoint(content)
}

func IsActiveSkillContent(content string) bool {
	return contextruntime.IsActiveSkillContent(content)
}

// IsInternalContent 是 visible conversation 的唯一内部行判据。
func IsInternalContent(content string) bool {
	return contextruntime.IsTaskContextCheckpoint(content) ||
		contextruntime.IsProviderOnlyHistoryContent(content) ||
		contextruntime.IsActiveSkillContent(content) ||
		// plan 帧（计划投影 + 执行政策段）是给模型看的内部材料：它是
		// wire_material 的 internal 事件，不是用户说过的话。落进 transcript 是为了
		// 位置稳定（D1），进可见会话则是把内部状态材料冒充成一句用户消息。
		contextruntime.IsActivePlanContext(content)
}

func IsOversizedToolResult(content string, limit int) bool {
	return contextruntime.IsOversizedToolResult(content, limit)
}

func OversizedToolResultWarning(name, resultRef string) string {
	return contextruntime.OversizedToolResultWarning(name, resultRef)
}

func ContentReferenceWarning(resultRef string) string {
	return contextruntime.ContentReferenceWarning(resultRef)
}

func RetainedSystemOnly(history []contract.EngineMessage) []contract.EngineMessage {
	return contextruntime.RetainedSystemOnly(history)
}
