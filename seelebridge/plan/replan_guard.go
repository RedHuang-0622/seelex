package plan

import (
	"errors"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/seelexctx"
)

var (
	ErrReplanConcurrencyLimit = errors.New("replan rejected: global concurrent replan limit reached")
	ErrReplanRateLimit        = errors.New("replan rejected: global replan budget exhausted")
	ErrReplanProviderBudget   = errors.New("replan rejected: global provider request budget exhausted")
	ErrReplanDuplicate        = errors.New("replan rejected: duplicate operation already in flight")
)

// ReplanMetrics is a non-secret, process-wide accounting snapshot. Provider
// calls are an intentional cost proxy when a provider does not expose token
// usage for tool-only responses.
type ReplanMetrics struct {
	InFlight               int       `json:"in_flight"`
	ConcurrentLimit        int       `json:"concurrent_limit"`
	WindowAttempts         int       `json:"window_attempts"`
	WindowLimit            int       `json:"window_limit"`
	WindowStartedAt        time.Time `json:"window_started_at,omitempty"`
	Accepted               uint64    `json:"accepted"`
	Succeeded              uint64    `json:"succeeded"`
	Failed                 uint64    `json:"failed"`
	Rejected               uint64    `json:"rejected"`
	DuplicateRejected      uint64    `json:"duplicate_rejected"`
	ProviderRequests       uint64    `json:"provider_requests"`
	ProviderWindowRequests int       `json:"provider_window_requests"`
	ProviderWindowLimit    int       `json:"provider_window_limit"`
}

type ReplanGuard struct {
	mu                  sync.Mutex
	now                 func() time.Time
	maxConcurrent       int
	maxWindowAttempts   int
	maxProviderRequests int
	window              time.Duration
	starts              []time.Time
	providerStarts      []time.Time
	inFlightKeys        map[string]struct{}
	metrics             ReplanMetrics
}

func NewReplanGuard(maxConcurrent, maxWindowAttempts, maxProviderRequests int, window time.Duration) *ReplanGuard {
	defaults := seelexctx.DefaultLimits()
	if maxConcurrent <= 0 {
		maxConcurrent = defaults.MaxConcurrentReplans
	}
	if maxWindowAttempts <= 0 {
		maxWindowAttempts = defaults.MaxReplansPerWindow
	}
	if maxProviderRequests <= 0 {
		maxProviderRequests = defaults.MaxReplanProviderReqs
	}
	if window <= 0 {
		window = time.Duration(defaults.ReplanWindowSec) * time.Second
	}
	return &ReplanGuard{
		now:                 time.Now,
		maxConcurrent:       maxConcurrent,
		maxWindowAttempts:   maxWindowAttempts,
		maxProviderRequests: maxProviderRequests,
		window:              window,
		inFlightKeys:        make(map[string]struct{}),
		metrics: ReplanMetrics{
			ConcurrentLimit:     maxConcurrent,
			WindowLimit:         maxWindowAttempts,
			ProviderWindowLimit: maxProviderRequests,
		},
	}
}

func (guard *ReplanGuard) acquire(idempotencyKey string) (func(error), error) {
	guard.mu.Lock()
	now := guard.now()
	guard.pruneLocked(now)
	if idempotencyKey != "" {
		if _, exists := guard.inFlightKeys[idempotencyKey]; exists {
			guard.metrics.Rejected++
			guard.metrics.DuplicateRejected++
			guard.mu.Unlock()
			return nil, ErrReplanDuplicate
		}
	}
	if guard.metrics.InFlight >= guard.maxConcurrent {
		guard.metrics.Rejected++
		guard.mu.Unlock()
		return nil, ErrReplanConcurrencyLimit
	}
	if len(guard.starts) >= guard.maxWindowAttempts {
		guard.metrics.Rejected++
		guard.mu.Unlock()
		return nil, ErrReplanRateLimit
	}
	if len(guard.providerStarts) >= guard.maxProviderRequests {
		guard.metrics.Rejected++
		guard.mu.Unlock()
		return nil, ErrReplanProviderBudget
	}
	guard.starts = append(guard.starts, now)
	if idempotencyKey != "" {
		guard.inFlightKeys[idempotencyKey] = struct{}{}
	}
	guard.metrics.InFlight++
	guard.metrics.Accepted++
	guard.updateWindowLocked()
	guard.mu.Unlock()

	var once sync.Once
	return func(err error) {
		once.Do(func() {
			guard.mu.Lock()
			guard.metrics.InFlight--
			delete(guard.inFlightKeys, idempotencyKey)
			if err == nil {
				guard.metrics.Succeeded++
			} else {
				guard.metrics.Failed++
			}
			guard.mu.Unlock()
		})
	}, nil
}

func (guard *ReplanGuard) acquireProviderRequest() error {
	guard.mu.Lock()
	guard.pruneLocked(guard.now())
	if len(guard.providerStarts) >= guard.maxProviderRequests {
		guard.metrics.Rejected++
		guard.mu.Unlock()
		return ErrReplanProviderBudget
	}
	guard.providerStarts = append(guard.providerStarts, guard.now())
	guard.metrics.ProviderRequests++
	guard.updateWindowLocked()
	guard.mu.Unlock()
	return nil
}

func (guard *ReplanGuard) snapshot() ReplanMetrics {
	guard.mu.Lock()
	guard.pruneLocked(guard.now())
	metrics := guard.metrics
	guard.mu.Unlock()
	return metrics
}

func (guard *ReplanGuard) pruneLocked(now time.Time) {
	cutoff := now.Add(-guard.window)
	first := 0
	for first < len(guard.starts) && guard.starts[first].Before(cutoff) {
		first++
	}
	if first > 0 {
		guard.starts = append([]time.Time(nil), guard.starts[first:]...)
	}
	providerFirst := 0
	for providerFirst < len(guard.providerStarts) && guard.providerStarts[providerFirst].Before(cutoff) {
		providerFirst++
	}
	if providerFirst > 0 {
		guard.providerStarts = append([]time.Time(nil), guard.providerStarts[providerFirst:]...)
	}
	guard.updateWindowLocked()
}

func (guard *ReplanGuard) updateWindowLocked() {
	guard.metrics.WindowAttempts = len(guard.starts)
	guard.metrics.ProviderWindowRequests = len(guard.providerStarts)
	guard.metrics.WindowStartedAt = time.Time{}
	if len(guard.starts) > 0 {
		guard.metrics.WindowStartedAt = guard.starts[0]
	}
}
