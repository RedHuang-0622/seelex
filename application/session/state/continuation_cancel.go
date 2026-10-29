package sessionstate

// continuation_cancel.go — push 通道（⏸ 即时中断）在会话域这一侧的唯一取消面
// （docs/arch/goal-auto-continue.md §2.8.3）。
//
// 它停的是**子会话的回合**（teammate / subagent 会话），语义等同输入框里那个红色
// 停止键：立刻 cancel 那一轮，不等轮边界、不走轮询。主会话自己正在跑的这一轮不在
// 这里——那是用户停止（CancelChat）的事，暂停从不打断当前这一轮。
//
// 只取消**确实在跑**的：没注册 unit、或回合已结束的会话不进返回值——不假装停掉了谁。

// CancelRunningTurns 立刻取消给定会话的在飞回合，返回真被取消的会话 ID。
func CancelRunningTurns(domain *Domain, sessionIDs []string) []string {
	if domain == nil || len(sessionIDs) == 0 {
		return nil
	}
	interrupted := make([]string, 0, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		unit := domain.Unit(sessionID)
		if unit == nil || !unit.ChatState().Running {
			continue
		}
		cancel := unit.CancelFunc()
		if cancel == nil {
			continue
		}
		cancel()
		interrupted = append(interrupted, sessionID)
	}
	return interrupted
}
