package table

import "time"

// AsyncConsumerDeps supplies WorkTable lifecycle signals and host actions.
type AsyncConsumerDeps struct {
	AsyncEvents    <-chan struct{}
	TeamworkEvents <-chan struct{}
	// ContinuationEvents 是**第三个信号源**：一条"某个会话刚收尾，去看看它的
	// goal 是不是还没收口、要不要自动续跑"的提示（payload = sessionID）。
	// 它只做唤醒，判据全在 application/execution 的续跑 gate 里
	// （docs/arch/goal-auto-continue.md §2.5/§2.8）。
	ContinuationEvents <-chan string
	Stop               <-chan struct{}
	Refresh            func()
	TriggerAsync       func(map[string]struct{}) int
	TriggerTeamwork    func(map[string]struct{}) int
	// TriggerContinuation 扫一次某会话的续跑门；返回值 >0 = 欠一次（忙/退避），
	// 交给同一条 RetryDelay 定时器再来——续跑**不新增定时器**。
	TriggerContinuation func(map[string]struct{}, string) int
	SafeCall            func(func())
	RetryDelay          time.Duration
}

// AsyncConsumer owns the async/teamwork/continuation lifecycle loop. It
// deliberately owns the retry ledger locally; callers never share that
// bookkeeping.
type AsyncConsumer struct {
	deps AsyncConsumerDeps
}

// NewAsyncConsumer constructs one lifecycle consumer.
func NewAsyncConsumer(deps AsyncConsumerDeps) *AsyncConsumer {
	if deps.RetryDelay <= 0 {
		deps.RetryDelay = 500 * time.Millisecond
	}
	return &AsyncConsumer{deps: deps}
}

// Run blocks until all signal sources are absent or Stop closes.
func (consumer *AsyncConsumer) Run() {
	if consumer == nil ||
		(consumer.deps.AsyncEvents == nil &&
			consumer.deps.TeamworkEvents == nil &&
			consumer.deps.ContinuationEvents == nil) {
		return
	}
	triggered := map[string]struct{}{}
	var (
		retryTimer       *time.Timer
		retryC           <-chan time.Time
		pendingAsync     bool
		pendingTeamwork  bool
		pendingSessionID string
	)
	safeCall := func(call func()) {
		if consumer.deps.SafeCall != nil {
			consumer.deps.SafeCall(call)
			return
		}
		call()
	}
	scheduleRetry := func() {
		if !pendingAsync && !pendingTeamwork && pendingSessionID == "" {
			return
		}
		if retryTimer == nil {
			retryTimer = time.NewTimer(consumer.deps.RetryDelay)
		} else {
			if !retryTimer.Stop() {
				select {
				case <-retryTimer.C:
				default:
				}
			}
			retryTimer.Reset(consumer.deps.RetryDelay)
		}
		retryC = retryTimer.C
	}
	scanAsync := func() bool {
		if consumer.deps.TriggerAsync == nil {
			return false
		}
		pending := 0
		safeCall(func() { pending = consumer.deps.TriggerAsync(triggered) })
		return pending > 0
	}
	scanTeamwork := func() bool {
		if consumer.deps.TriggerTeamwork == nil {
			return false
		}
		pending := 0
		safeCall(func() { pending = consumer.deps.TriggerTeamwork(triggered) })
		return pending > 0
	}
	scanContinuation := func(sessionID string) bool {
		if consumer.deps.TriggerContinuation == nil || sessionID == "" {
			return false
		}
		pending := 0
		safeCall(func() { pending = consumer.deps.TriggerContinuation(triggered, sessionID) })
		return pending > 0
	}
	for {
		select {
		case <-consumer.deps.AsyncEvents:
			if consumer.deps.Refresh != nil {
				safeCall(func() { consumer.deps.Refresh() })
			}
			pendingAsync = scanAsync()
			scheduleRetry()
		case <-consumer.deps.TeamworkEvents:
			pendingTeamwork = scanTeamwork()
			scheduleRetry()
		case sessionID, ok := <-consumer.deps.ContinuationEvents:
			if !ok {
				// 信号源关闭：置 nil 不再参与 select（避免空转）。
				consumer.deps.ContinuationEvents = nil
				if consumer.deps.AsyncEvents == nil && consumer.deps.TeamworkEvents == nil && !pendingAsync &&
					!pendingTeamwork && pendingSessionID == "" && retryTimer == nil {
					return
				}
				continue
			}
			pendingSessionID = ""
			if scanContinuation(sessionID) {
				pendingSessionID = sessionID
			}
			scheduleRetry()
		case <-retryC:
			retryC = nil
			if pendingAsync {
				pendingAsync = scanAsync()
			}
			if pendingTeamwork {
				pendingTeamwork = scanTeamwork()
			}
			if pendingSessionID != "" {
				sessionID := pendingSessionID
				pendingSessionID = ""
				if scanContinuation(sessionID) {
					pendingSessionID = sessionID
				}
			}
			scheduleRetry()
		case <-consumer.deps.Stop:
			if retryTimer != nil {
				retryTimer.Stop()
			}
			return
		}
	}
}
