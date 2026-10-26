package table

import "time"

// AsyncConsumerDeps supplies WorkTable lifecycle signals and host actions.
type AsyncConsumerDeps struct {
	AsyncEvents     <-chan struct{}
	TeamworkEvents  <-chan struct{}
	Stop            <-chan struct{}
	Refresh         func()
	TriggerAsync    func(map[string]struct{}) int
	TriggerTeamwork func(map[string]struct{}) int
	SafeCall        func(func())
	RetryDelay      time.Duration
}

// AsyncConsumer owns the async/teamwork lifecycle loop. It deliberately owns
// the retry ledger locally; callers never share that bookkeeping.
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

// Run blocks until both signal sources are absent or Stop closes.
func (consumer *AsyncConsumer) Run() {
	if consumer == nil || (consumer.deps.AsyncEvents == nil && consumer.deps.TeamworkEvents == nil) {
		return
	}
	triggered := map[string]struct{}{}
	var (
		retryTimer      *time.Timer
		retryC          <-chan time.Time
		pendingAsync    bool
		pendingTeamwork bool
	)
	safeCall := func(call func()) {
		if consumer.deps.SafeCall != nil {
			consumer.deps.SafeCall(call)
			return
		}
		call()
	}
	scheduleRetry := func() {
		if !pendingAsync && !pendingTeamwork {
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
	for {
		select {
		case <-consumer.deps.AsyncEvents:
			if consumer.deps.Refresh != nil {
				safeCall(consumer.deps.Refresh)
			}
			pendingAsync = scanAsync()
			scheduleRetry()
		case <-consumer.deps.TeamworkEvents:
			pendingTeamwork = scanTeamwork()
			scheduleRetry()
		case <-retryC:
			retryC = nil
			if pendingAsync {
				pendingAsync = scanAsync()
			}
			if pendingTeamwork {
				pendingTeamwork = scanTeamwork()
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
