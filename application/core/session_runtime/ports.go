// Package session_runtime owns the session domain: durable session records,
// catalog discovery, project binding and storage policy. The Coordinator
// reads task/plan authoritative state only through TaskPersistencePort
// (consumer-declared interface, injected by the composition root), so it
// never reaches into other domain implementations.
package session_runtime

import (
	"context"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// TaskPersistencePort 是会话持久化对 task/plan 权威状态的读写面。
// 全部方法显式携带 sessionID（For 变体）：后台会话收尾不得读活跃会话槽，
// 违反即编译失败（阶段 0 契约，对应 plan.md P2/P4/R4）。
// Locked 后缀方法要求调用方已持有 state.Core.Mu（内核锁内调用）。
type TaskPersistencePort interface {
	TaskProjectionLocked(sessionID string) *model.TaskContextProjection
	TranscriptFor(sessionID string) []model.TranscriptEvent
	PendingToolResultsFor(sessionID string) []model.StoredToolResult
	TaskCheckpointsFor(sessionID string) []model.TaskCheckpoint
	ToolResultRefsFor(sessionID string) []model.ToolResultRef
	ToolResultRefByCallIDFor(sessionID, callID string) string
	ContinuationSummaryFor(sessionID, requestID string) string
	CurrentRequestIDFor(sessionID string) string
	TaskStateFor(sessionID string) *model.TaskState
	ActivePlanIDFor(sessionID string) string
	PlanStackFor(sessionID string) []model.SessionPlanFrame
	SyncActivePlanFrameLockedFor(sessionID string, now time.Time)
	RemoveCommittedToolResultsForLocked(sessionID string, committed []model.StoredToolResult)
}

// SessionRecordPort 是会话归档的持久化面（可选能力断言：会话端口实现
// 该接口时，record 按 workspace/session 唯一键原子落盘）。
type SessionRecordPort interface {
	SaveSessionRecord(string, model.SessionRecord) error
	LoadSessionRecord(string) (model.SessionRecord, error)
	LoadSessionRecordWorkspace(string, string) (model.SessionRecord, error)
	// SaveSessionRecordWorkspace 在显式项目作用域下写会话 record
	// （后台会话落盘不依赖全局 Router 写作用域；阶段 0 键漂移修复）。
	SaveSessionRecordWorkspace(string, string, model.SessionRecord) error
}

// SessionSnapshotPort 是会话原子快照写入面（可选能力断言：record +
// transcript + tool-result 一次性提交）。
type SessionSnapshotPort interface {
	SaveSessionSnapshot(string, []contract.EngineMessage, model.SessionRecord, []model.TranscriptEvent, []model.StoredToolResult) error
	// SaveSessionSnapshotWorkspace 在显式项目作用域下原子写入会话快照
	// （后台会话落盘用；不改变 Router active write scope）。
	SaveSessionSnapshotWorkspace(string, string, []contract.EngineMessage, model.SessionRecord, []model.TranscriptEvent, []model.StoredToolResult) error
}

// SessionTranscriptPort 是 transcript 尾部与工具结果读回面（可选能力断言）。
type SessionTranscriptPort interface {
	LoadTranscriptTailWorkspace(string, string, int, int) ([]model.TranscriptEvent, error)
	LoadToolResultWorkspace(string, string, string) (model.StoredToolResult, error)
}

// SessionConversationRangePort 是可见会话分页读回面（可选能力断言）。
type SessionConversationRangePort interface {
	LoadConversationRangeWorkspace(string, string, int, int) ([]model.Message, int, error)
}

// SessionContextPort 是会话 context 模块（system prompt + Plan/Task/Skill/
// Compact 四栈）的装配端口：resume 时加载并挂接 Runtime，离开会话时解绑，
// 防止跨会话串栈。
type SessionContextPort interface {
	AttachSessionContext(workspaceID, sessionID string) error
	DetachSessionContext()
}

// SessionForkPort 是会话 fork 的持久化面（可选能力断言：实现后会话 fork
// 才能落盘；未实现时 ForkSession 返回明确错误）。全部方法走显式项目作用域，
// 不改变 Router 的 active write scope。
type SessionForkPort interface {
	// LoadEventRangeWorkspace 按 EventSeq 范围（含端点）读取事件流
	// （fork 切断点解析/段落边界校验用）。
	LoadEventRangeWorkspace(projectID, sessionID string, fromSeq, toSeq uint64) ([]sessionstore.Event, error)
	// LoadToolResultsWorkspace 枚举父会话 tool-results 通道全部结果
	// （fork 深拷贝物理复制用）。
	LoadToolResultsWorkspace(projectID, sessionID string) ([]sessionstore.ToolResult, error)
	// SaveSessionSnapshotWorkspace 在显式项目作用域下原子写入子会话快照
	// （截断后的 record + events + tool-results 物理复制）。
	SaveSessionSnapshotWorkspace(projectID, sessionID string, providerHistory []contract.EngineMessage, record model.SessionRecord, events []model.TranscriptEvent, results []model.StoredToolResult) error
	// LoadContextStateWorkspace / SaveContextStateWorkspace 读写会话 context
	// 模块（四栈深拷贝写入子会话）。
	LoadContextStateWorkspace(projectID, sessionID string) ([]byte, error)
	SaveContextStateWorkspace(projectID, sessionID string, state []byte) error
	// CurrentGenerationWorkspace 返回会话当前已发布 generation（血缘
	// forked_from_generation 来源）。
	CurrentGenerationWorkspace(projectID, sessionID string) (string, error)
}

// SessionGranularPort 是会话粒度读取面：原子单位 = session，调用方只传
// sessionID/项目 ID，不再以 workspace 粒度口读写。实现方为
// sessionstore.SessionGranularStore 的适配器（装配根注入）。
type SessionGranularPort interface {
	// SessionsOf 按项目索引枚举会话（project = 会话集合）。
	SessionsOf(projectID string) []model.SessionInfo
	// LoadHistory 读取会话 provider 历史。
	LoadHistory(sessionID string) ([]contract.EngineMessage, error)
	// LoadHistoryRange 按窗口读取会话历史。
	LoadHistoryRange(sessionID string, offset, limit int) ([]contract.EngineMessage, int, error)
	// Delete 删除会话（record/history/transcript/toolresults/context 同键）。
	Delete(sessionID string) error
}

// SessionStoragePort 是会话存储设置面（可选能力断言）。
type SessionStoragePort interface {
	StorageConfig() (sessionstore.Config, error)
	TestStorage(context.Context, sessionstore.Config) error
	ConfigureStorage(context.Context, sessionstore.Config) error
}

// ViewPort 是 Snapshot revision bump 的消费方窄接口（实现：view 域
// coordinator，装配根注入）。
type ViewPort interface {
	BumpLocked() uint64
}

// Deps 是 session_runtime 的装配输入。Core 是共享状态内核；Tasks 是
// task/plan 权威状态端口；其余为跨域纯逻辑端口，由装配根注入，避免
// 本包反向依赖 core 根包。
type Deps struct {
	Core  *state.Core
	Tasks TaskPersistencePort
	// View 用于会话目录刷新时的 Snapshot revision bump（锁内调用）。
	View ViewPort
	// Closed 返回应用是否已进入关闭状态；调用方持 Core.Mu 时读取
	// （与 Shutdown 写 closed 同锁，避免竞态）。
	Closed func() bool
	// TranscriptTailBudget 返回 transcript 尾部加载的压缩后 token 预算。
	TranscriptTailBudget func(runtime any) int
	// IsInternalContent 判定一段内容是否为内部标记（task-context
	// checkpoint / provider-only），会话归档与尾部加载过滤用。
	IsInternalContent func(content string) bool
	// TailHistory 把 transcript 尾部事件按协议单元收敛为 provider 历史
	// （token 预算 + 单元上限），会话冷加载回退历史用。
	TailHistory func(events []model.TranscriptEvent, tokenBudget, maxUnits int) []contract.EngineMessage
	// OversizedToolResultWarning 是超限工具结果归档警告文本（context 域纯
	// 逻辑，装配根注入）。
	OversizedToolResultWarning func(name, resultRef string) string
	// ContentReferenceWarning 是超限用户输入归档引用警告文本（context 域
	// 纯逻辑，装配根注入）。
	ContentReferenceWarning func(resultRef string) string
	// Limits 返回当前生效的运行时上限（装配根注入，见 core.Limits）。
	Limits func() seelexctx.Limits
	// DisplayUserInput 把 provider 可见输入还原为 UI 显示输入（chat 域纯
	// 逻辑，装配根注入），会话标题恢复用。
	DisplayUserInput func(modelInput string) string
}
