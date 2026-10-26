package table

import (
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

func TestLifecycleDispatchesSubagentAndTaskSignals(t *testing.T) {
	subagentEvents := make(chan struct{}, 1)
	taskChanges := make(chan dto.TaskRecord, 1)
	stop := make(chan struct{})
	refreshed := make(chan struct{}, 1)
	published := make(chan struct {
		taskID    string
		revision  uint64
		requestID string
		sessionID string
	}, 1)

	lifecycle := NewLifecycle(LifecycleDeps{
		SubagentEvents: subagentEvents,
		TaskChanges:    taskChanges,
		Stop:           stop,
		RefreshSubagents: func() {
			refreshed <- struct{}{}
		},
		TaskContext: func() (uint64, string, string) { return 7, "request-1", "session-1" },
		PublishTaskChange: func(record dto.TaskRecord, revision uint64, requestID, sessionID string) {
			published <- struct {
				taskID    string
				revision  uint64
				requestID string
				sessionID string
			}{record.ID, revision, requestID, sessionID}
		},
	})
	lifecycle.Start()
	lifecycle.Start() // start-once

	subagentEvents <- struct{}{}
	select {
	case <-refreshed:
	case <-time.After(time.Second):
		t.Fatal("subagent signal did not refresh")
	}
	taskChanges <- dto.TaskRecord{ID: "task-1"}
	select {
	case got := <-published:
		if got.taskID != "task-1" || got.revision != 7 || got.requestID != "request-1" || got.sessionID != "session-1" {
			t.Fatalf("task publication = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("task change did not publish")
	}
	close(stop)
}

func TestLifecycleWithoutSignalsDoesNotStartWorkers(t *testing.T) {
	NewLifecycle(LifecycleDeps{}).Start()
}
