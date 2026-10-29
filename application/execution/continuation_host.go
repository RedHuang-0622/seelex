package execution

// continuation_host.go — 续跑 gate 的**生产适配器**：把会话活状态、goal 栈读面、
// 回合终态这几处既有事实装成 ContinuationDeps，并给横条读面补上 stalled 时长。
//
// 为什么单开一个文件：continuation.go 只做判据与决策（不读存档、不起回合、不造
// idle 判据），装配方要的是"把这几条窄端口接上真实事实源"的那一层。判据一行都不
// 复制到这里——本文件只搬事实，真正的判定仍然只有一份（同文件 continuable /
// dto.DeriveGoalRunState）。
//
// 端口方向：本包不 import 装配层，也不 import 别的 app 子包；接口由消费方声明、
// 由 kernel/task（ContinuationStatePort / ContinuationTurnPort）与 application/goal
// （ContinuationGoalPort）在装配处**结构式**满足。

import (
	"context"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
	model "github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/limit"
)

// ContinuationStatePort 是续跑活状态的会话粒度读写面（kernel/task.Coordinator 实现）。
type ContinuationStatePort interface {
	ContinuationStateFor(sessionID string) model.SessionContinuationState
	SetContinuationPausedFor(sessionID, goalID string, paused bool, at time.Time)
	AppendContinuationAttemptFor(sessionID string, attempt model.ContinuationAttempt)
}

// ContinuationTurnPort 是"上一回合终态 + 当前回合身份"的读面。
type ContinuationTurnPort interface {
	TaskStateFor(sessionID string) *model.TaskState
	CurrentRequestIDFor(sessionID string) string
}

// ContinuationGoalPort 是层一的读面（application/goal.Application 实现）。
type ContinuationGoalPort interface {
	StatusFor(sessionID string) (dto.GoalStatusView, error)
}

// ContinuationHostDeps 是装配方要提供的事实源（全部函数式 / 结构式，缺一项 = 该
// 判据不成立，从不"默认放行"）。
type ContinuationHostDeps struct {
	State ContinuationStatePort
	Turns ContinuationTurnPort
	Goals ContinuationGoalPort
	// SessionIdle 复用装配侧既有的 async 触发 idle 判据（不在这里造第二份）。
	SessionIdle func(sessionID string) bool
	// Submit 与 CompletionApplication 走同一条提交通道。
	Submit func(ctx context.Context, sessionID, prompt string) error
	// Interrupt 是 push 通道的唯一副作用点（立刻停掉在飞的子会话）。
	Interrupt func(sessionID string) []string
	// Audit 把结构化打点映射到 goal 审计账（EventContinue / EventPause / EventResume）。
	Audit  func(sessionID string, entry ContinuationAudit)
	Limits func() limit.ContinuationLimits
	Now    func() time.Time
}

// NewContinuationHost 用真实事实源构造续跑 gate。
func NewContinuationHost(deps ContinuationHostDeps) *ContinuationApplication {
	host := &continuationAdapter{deps: deps}
	return NewContinuation(ContinuationDeps{
		Facts:              host.facts,
		SessionIdle:        deps.SessionIdle,
		CurrentRequestID:   host.currentRequestID,
		SubmitConversation: deps.Submit,
		RecordAttempt:      host.recordAttempt,
		SetPaused:          host.setPaused,
		InterruptInFlight:  deps.Interrupt,
		Audit:              deps.Audit,
		Limits:             deps.Limits,
		Now:                deps.Now,
	})
}

type continuationAdapter struct {
	deps ContinuationHostDeps
}

// facts 一次读全：活状态（层二 + 停止位 + 续跑账）、回合终态、goal 栈（层一）。
func (h *continuationAdapter) facts(sessionID string) ContinuationFacts {
	sessionID = strings.TrimSpace(sessionID)
	facts := ContinuationFacts{Continued: map[string]bool{}, Truncated: map[string]bool{}}
	if sessionID == "" || h.deps.State == nil {
		return facts
	}
	state := h.deps.State.ContinuationStateFor(sessionID)
	facts.Paused = state.Paused
	facts.PausedGoalID = state.PausedGoalID
	facts.StoppedByUser = state.StoppedByUser
	if h.deps.Turns != nil {
		if task := h.deps.Turns.TaskStateFor(sessionID); task != nil {
			facts.TurnStatus = task.Status
			facts.RequestID = strings.TrimSpace(task.RequestID)
			// 中断时刻只在"上一回合确实是硬中断"时当作 stalled 起点。
			if task.Status == dto.TurnInterrupted {
				facts.InterruptedAt = task.UpdatedAt
			}
		}
		if facts.RequestID == "" {
			facts.RequestID = strings.TrimSpace(h.deps.Turns.CurrentRequestIDFor(sessionID))
		}
	}
	if h.deps.Goals != nil {
		if view, err := h.deps.Goals.StatusFor(sessionID); err == nil {
			// 层一由 goal 栈现读、层二由活状态给：两层合成点只有 dto 一处。
			run := dto.GoalRunStateOf(view, state.Paused, state.PausedGoalID)
			facts.GoalID = run.GoalID
			facts.GoalStatus = run.Status
			if view.Active != nil && view.Active.ID == run.GoalID {
				facts.GoalTitle = view.Active.Title
			}
		}
	}
	ledger := model.DeriveContinuationLedger(facts.GoalID, state.Continuations)
	facts.Attempts = ledger.Attempts
	facts.LastAttemptAt = ledger.LastAttemptAt
	facts.Continued = ledger.Continued
	facts.Truncated = ledger.Truncated
	return facts
}

func (h *continuationAdapter) currentRequestID(sessionID string) string {
	if h.deps.Turns == nil {
		return ""
	}
	return strings.TrimSpace(h.deps.Turns.CurrentRequestIDFor(sessionID))
}

func (h *continuationAdapter) setPaused(sessionID, goalID string, paused bool, at time.Time) {
	if h.deps.State == nil {
		return
	}
	h.deps.State.SetContinuationPausedFor(sessionID, goalID, paused, at)
}

func (h *continuationAdapter) recordAttempt(sessionID string, attempt model.ContinuationAttempt) {
	if h.deps.State == nil {
		return
	}
	h.deps.State.AppendContinuationAttemptFor(sessionID, attempt)
}

// InterruptChildSessions 是 push 通道（⏸ 即时中断）的通用装配实现：按"子会话目录"
// 找出在飞回合并立刻取消，返回真被停掉的会话 ID。
//
// 本包不认识会话目录、也不持有取消句柄——两者都由装配方给（children = 角色/teammate
// 子会话目录读面；cancel = 会话域的取消面）。缺任一项 = 没有可用中断通道：返回 nil，
// 不声称停掉了任何子会话。
func InterruptChildSessions(
	children func(mainSessionID string) ([]string, error),
	cancel func(sessionIDs []string) []string,
) func(mainSessionID string) []string {
	return func(mainSessionID string) []string {
		if children == nil || cancel == nil || strings.TrimSpace(mainSessionID) == "" {
			return nil
		}
		sessionIDs, err := children(mainSessionID)
		if err != nil || len(sessionIDs) == 0 {
			return nil
		}
		return cancel(sessionIDs)
	}
}
