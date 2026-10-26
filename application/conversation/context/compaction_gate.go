package context

import (
	"context"
	"log"

	kernelstate "github.com/RedHuang-0622/seelex/kernel/state"
)

// CompactionGateDeps supplies the shared view lock and deferred submit action.
type CompactionGateDeps struct {
	Core           *kernelstate.Core
	DeferredSubmit func(context.Context, string, string) error
}

// CompactionGate serializes explicit compaction rounds per session.
type CompactionGate struct {
	deps       CompactionGateDeps
	compacting map[string]struct{}
	signal     chan struct{}
}

// NewCompactionGate constructs the per-session compaction gate.
func NewCompactionGate(deps CompactionGateDeps) *CompactionGate {
	return &CompactionGate{
		deps:       deps,
		compacting: make(map[string]struct{}),
		signal:     make(chan struct{}),
	}
}

// IsCompactingLocked reports whether a session has a compaction round.
func (gate *CompactionGate) IsCompactingLocked(sessionID string) bool {
	if gate == nil {
		return false
	}
	_, ok := gate.compacting[sessionID]
	return ok
}

func (gate *CompactionGate) signalLocked() {
	if gate.signal == nil {
		gate.signal = make(chan struct{})
		return
	}
	close(gate.signal)
	gate.signal = make(chan struct{})
}

// SignalLocked returns the current compaction-change signal.
func (gate *CompactionGate) SignalLocked() <-chan struct{} {
	if gate.signal == nil {
		gate.signal = make(chan struct{})
	}
	return gate.signal
}

// Acquire blocks until this session owns its single compaction round.
func (gate *CompactionGate) Acquire(ctx context.Context, sessionID string) error {
	for {
		gate.deps.Core.ViewMu.Lock()
		if !gate.IsCompactingLocked(sessionID) {
			gate.compacting[sessionID] = struct{}{}
			gate.deps.Core.ViewMu.Unlock()
			return nil
		}
		signal := gate.SignalLocked()
		gate.deps.Core.ViewMu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-signal:
		}
	}
}

// Release closes one compaction round and wakes waiters.
func (gate *CompactionGate) Release(sessionID string) {
	gate.deps.Core.ViewMu.Lock()
	if _, ok := gate.compacting[sessionID]; ok {
		delete(gate.compacting, sessionID)
		gate.signalLocked()
	}
	gate.deps.Core.ViewMu.Unlock()
}

// Await waits until the session has no compaction round.
func (gate *CompactionGate) Await(ctx context.Context, sessionID string) error {
	for {
		gate.deps.Core.ViewMu.Lock()
		compacting := gate.IsCompactingLocked(sessionID)
		signal := gate.SignalLocked()
		gate.deps.Core.ViewMu.Unlock()
		if !compacting {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-signal:
		}
	}
}

// DeferSubmitAfterCompaction replays one input after the session's current
// compaction round completes.
func (gate *CompactionGate) DeferSubmitAfterCompaction(ctx context.Context, sessionID, text string) {
	go func() {
		if err := gate.Await(ctx, sessionID); err != nil {
			return
		}
		if err := gate.deps.DeferredSubmit(ctx, sessionID, text); err != nil {
			log.Printf("[submit] deferred submit to %q after compaction: %v", sessionID, err)
		}
	}()
}
