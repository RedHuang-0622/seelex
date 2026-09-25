// Package context_runtime owns provider context assembly and control:
// token budgeting, compaction, result-refs, provider-cache normalization and
// recoverable provider interruption. The Coordinator reads task/session/
// prompt/view capabilities only through consumer-declared ports injected by
// the composition root.
package context_runtime

import (
	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/model"
)

// TaskPort 是 context 域对 task 域的窄端口。
type TaskPort interface {
	ActivePlanProjectionLocked() *model.ActivePlanProjection
	ActivePlanProjectionLockedFor(sessionID string) *model.ActivePlanProjection
	BuildTaskCheckpointLocked(*task_context.TaskExecutionState) model.TaskCheckpoint
	RecordContextCompactionLocked(string, model.ContextCompaction) bool
	StoreToolResultLocked(string, string) model.StoredToolResult
	StoreToolResultForLocked(string, string, string) model.StoredToolResult
	CountTranscriptEvent(model.TranscriptEvent) int
	CurrentTaskExecution() *task_context.TaskExecutionState
	CurrentTaskExecutionFor(sessionID string) *task_context.TaskExecutionState
	// BeginSessionContextMaintenanceLocked 为没有在飞回合的会话（冷加载、刚
	// 清空）打开会话级上下文维护身份，返回该身份；空串 = 没拿到（已有在飞
	// 回合），调用方必须按既有纪元路径处理。调用方持有 Core.ViewMu。
	BeginSessionContextMaintenanceLocked(sessionID string) string
	// EndSessionContextMaintenanceLocked 撤销维护身份，保留压缩产生的上下文
	// 状态（版本/压缩记录/checkpoint）。调用方持有 Core.ViewMu。
	EndSessionContextMaintenanceLocked(sessionID, requestID string)
	Transcript() []model.TranscriptEvent
	TranscriptFor(sessionID string) []model.TranscriptEvent
	RememberCheckpointLocked(model.TaskCheckpoint)
	ResultRefsByCallID() map[string]string
	ResultRefsByCallIDFor(sessionID string) map[string]string
	TokenCounterName() string
	CountRequestTokens(string, []contract.EngineMessage, string, []model.Tool) int
	CountTextTokens(string) int
	PlanStack() []model.SessionPlanFrame
	PlanStackFor(sessionID string) []model.SessionPlanFrame
	ActivePlanID() string
	ActivePlanIDFor(sessionID string) string
	SessionIDForRequest(requestID string) string
	RecordContextControlFailure(requestID string, err error)
	TakeContextControlFailure(requestID string) error
}

// SessionPort 是 context 域对会话域的窄端口（压缩 checkpoint 落盘）。
type SessionPort interface {
	// PersistCurrentSession 在显式 location（workspace 键）下持久化指定
	// 会话（阶段 0：压缩 checkpoint 落盘按会话键，不依赖全局写作用域）。
	PersistCurrentSession(session_runtime.Location, string) error
}

// PromptPort 是 context 域对 prompt 域的窄端口。
type PromptPort interface {
	SystemPromptForActiveTaskLocked() string
	// SystemPromptForActiveTaskLockedFor 返回指定会话活跃任务 system prompt
	// （多会话并行）。
	SystemPromptForActiveTaskLockedFor(sessionID string) string
}

// ViewPort 是 Snapshot revision bump 窄接口。
type ViewPort interface {
	BumpLocked() uint64
}

// HistoryPort 是 provider 缓存归一化窄接口（sessionID 指明目标会话；inLoop 是
// 可选的环内历史通道，nil = 锁外路径）。
type HistoryPort interface {
	PrepareProviderHistoryFor(sessionID string, inLoop *loopHistoryChannel) error
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
	// 注入）。sessionID 指明正在组装执行上下文的会话：打点必须取自该会话
	// 自己的 task scope，多会话并行时后台会话不得看到活跃会话的打点。
	WorkTableTraceBlock func(sessionID string) string
}
