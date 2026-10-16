// Package wiring 是 Seelex 的组合根：把各层实现装配成门面所需的端口，并承载
// 只有启动期才需要的构造函数。
//
// 生态位与边界见 README.md。本包不承载业务规则：这里只有"谁把什么东西造出来"
// 的装配事实，判据、状态与协议一律留在其所属层。
package wiring

import (
	"context"

	"github.com/RedHuang-0622/seelex/application/core"
	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/prompt"
	limitapproval "github.com/RedHuang-0622/seelex/backend/limit/approval"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/seelebridge/search"
)

// New 构造应用 Service 并接线 contract.Dependencies 里的全部端口。
//
// 唯一实现是 core.New：这里只提供组合根侧的调用点，前端不得自造引擎
// （plan §1.3 ④）。返回值 *core.Service 与门面别名 application.Service 是同一
// 类型身份（别名不是新类型）。
func New(deps contract.Dependencies) (*core.Service, error) { return core.New(deps) }

// NewEventHub 构造进程级事件 hub（唯一事件发布点，前端按 Subscription 消费）。
func NewEventHub() *event.EventHub { return event.NewEventHub() }

// NewApprovalBroker 构造异步审批 broker；events 为 nil 表示不发布审批事件。
//
// 形参类型是具体 *event.EventHub 而不是 event.Hub 接口——调用方传 nil 时
// 语义才有意义（接口持 nil 指针与 nil 接口不同，见 permission_employee_panel_test.go）。
func NewApprovalBroker(events *event.EventHub) *limitapproval.ApprovalBroker {
	return limitapproval.NewApprovalBroker(events)
}

// NewToolHookBridge 构造工具钩子桥（工具调用前后的诊断与观测挂点）。
func NewToolHookBridge() *core.ToolHookBridge { return core.NewToolHookBridge() }

// NewPromptStack 构造空的 prompt 栈。
func NewPromptStack() *prompt.PromptStack { return prompt.NewPromptStack() }

// NewEffortManager 构造 effort 管理器（推理档位与 ReAct 循环预算）。
func NewEffortManager(stack *prompt.PromptStack, engine interface {
	SetMaxLoops(int)
	SetSystemPrompt(string)
}) *prompt.EffortManager {
	return prompt.NewEffortManager(stack, engine)
}

// WebSearch 执行一次 Web 搜索。
//
// 规划（尚未实现）：按 docs/2026-10-15-architecture-reshape/plan.md §2.2 归位到
// `backend/tool/websearch`（M4）；本包只作为 M1-B 的过渡调用点，实现仍是
// seelebridge/search.WebSearch 一份。
func WebSearch(ctx context.Context, config search.WebSearchConfig, query string, maxResults int) (string, error) {
	return search.WebSearch(ctx, config, query, maxResults)
}
