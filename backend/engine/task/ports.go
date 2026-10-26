// Package task_context owns two task domains: (1) the task **execution** domain
// (task runtime state, TaskService plan checkpoints / terminal states,
// append-only transcript, checkpoints, result-refs, token audit, plan runtime
// state — see Coordinator) and (2) the task **ledger** domain (cross-session
// work-table read face, per-session scope partitions, session default batch —
// see Ledger). Both read external capabilities only through injected ports, so
// neither reaches into core root or other domain packages.
package task_context

import (
	"github.com/RedHuang-0622/seelex/backend/engine/session"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/kernel/limit"
	"github.com/RedHuang-0622/seelex/kernel/state"
)

// 编译期断言：task_context.Coordinator 满足 session 域的 task/plan 权威状态
// 读写端口（实现方包内固化，装配根无需重复验证）。
var _ session_runtime.TaskPersistencePort = (*Coordinator)(nil)

// PromptPort 是 prompt 域对 task 的窄协作面（装配根注入）：
// 任务恢复需要 effort 级别与 skill 栈操作。
type PromptPort interface {
	CurrentEffort() string
	ClearSkillLayers()
	PushSkillLayer(kind, name, text string)
}

// Deps 是 task_context 的装配输入。Core 是共享状态内核；其余为跨域纯逻辑
// 端口（token 预算、prompt 栈、错误呈现、队列引用等），由装配根注入。
type Deps struct {
	Core   *state.Core
	Prompt PromptPort

	Limits func() limit.Limits
	// IsInternalContent 判定内容是否为内部标记（task-context checkpoint /
	// provider-only），transcript 导入过滤用。
	IsInternalContent func(content string) bool
	// IsOversizedToolResult 判定工具结果是否超限（context 域纯逻辑）。
	IsOversizedToolResult func(content string, maxChars int) bool
	// OversizedToolResultWarning 是超限工具结果归档警告文本（context 域）。
	OversizedToolResultWarning func(name, resultRef string) string
	// PresentToolError 把工具错误呈现为 provider 可见文本（error 域）。
	PresentToolError func(name string, err error) string
	// QueuedInputRefs 返回指定会话排队输入的显示引用（lifecycle 域；终态
	// 恢复记录用。F-3b：按会话取，回调自行保证线程安全，不要求调用方持
	// Core.ViewMu）。
	QueuedInputRefs func(sessionID string) []string
	// CurrentSessionID 返回当前活跃会话 ID（线程安全；会话域重构后流式
	// 路径在无 Core.ViewMu 下反查活跃会话）。
	CurrentSessionID func() string
	// AsyncPending 返回指定会话此刻在途的后台命令数（nil = 无后台执行域）。
	// 消费点是"无进展预算"：载荷字节口径的进展对安静的长命令不会推进，
	// 而有在途执行被查询本身就是进展。
	AsyncPending func(sessionID string) int
}

// LedgerHost 是 task 台账域（ledger.go）需要的宿主能力面：实时注册表
// （当前会话）的读写面。零依赖契约——只用 stdlib + contract/dto。
//
// 刻意只留"注册表怎么读写"：会话 scope 分区、当前任务会话与会话级批次是台账
// 域**自己**的状态，宿主不参与判据。进程级自动 ID 分配（NextAutoID /
// ObserveAutoID）与清单行形状的纯转换（TaskToTodoItem / TodoToTaskStatus /
// ValidTodoStatus）不是宿主状态，而是 `backend/tool/task` 的**包级纯函数与
// 分配器**——台账域直接调用它们，不为纯函数再造一层端口（AGENTS §8.1：
// 结构满足 > 适配层 > 复制一份）。
//
// 生产宿主是 `*backend/tool/task.TaskRegistry`（编译期钉子见 ledger.go）：
// 注册表结构上满足本接口，装配点直接注入，不需要适配层。
type LedgerHost interface {
	// Snapshot 返回实时注册表（当前会话）的只读快照。
	Snapshot() []dto.TaskRecord
	// Add 主动登记 task（幂等：Key 命中返回既有记录）。
	Add(spec dto.TaskSpec) (dto.TaskRecord, bool, error)
	// ResolveByKey 按幂等键查 task。
	ResolveByKey(key string) (dto.TaskRecord, bool, error)
	// SetStatus 更新 task 生命周期状态。
	SetStatus(id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error)
	// AttachParticipant 记录参与节点（并行执行证明）。
	AttachParticipant(id, participant string) (dto.TaskRecord, error)
	// AppendTrace 追加任务生命周期 trace 点。
	AppendTrace(id string, point dto.TaskTracePoint) (dto.TaskRecord, error)
	// ReplaceAll 用给定记录整体替换实时注册表（会话切换装载）。
	ReplaceAll(records []dto.TaskRecord) error
	// SetDefaultBatch 设置实时注册表的默认批次。
	SetDefaultBatch(batchID string) error
	// TaskChanged 返回 task.changed 输出 channel（CSP：变更即投递）。
	TaskChanged() <-chan dto.TaskRecord

	// TodoSnapshot 返回实时注册表里的清单行（kind=todo，保持顺序）。
	TodoSnapshot() []dto.TaskRecord
	// ReplaceTodo 整体替换实时注册表的清单（todo_init）。
	ReplaceTodo(items []dto.TodoItem) error
	// AppendTodo 追加清单项（todo_add；limit > 0 时按上限拒绝）。
	AppendTodo(item dto.TodoItem, limit int) error
	// SetTodoStatusByIndex 按清单序号设置三态（todo_done）。
	SetTodoStatusByIndex(index int, status dto.TaskStatus) (dto.TaskRecord, error)
}
