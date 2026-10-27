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
