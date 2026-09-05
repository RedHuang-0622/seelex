// Package prompt_layer owns system prompt composition and engine
// synchronization. It holds only references to the prompt stack / effort
// manager instances (owned by the composition root until a later sink) and a
// narrow task-context view; it never reaches into other domain packages.
package prompt_layer

import (
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/prompt"
	"github.com/RedHuang-0622/seelex/internal/promptassets"
)

// TaskContextView 是 prompt 域对 task 域的窄只读面。
type TaskContextView interface {
	CurrentTaskExecution() *task_context.TaskExecutionState
	// CurrentTaskExecutionFor 返回指定会话当前任务（多会话并行；未激活
	// 会话也返回其独立状态）。
	CurrentTaskExecutionFor(sessionID string) *task_context.TaskExecutionState
	ActivePlanID() string
	ActivePlanIDFor(sessionID string) string
	PlanSequence() uint64
	PlanSequenceFor(sessionID string) uint64
}

// Deps 是 prompt_layer 的装配输入。
type Deps struct {
	Core          *state.Core
	PromptStack   *prompt.PromptStack
	EffortManager *prompt.EffortManager
	Tasks         TaskContextView
}

// Coordinator 拥有 prompt 层组装与 provider 同步。
type Coordinator struct {
	*state.Core
	promptStack   *prompt.PromptStack
	effortManager *prompt.EffortManager
	tasks         TaskContextView

	// cacheMu 保护 lastSystemPrompt（prompt 域自有状态锁；G5 锁拆分：本层
	// 只串行化自己的"前缀缓存 + 引擎同步"临界区，不借用 Core.ViewMu 护它）。
	cacheMu          sync.Mutex
	lastSystemPrompt string // 缓存前缀稳定：内容不变时不重复 SetSystemPrompt
}

// NewCoordinator 构造 prompt 域协调器。
func NewCoordinator(deps Deps) *Coordinator {
	return &Coordinator{
		Core:          deps.Core,
		promptStack:   deps.PromptStack,
		effortManager: deps.EffortManager,
		tasks:         deps.Tasks,
	}
}

// BuildSystemPrompt 只组装稳定 system 层（激活技能正文由 context_runtime 作为
// provider-only 前缀消息装配，不持久化、不进 system）。
func (c *Coordinator) BuildSystemPrompt() {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	c.promptStack.ClearKind("identity")
	c.promptStack.ClearKind("instructions")
	c.promptStack.Push("identity", "identity", promptassets.SystemIdentity())
	c.promptStack.ClearKind("base")
	if current, ok := c.Deps.Plugins.Current(); ok {
		if promptText := strings.TrimSpace(current.Prompt); promptText != "" {
			c.promptStack.Push("base", "plugin-"+current.Name, promptText)
		}
	}
	_ = c.effortManager.Apply(c.effortManager.Current())
	c.promptStack.Push("instructions", "instructions", promptassets.SystemInstructions())
	promptText := c.promptStack.Render()
	if promptText == c.lastSystemPrompt {
		return
	}
	c.lastSystemPrompt = promptText
	c.Deps.Engine.SetSystemPrompt(promptText)
}

// ApplyActiveTaskSystemPrompt 按活跃任务刷新 system prompt（锁内读取任务
// 状态，锁外同步引擎）。
func (c *Coordinator) ApplyActiveTaskSystemPrompt(requestID string) {
	c.ApplyActiveTaskSystemPromptFor(c.activeSessionID(), requestID)
}

// ApplyActiveTaskSystemPromptFor 按指定会话活跃任务刷新 system prompt（锁内
// 读取任务状态，锁外同步引擎；多会话并行执行路径）。
func (c *Coordinator) ApplyActiveTaskSystemPromptFor(sessionID, requestID string) {
	c.ViewMu.RLock()
	if task := c.tasks.CurrentTaskExecutionFor(sessionID); task == nil || task.RequestID != requestID {
		c.ViewMu.RUnlock()
		return
	}
	promptText := c.SystemPromptForActiveTaskLockedFor(sessionID)
	c.ViewMu.RUnlock()
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	if promptText == c.lastSystemPrompt {
		return
	}
	c.lastSystemPrompt = promptText
	c.setEngineSystemPrompt(sessionID, promptText)
}

// SystemPromptForActiveTaskLocked 组装活跃任务 system prompt（调用方持有
// Core.ViewMu）。
func (c *Coordinator) SystemPromptForActiveTaskLocked() string {
	return c.SystemPromptForActiveTaskLockedFor(c.activeSessionID())
}

// SystemPromptForActiveTaskLockedFor 组装指定会话活跃任务 system prompt
// （调用方持有 Core.ViewMu）。
//
// 前缀缓存纪律：system 只放"与任务/会话无关"的稳定字节——base
// （identity→plugin→effort→instructions）+ 被动技能目录（仅随插件切换变化）。
// 两个此前毒害开头的动态段已移出：
//   - 激活技能正文（TrustedSkillLayers）→ 激活时作为 internal 事件 append 进
//     transcript（跟随对话 append-only，task_context 负责落事件），装配经
//     TranscriptTailHistory 携带，见 context_runtime 与 plan_transcript.go；
//   - plan 执行指令 → 并入请求尾部 plan 上下文消息（planContextMessage）。
//
// 效果：skill_activate/plan_load/节点推进/任务边界都不再改写 system 头部，
// system 变成跨会话、跨任务共享的常量前缀。
func (c *Coordinator) SystemPromptForActiveTaskLockedFor(sessionID string) string {
	parts := []string{c.promptStack.Render()}
	// 被动技能目录（插件级稳定）：base 之后插入"可用技能"清单。数据源随当前
	// 激活插件装配，模型零调用即每轮可见 → 插件切换/技能表变化才改变本段字节。
	if catalog := c.skillCatalogPart(); catalog != "" {
		parts = append(parts, catalog)
	}
	filtered := parts[:0]
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			filtered = append(filtered, part)
		}
	}
	return strings.Join(filtered, "\n\n---\n\n")
}

// skillCatalogPart 返回当前激活插件的"可用技能"被动目录段（无技能/无插件
// 返回 ""）。数据源 = contract.Skills 端口：skill.Registry 已在插件
// Load/Activate 时把该插件技能写入 pluginSkills 并按 activePlugin 隔离，
// adapters.SkillPort.All() 因此天然只返回当前插件技能表——插件装配即生效，
// 不需要模型先调用 skills_list 或任何工具（被动技能，降低插件自主性要求）。
func (c *Coordinator) skillCatalogPart() string {
	if c == nil || c.Core == nil {
		return ""
	}
	skills := c.Deps.Skills
	if skills == nil {
		return ""
	}
	return RenderSkillCatalog(skills.All())
}

// setEngineSystemPrompt 设置指定会话引擎的 system prompt（支持会话路由的
// 引擎用 For 方法，否则回退活跃引擎）。
func (c *Coordinator) setEngineSystemPrompt(sessionID, promptText string) {
	if routed, ok := c.Deps.Engine.(contract.SessionChatEngine); ok {
		routed.SetSystemPromptFor(sessionID, promptText)
		return
	}
	c.Deps.Engine.SetSystemPrompt(promptText)
}

// activeSessionID 返回当前活跃会话（快照归属会话）。
func (c *Coordinator) activeSessionID() string {
	return c.Snapshot.Session.ID
}
