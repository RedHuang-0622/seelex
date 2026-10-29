package model

import (
	"strings"
	"time"
)

// continuation_state.go — 自动续跑控制位的**活状态**形状（docs/arch/goal-auto-continue.md
// §2.8.0/§2.8.1）。
//
// 为什么单独一个类型而不是直接拿 SessionExecutionRecord 到处走：record 是**落盘面**
// （整键替换、只在会话落盘时写），而层二子状态与续跑账必须在回合进行中就能读、能写
// （⏸ 写位 → 轮边界读位）。两者字段一一对应（见 ApplyTo / SessionContinuationStateOf），
// 但生命周期不同：活状态按会话常驻内存，落盘面是它的投影。
//
// 判据不在这里：两层状态的唯一判定点是 dto.DeriveGoalRunState，续跑是否放行是
// application/execution 的 continuable。本类型只装事实。
type SessionContinuationState struct {
	// StoppedByUser / StoppedAt 是"用户显式按过停止"（C3，跨重启有效的人工意图）。
	StoppedByUser bool
	StoppedAt     time.Time
	// Paused / PausedAt / PausedGoalID 是层二子状态（⏸）与其绑定的 goal：
	// 只有 PausedGoalID 等于当前 Active goal 时才算 paused（I2，换 goal 不残留）。
	Paused       bool
	PausedAt     time.Time
	PausedGoalID string
	// Continuations 是 append-only 的续跑记录（幂等 + 上限 + 退避的唯一权威）。
	Continuations []ContinuationAttempt
}

// ContinuationLedger 是续跑账（append-only 记录）的派生读面。
//
// 派生口径**只有这一处**（ContinuationAttempt 的注释是规格，这里是实现）：
//   - Attempts 是"最后一次人工清零之后真发出去的续跑条数"（上限 + 退避用它）；
//   - LastAttemptAt 是这些真续跑里最近一条的时刻（退避起点）；
//   - Continued / Truncated 是按 (GoalID, RequestID) 的**幂等/止步痕**判据表。
type ContinuationLedger struct {
	Attempts      int
	LastAttemptAt time.Time
	Continued     map[string]bool
	Truncated     map[string]bool
}

// DeriveContinuationLedger 把某一 goal 的续跑记录派生成读面。
// goalID 为空（goal 已收口）时一条都不认：没有可交付对象，就没有续跑的账。
func DeriveContinuationLedger(goalID string, records []ContinuationAttempt) ContinuationLedger {
	ledger := ContinuationLedger{Continued: map[string]bool{}, Truncated: map[string]bool{}}
	goalID = strings.TrimSpace(goalID)
	if goalID == "" {
		return ledger
	}
	for _, record := range records {
		if strings.TrimSpace(record.GoalID) != goalID {
			continue
		}
		key := ContinuationKey(record.GoalID, record.RequestID)
		switch {
		case record.Truncated:
			// 截断没发出续跑：只记止步痕，不挡人工恢复。
			ledger.Truncated[key] = true
		case record.Manual && record.Attempt <= 0:
			// 人工恢复的清零标记（Attempt=0）：计数从这里重新数。
			ledger.Attempts = 0
			ledger.LastAttemptAt = time.Time{}
		case record.Attempt <= 0:
			// 既不是续跑也不是清零：不计数。
		default:
			// 真发过一次续跑（Manual=true 表示这一次是 ⟳ 触发的）。
			ledger.Continued[key] = true
			ledger.Attempts++
			ledger.LastAttemptAt = record.At
		}
	}
	return ledger
}

// ContinuationKey 是续跑记录的幂等键：(goal, 被中断的那次 request)。
func ContinuationKey(goalID, requestID string) string {
	return strings.TrimSpace(goalID) + "|" + strings.TrimSpace(requestID)
}

// ApplyTo 把活状态投影到会话存档的 execution 块（落盘面与活状态只有这一处转换）。
func (state SessionContinuationState) ApplyTo(record *SessionExecutionRecord) {
	if record == nil {
		return
	}
	record.StoppedByUser = state.StoppedByUser
	record.StoppedAt = state.StoppedAt
	record.Paused = state.Paused
	record.PausedAt = state.PausedAt
	record.PausedGoalID = state.PausedGoalID
	record.Continuations = append([]ContinuationAttempt(nil), state.Continuations...)
}

// SessionContinuationStateOf 从会话存档的 execution 块读回活状态（冷加载方向）。
func SessionContinuationStateOf(record SessionExecutionRecord) SessionContinuationState {
	return SessionContinuationState{
		StoppedByUser: record.StoppedByUser,
		StoppedAt:     record.StoppedAt,
		Paused:        record.Paused,
		PausedAt:      record.PausedAt,
		PausedGoalID:  record.PausedGoalID,
		Continuations: append([]ContinuationAttempt(nil), record.Continuations...),
	}
}
