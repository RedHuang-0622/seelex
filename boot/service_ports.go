package boot

// service_ports.go 是应用层外部端口的**唯一**装配表。
//
// 为什么这张表只能在组合根：每个字段都是一个"应用层不认识实现"的端口，实现分散在
// Middle/Backend/Kernel 各 owner 里，只有组合根同时知道两边。表里能直接交出 owner
// 符号的，就直接交（`middlehistory.Assembler{}`、`assemble.HostSessionForToolCall`），
// 不再为它写一个同名构造函数——那是只返回 owner 结果的转发别名。
// 只有需要绑参数、改签名或做类型转换的端口，才留在 internal/host。

import (
	"github.com/RedHuang-0622/seelex/application/assembly"
	applicationexecution "github.com/RedHuang-0622/seelex/application/execution"
	applicationreview "github.com/RedHuang-0622/seelex/application/review"
	assemble "github.com/RedHuang-0622/seelex/backend/engine/assemble"
	inputrouter "github.com/RedHuang-0622/seelex/backend/engine/input"
	limitapproval "github.com/RedHuang-0622/seelex/backend/limit/approval"
	"github.com/RedHuang-0622/seelex/backend/multiagent/teamwork"
	reviewtool "github.com/RedHuang-0622/seelex/backend/tool/review"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/internal/host"
	"github.com/RedHuang-0622/seelex/kernel/event"
	middlehistory "github.com/RedHuang-0622/seelex/middle/switch/history"
)

func ServicePorts(runtime contract.RuntimePort) assembly.ExternalPorts {
	return assembly.ExternalPorts{
		HistoryWindow:      middlehistory.Assembler{},
		Residency:          host.NewResidencyPort(),
		ConversationStream: host.NewConversationStreamPort(),
		AccountWindow:      host.NewAccountWindowPolicy(),
		SkillActivation:    host.NewSkillActivationPort(),
		SessionRecords:     host.NewSessionRecordsPort(),
		ErrorPresentation:  host.NewErrorPresentationPort(),
		// 下面几格是签名/返回类型适配：owner 的构造函数形状与端口形状不同，
		// 适配写在装配表这一处，不再各自有一个命名构造函数。
		ApprovalFactory: func(events event.Hub) contract.ApprovalBroker {
			return limitapproval.NewApprovalBroker(events)
		},
		CommandRegistry: func() contract.CommandRegistryPort {
			return inputrouter.NewCommandRegistry()
		},
		InputRouter: func(handlers contract.InputRouteHandlers) contract.InputDispatcher {
			return inputrouter.NewRouter(inputrouter.RouteHandlers{
				Command:      handlers.Command,
				Plugin:       handlers.Plugin,
				Skill:        handlers.Skill,
				Conversation: handlers.Conversation,
			})
		},
		RoleSessions: host.NewRoleSessionsFactory(),
		TeamworkBoardReader: func(runtime contract.RuntimePort) applicationexecution.BoardReader {
			return teamwork.NewBoardReader(runtime)
		},
		HostSessionForToolCall: assemble.HostSessionForToolCall,
		Goal:                   host.NewGoalCoordinator(runtime),
		ReviewProcessor: func(host applicationreview.Host) applicationreview.RequestProcessor {
			return reviewtool.NewRequests(host)
		},
	}
}
