package table

import (
	"sync"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// LifecycleDeps supplies all WorkTable lifecycle signals and host actions.
type LifecycleDeps struct {
	SubagentEvents    <-chan struct{}
	TaskChanges       <-chan dto.TaskRecord
	Stop              <-chan struct{}
	RefreshSubagents  func()
	TaskContext       func() (revision uint64, requestID, sessionID string)
	PublishTaskChange func(dto.TaskRecord, uint64, string, string)
	SafeCall          func(func())
	Async             AsyncConsumerDeps
}

// Lifecycle owns the WorkTable consumer goroutines and their start-once guard.
type Lifecycle struct {
	deps LifecycleDeps
	once sync.Once
}

// NewLifecycle constructs the WorkTable lifecycle consumers.
func NewLifecycle(deps LifecycleDeps) *Lifecycle {
	if deps.Async.Stop == nil {
		deps.Async.Stop = deps.Stop
	}
	if deps.Async.SafeCall == nil {
		deps.Async.SafeCall = deps.SafeCall
	}
	return &Lifecycle{deps: deps}
}

// Start launches each configured consumer once.
func (lifecycle *Lifecycle) Start() {
	if lifecycle == nil {
		return
	}
	lifecycle.once.Do(func() {
		if lifecycle.deps.SubagentEvents != nil {
			go lifecycle.consumeSubagentLifecycle()
		}
		if lifecycle.deps.TaskChanges != nil {
			go lifecycle.consumeTaskChanges()
		}
		if lifecycle.deps.Async.AsyncEvents != nil || lifecycle.deps.Async.TeamworkEvents != nil || lifecycle.deps.Async.ContinuationEvents != nil {
			go NewAsyncConsumer(lifecycle.deps.Async).Run()
		}
	})
}

func (lifecycle *Lifecycle) consumeSubagentLifecycle() {
	for {
		select {
		case <-lifecycle.deps.SubagentEvents:
			lifecycle.safeCall(lifecycle.deps.RefreshSubagents)
		case <-lifecycle.deps.Stop:
			return
		}
	}
}

func (lifecycle *Lifecycle) consumeTaskChanges() {
	for {
		select {
		case record, ok := <-lifecycle.deps.TaskChanges:
			if !ok {
				return
			}
			if lifecycle.deps.PublishTaskChange == nil {
				continue
			}
			revision, requestID, sessionID := uint64(0), "", ""
			if lifecycle.deps.TaskContext != nil {
				revision, requestID, sessionID = lifecycle.deps.TaskContext()
			}
			lifecycle.safeCall(func() {
				lifecycle.deps.PublishTaskChange(record, revision, requestID, sessionID)
			})
		case <-lifecycle.deps.Stop:
			return
		}
	}
}

func (lifecycle *Lifecycle) safeCall(call func()) {
	if call == nil {
		return
	}
	if lifecycle.deps.SafeCall != nil {
		lifecycle.deps.SafeCall(call)
		return
	}
	call()
}
