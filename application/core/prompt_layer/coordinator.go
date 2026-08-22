// Package prompt_layer owns system prompt composition and engine
// synchronization. It holds only references to the prompt stack / effort
// manager instances (owned by the composition root until a later sink) and a
// narrow task-context view; it never reaches into other domain packages.
package prompt_layer

import (
	"strings"

	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/application/prompt"
	"github.com/RedHuang-0622/seelex/internal/promptassets"
)

// TaskContextView 是 prompt 域对 task 域的窄只读面。
type TaskContextView interface {
	CurrentTaskExecution() *task_context.TaskExecutionState
	ActivePlanID() string
	PlanSequence() uint64
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

// BuildSystemPrompt 只组装 system 层（skill 内容留在请求信封，不持久化）。
func (c *Coordinator) BuildSystemPrompt() {
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
	c.Mu.RLock()
	if task := c.tasks.CurrentTaskExecution(); task == nil || task.RequestID != requestID {
		c.Mu.RUnlock()
		return
	}
	promptText := c.SystemPromptForActiveTaskLocked()
	c.Mu.RUnlock()
	if promptText == c.lastSystemPrompt {
		return
	}
	c.lastSystemPrompt = promptText
	c.Deps.Engine.SetSystemPrompt(promptText)
}

// SystemPromptForActiveTaskLocked 组装活跃任务 system prompt（调用方持有
// Core.Mu）。
func (c *Coordinator) SystemPromptForActiveTaskLocked() string {
	parts := []string{c.promptStack.Render()}
	if task := c.tasks.CurrentTaskExecution(); task != nil {
		for _, layer := range task.TrustedSkillLayers {
			text := strings.TrimSpace(layer.Text)
			if text == "" {
				continue
			}
			parts = append(parts, "## Trusted Active Skill: "+layer.Name+"\n"+text)
		}
	}
	if plan := task_context.ActivePlanProjection(c.Snapshot.Runtime.Plan, c.tasks.ActivePlanID(), c.tasks.PlanSequence()); plan != nil && plan.Status != string(model.PlanCompleted) {
		// 前缀缓存友好：system prompt 只放 plan 级稳定信息（plan_ref），
		// 不放随节点变化的 current_node——节点状态由每请求尾部的 plan
		// 上下文消息（planContextMessage）与 read_plan 提供。
		parts = append(parts, "## Active Plan Execution Policy\n"+
			"The Plan is validated and authoritative for this task. Do not silently replace or reorder it. "+
			"Execute the current node and its declared dependencies in stable order. Use read_plan for omitted node detail. "+
			"plan_ref="+plan.CanonicalPlanRef)
	}
	filtered := parts[:0]
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			filtered = append(filtered, part)
		}
	}
	return strings.Join(filtered, "\n\n---\n\n")
}
