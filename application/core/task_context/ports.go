// Package task_context owns the task execution domain: task runtime state,
// TaskService (plan checkpoints / terminal states), append-only transcript,
// checkpoints, result-refs, token audit and plan runtime state. The
// Coordinator reads external capabilities only through injected ports, so it
// never reaches into core root or other domain packages.
package task_context

import (
	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
	"github.com/RedHuang-0622/seelex/seelexctx"
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

	Limits func() seelexctx.Limits
	// IsInternalContent 判定内容是否为内部标记（task-context checkpoint /
	// provider-only），transcript 导入过滤用。
	IsInternalContent func(content string) bool
	// IsOversizedToolResult 判定工具结果是否超限（context 域纯逻辑）。
	IsOversizedToolResult func(content string, maxChars int) bool
	// OversizedToolResultWarning 是超限工具结果归档警告文本（context 域）。
	OversizedToolResultWarning func(name, resultRef string) string
	// PresentToolError 把工具错误呈现为 provider 可见文本（error 域）。
	PresentToolError func(name string, err error) string
	// QueuedInputRefs 返回当前排队输入的显示引用（lifecycle 域；终态恢复
	// 记录用）。
	QueuedInputRefs func() []string
}
