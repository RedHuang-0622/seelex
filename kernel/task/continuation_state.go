package task_context

// continuation_state.go — 自动续跑控制位与续跑账的**会话粒度活状态**
// （docs/arch/goal-auto-continue.md §2.8.0/§2.8.1）。
//
// 状态为什么住在这里：paused / stopped / continuations 是三层事实的耦合体——
// 它们与"上一回合终态 + 当前 requestID"必须一起读（Facts 一次读面给全），而后者
// 已经由本协调器按会话持有。分开存会要求续跑 gate 同时读两个来源、再去对齐时序。
//
// 纪律：
//   - 会话粒度（I3）：键是 sessionID，A 的暂停不影响 B；
//   - 本文件只读写事实，不做判据：两层状态的唯一判定点是 dto.DeriveGoalRunState；
//   - Update 是唯一写点，回调在状态锁内执行（回调里不得再调本协调器的任何方法）。

import (
	"time"

	model "github.com/RedHuang-0622/seelex/contract/protocol"
)

// ContinuationStateFor 返回指定会话的续跑活状态（深拷贝：调用方拿到的切片与
// 内部账不共享底层数组）。
func (c *Coordinator) ContinuationStateFor(sessionID string) model.SessionContinuationState {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	state := c.sessionStateLocked(sessionID).continuation
	state.Continuations = append([]model.ContinuationAttempt(nil), state.Continuations...)
	return state
}

// UpdateContinuationStateFor 是续跑活状态的唯一写点：在状态锁内把当前状态交给
// update 改完写回。回调内**不得**再调用本协调器的任何方法（会自等 stateMu）。
func (c *Coordinator) UpdateContinuationStateFor(
	sessionID string,
	update func(*model.SessionContinuationState),
) {
	if update == nil {
		return
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	state := &c.sessionStateLocked(sessionID).continuation
	update(state)
}

// ClearContinuationGatesFor 是"开新回合"的唯一清位实现（turn_process.BeginLocked 调
// 它）：层二回到 continue、停止位清零——新回合不继承上一轮的暂停，也不继承上一轮
// 的人工停止意图（用户自己发了下一条 = 恢复让它跑）。续跑账**不清**：它是跨回合的
// 幂等与计数事实，清掉就等于允许同一中断点再续一次。
func (c *Coordinator) ClearContinuationGatesFor(sessionID string) {
	c.UpdateContinuationStateFor(sessionID, func(state *model.SessionContinuationState) {
		// 清位不记时刻：读侧只问"现在是不是暂停"，不问"什么时候清的"。
		state.Paused = false
		state.PausedAt = time.Time{}
		state.PausedGoalID = ""
		state.StoppedByUser = false
		state.StoppedAt = time.Time{}
	})
}

// SetContinuationPausedFor 是层二的唯一写点（⏸ 置位 / ⟳ 清位共用）。
// goalID 是这次暂停绑定到哪张 goal 上：清位时同样带上，便于把过期绑定一起清掉。
func (c *Coordinator) SetContinuationPausedFor(sessionID, goalID string, paused bool, at time.Time) {
	c.UpdateContinuationStateFor(sessionID, func(state *model.SessionContinuationState) {
		state.Paused = paused
		if paused {
			state.PausedAt = at
			state.PausedGoalID = goalID
			return
		}
		state.PausedAt = time.Time{}
		state.PausedGoalID = goalID
	})
}

// MarkStoppedByUserFor 记录"用户显式按过停止"（C3）。
func (c *Coordinator) MarkStoppedByUserFor(sessionID string, at time.Time) {
	c.UpdateContinuationStateFor(sessionID, func(state *model.SessionContinuationState) {
		state.StoppedByUser = true
		state.StoppedAt = at
	})
}

// AppendContinuationAttemptFor 追加一条续跑记录（append-only：幂等、上限、退避、
// 人工清零都从这一份账派生，见 model.ContinuationAttempt 的派生口径）。
func (c *Coordinator) AppendContinuationAttemptFor(sessionID string, attempt model.ContinuationAttempt) {
	c.UpdateContinuationStateFor(sessionID, func(state *model.SessionContinuationState) {
		state.Continuations = append(state.Continuations, attempt)
	})
}
