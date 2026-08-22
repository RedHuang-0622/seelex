// Package context_runtime owns provider context assembly and control:
// token budgeting, compaction, result-refs, provider-cache normalization and
// recoverable provider interruption. The Coordinator reads task/session/
// prompt/view capabilities only through consumer-declared ports injected by
// the composition root.
package context_runtime

import (
	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/model"
)

// TaskPort 是 context 域对 task 域的窄端口。
type TaskPort interface {
	ActivePlanProjectionLocked() *model.ActivePlanProjection
	BuildTaskCheckpointLocked(*task_context.TaskExecutionState) model.TaskCheckpoint
	RecordContextCompactionLocked(string, model.ContextCompaction) bool
	StoreToolResultLocked(string, string) model.StoredToolResult
	CountTranscriptEvent(model.TranscriptEvent) int
	CurrentTaskExecution() *task_context.TaskExecutionState
	Transcript() []model.TranscriptEvent
	RememberCheckpointLocked(model.TaskCheckpoint)
	ResultRefsByCallID() map[string]string
	TokenCounterName() string
	CountRequestTokens(string, []contract.EngineMessage, string, []model.Tool) int
	CountTextTokens(string) int
	PlanStack() []model.SessionPlanFrame
	ActivePlanID() string
	RecordContextControlFailure(requestID string, err error)
	TakeContextControlFailure(requestID string) error
}

// SessionPort 是 context 域对会话域的窄端口（压缩 checkpoint 落盘）。
type SessionPort interface {
	PersistCurrentSession(string) error
}

// PromptPort 是 context 域对 prompt 域的窄端口。
type PromptPort interface {
	SystemPromptForActiveTaskLocked() string
}

// ViewPort 是 Snapshot revision bump 窄接口。
type ViewPort interface {
	BumpLocked() uint64
}

// HistoryPort 是 provider 缓存归一化窄接口。
type HistoryPort interface {
	PrepareProviderHistory() error
}

// Deps 是 context_runtime 的装配输入。
type Deps struct {
	Core     *state.Core
	Tasks    TaskPort
	Sessions SessionPort
	Prompts  PromptPort
	View     ViewPort
	History  HistoryPort
	// WorkTableTraceBlock 返回打点表标记块（work_table 域；请求尾部只读
	// 注入）。
	WorkTableTraceBlock func() string
}
