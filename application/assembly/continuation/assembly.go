// Package continuation 是自动续跑的装配件（docs/arch/goal-auto-continue.md §2.8.8）。
//
// 它是应用装配层的一个子包：装配根只负责把既有组件作为**端口值**交进来，判据与
// 决策全部留在 application/execution，事实源留在各自的域（kernel/task 的会话活状态、
// application/goal 的 goal 栈、会话域的取消面、角色子会话目录）。
//
// 为什么要落在这里而不是直接写在装配根里：装配层规模预算只许持平或下降；把
// "哪些事实按什么形状喂给续跑门"这一层搬出根包，根包只留一行调用。
package continuation

import (
	"context"

	"github.com/RedHuang-0622/seelex/application/execution"
	taskcontext "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	sessionstate "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/kernel/limit"
)

// Ports 是装配根交出的事实源（缺一项 = 该判据不成立，从不默认放行）。
type Ports struct {
	// Tasks 持有会话粒度的续跑活状态与上一回合终态/requestID。
	Tasks *taskcontext.Coordinator
	// Goals 是层一（goal 栈）读面（application/goal.Application）。
	Goals execution.ContinuationGoalPort
	// Sessions 是会话域（push 通道的真实取消面）。
	Sessions *sessionstate.Domain
	// RoleSessions 是角色/teammate 子会话目录（push 的中断对象来源）。
	RoleSessions contract.RoleSessionsPort
	// Idle 复用装配侧既有的 async 触发 idle 判据（不造第二份）。
	Idle func(sessionID string) bool
	// Submit 与 CompletionApplication 走同一条提交通道。
	Submit func(ctx context.Context, sessionID, prompt string) error
}

// New 组装续跑 gate（开关读 limits.continuation：零值 = 关）。
func New(ports Ports) *execution.ContinuationApplication {
	var children func(string) ([]string, error)
	if ports.RoleSessions != nil {
		children = ports.RoleSessions.ListRoleSessions
	}
	return execution.NewContinuationHost(execution.ContinuationHostDeps{
		State:       ports.Tasks,
		Turns:       ports.Tasks,
		Goals:       ports.Goals,
		SessionIdle: ports.Idle,
		Submit:      ports.Submit,
		Interrupt: execution.InterruptChildSessions(children, func(sessionIDs []string) []string {
			return sessionstate.CancelRunningTurns(ports.Sessions, sessionIDs)
		}),
		Limits: func() limit.ContinuationLimits { return applicationpolicy.Get().Continuation },
	})
}
