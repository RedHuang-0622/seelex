// Package context_runtime owns provider context assembly and control:
// token budgeting, compaction, result-refs, provider-cache normalization and
// recoverable provider interruption. The Coordinator reads task/session/
// prompt/view capabilities only through consumer-declared ports injected by
// the composition root.
package context_runtime

import (
	"context"
	"errors"

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

// HistoryPort 是 provider 缓存归一化窄接口（sessionID 指明目标会话）。
type HistoryPort interface {
	PrepareProviderHistoryFor(sessionID string) error
}

// CompactionIndexRequest 是一次「把装配层折叠产出的帧推进会话压缩栈」的入参：
// 全部是折叠那一刻已经在手的事实，不含任何需要接收方重算的量。
//
// 为什么区间只给 EventSeq / MessageID 而不给单元下标：装配层手里的权威事实就是
// EventSeq（TranscriptPrefixRange 的产物），而"单元下标"属于持久化事件流的空间
// （sessionstore.CompleteEventUnits 会跳过孤儿 tool 与未知角色，因此两者不是减法
// 关系）。让接收方或检索侧按 Seq 反查，才不会出现第二个单元空间生产者
// （见 seelexctx/search 的 eventSeqUnitRange）。
type CompactionIndexRequest struct {
	// Overflow 是被折出保留窗口的完整协议单元原文（wire 素材，按序）。
	Overflow []contract.EngineMessage
	// UnitCount 是 Overflow 里的完整协议单元数（0 → 由接收方切分推导）。
	UnitCount int
	// ReplayHistory 是**上一次真实请求的历史字节**（前缀重放素材）。它与
	// SystemPrompt/Tools 一起决定重放请求能否命中 provider 前缀缓存；必须来自
	// 产出该请求的同一条装配路径，不能从事件流重拼（seelexctx/replay.go 的
	// 字节级一致性契约）。空 → 接收方走本地确定性折叠，不消耗模型 token。
	ReplayHistory []contract.EngineMessage
	// EventFrom/EventTo 是被折区间的 transcript 事件序号（含端点；0 = 无边界可记）。
	EventFrom uint64
	EventTo   uint64
	// MessageFrom/MessageTo 是被折区间的 UI 消息号（可空）。
	MessageFrom string
	MessageTo   string
	// RequestID 是触发这次折叠的回合标识（可空；冷加载维护身份也带前缀标识）。
	RequestID string
}

// CompactionIndexReceipt 是推帧的回执。
type CompactionIndexReceipt struct {
	// SegmentID 是栈帧标识（read_compressed_turn 的必选入参、快照记录与栈帧的
	// 互链键）。空 = 没有推成帧。
	SegmentID string
	// Summary 是栈帧的两章节摘要正文（Markdown 读后感）。帧正文**原样嵌入**它，
	// 绝不另写一份——两份措辞会漂移，而栈帧才是权威。
	Summary string
	// SummarySource 是摘要来源：replay（前缀重放厚摘要）| local（本地确定性折叠）。
	// 它是"重放到底成没成"的唯一证据：两次尝试都失败会静默落回 local。
	SummarySource string
}

// ErrCompactionIndexUnavailable 表示索引面不可用（Runtime 缺失或未装配压缩栈）。
// 它是**降级**信号而非故障：调用方据此把 SegmentID 留空、门禁 Detail 记明原因，
// 折叠与请求照常。
var ErrCompactionIndexUnavailable = errors.New("context_runtime: compaction index is unavailable")

// CompactionIndexPort 是「折叠帧进会话压缩栈」的**窄可选**能力。
//
// 刻意做成可选（调用方类型断言探测，照 task_context.ContextBudgetFor 对
// contextLimitProvider 的既有姿势），而不是加进 contract.RuntimePort：加进大接口
// 会迫使每一个 fake/harness 都实现它，而断言失败时的正确行为本来就是"不索引"——
// 折叠照样成立、请求照样发出，只是少了检索入口。降级方向天然安全，因此不必
// 把它变成所有实现方的义务。
//
// 实现方必须自己处理压缩栈的链锚点校验（非首帧的 PrevSegmentID / PrevRequestFrom /
// PrevRequestTo 必须与栈顶逐一相等，见 sessionstore.PushCompact），调用方不代劳。
type CompactionIndexPort interface {
	// PushCompactionFrame 生成并压入一帧，返回回执。error 时调用方只记日志、
	// 不中断装配：索引缺失是降级，不是错误。
	PushCompactionFrame(ctx context.Context, sessionID string, request CompactionIndexRequest) (CompactionIndexReceipt, error)
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
