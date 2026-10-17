package core

// assembly_helpers.go 是 core 的**装配与转发面**：未导出的 input 路由别名、
// `ReActBudget` 别名、几个**构造函数**（`NewCommandRegistry` / `NewEventHub` / `NewApprovalBroker` /
// `NewPromptStack` / `NewEffortManager` / `NewDefaultWindowPolicy`…）与若干纯转发 helper（`cloneSnapshot` /
// `cloneRuntimeState` / `maxLoopsFor` / `reasoningEffortFor` / `reactBudgetFor`）。
//
// 落点沿革：2026-10-17 随 **M5-R10b** 从 `application/core/aliases.go` 拆出（同一次删掉了那个文件的
// 类型别名块）。
//
// **登记（提案 P22）**：`New*` 一族按 plan §1.3 ④ 应迁到 `wiring/`（前端不得自造引擎）。
// 今天它们仍在 core，因为 `wiring/**` 与若干用例直接调 `core.New*`；迁移需要同时动
// `wiring/**`，不属于"行为零变化的收尾片"，故单独登记。

import (
	"github.com/RedHuang-0622/seelex/application/prompt"
	input_router "github.com/RedHuang-0622/seelex/backend/engine/input"
	cc "github.com/RedHuang-0622/seelex/backend/engine/input/contextcontrol"
	limitapproval "github.com/RedHuang-0622/seelex/backend/limit/approval"
	"github.com/RedHuang-0622/seelex/middle/protocol/event"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

type (
	inputDispatcher    = input_router.Dispatcher
	inputRouter        = input_router.Router
	inputRouteHandlers = input_router.RouteHandlers
)

func NewCommandRegistry() *input_router.CommandRegistry { return input_router.NewCommandRegistry() }

func newInputRouter(handlers inputRouteHandlers) *inputRouter {
	return input_router.NewRouter(handlers)
}

func NewDefaultWindowPolicy(config cc.WindowConfig) cc.DefaultWindowPolicy {
	return cc.NewDefaultWindowPolicy(config)
}

func DefaultWindowConfig() cc.WindowConfig {
	return cc.DefaultWindowConfig()
}

func LoadWindowConfig(path string) (cc.WindowConfig, error) {
	return cc.LoadWindowConfig(path)
}

func NewEventHub() *event.EventHub { return event.NewEventHub() }

func NewApprovalBroker(events event.Hub) *limitapproval.ApprovalBroker {
	return limitapproval.NewApprovalBroker(events)
}

func NewPromptStack() *prompt.PromptStack { return prompt.NewPromptStack() }

func NewEffortManager(stack *prompt.PromptStack, engine interface {
	SetMaxLoops(int)
	SetSystemPrompt(string)
}) *prompt.EffortManager {
	return prompt.NewEffortManager(stack, engine)
}

type ReActBudget = prompt.ReActBudget

func cloneSnapshot(snapshot model.Snapshot) model.Snapshot { return model.CloneSnapshot(snapshot) }

func cloneRuntimeState(runtime model.RuntimeState) model.RuntimeState {
	return model.CloneRuntimeState(runtime)
}

func maxLoopsFor(level string) int { return prompt.MaxLoops(level) }

func reasoningEffortFor(level string) string { return prompt.ReasoningEffortFor(level) }

func reactBudgetFor(level string) prompt.ReActBudget { return prompt.ReActBudgetFor(level) }
