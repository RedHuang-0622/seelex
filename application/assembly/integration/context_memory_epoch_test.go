//go:build !extended

package integration

import (
	"sync"
	"testing"

	model "github.com/RedHuang-0622/seelex/contract/protocol"
	context_runtime "github.com/RedHuang-0622/seelex/kernel/context"
	task_context "github.com/RedHuang-0622/seelex/kernel/task"
)

type memoryEpochRuntime struct {
	*fakeRuntime
	mu         sync.Mutex
	compactSeq uint64
	freezes    int
	content    string
	query      string
}

func (runtime *memoryEpochRuntime) PrefixWorld() context_runtime.PrefixWorld {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return context_runtime.PrefixWorld{CompactSeq: runtime.compactSeq}
}

func (runtime *memoryEpochRuntime) FreezeMemoryEpoch(_ string, query string) string {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.freezes++
	runtime.query = query
	runtime.content = "memory:" + query
	return runtime.content
}

func (runtime *memoryEpochRuntime) SetMemoryEpoch(_ string, content string) {
	runtime.mu.Lock()
	runtime.content = content
	runtime.mu.Unlock()
}

func TestMemoryEpochSelectsOnceUntilCompactionEpochRolls(t *testing.T) {
	runtime := &memoryEpochRuntime{fakeRuntime: &fakeRuntime{}}
	engine := &fakeEngine{}
	service := newLegacyTestService(t, engine, withTestRuntime(runtime))
	sessionID := engine.SessionID()
	service.ViewMu.Lock()
	service.Core.Snapshot.Session.ID = sessionID
	service.Core.Snapshot.Chat = model.ChatState{Running: true, RequestID: "memory-epoch"}
	service.components.tasks.BeginTask("memory-epoch", "memory epoch", "high", nil, model.TaskCheckpoint{})
	service.ViewMu.Unlock()

	prepare := func(input string) {
		t.Helper()
		service.ViewMu.Lock()
		service.components.tasks.AppendTranscriptEventLocked(model.TranscriptEvent{
			TaskID: "memory-epoch", Role: "user", Content: input,
		})
		service.ViewMu.Unlock()
		if _, err := service.components.context.PrepareExecutionContextFor(sessionID, "memory-epoch", input); err != nil {
			t.Fatalf("prepare %q: %v", input, err)
		}
	}

	prepare("query one")
	prepare("query two")
	runtime.mu.Lock()
	freezes := runtime.freezes
	query := runtime.query
	runtime.mu.Unlock()
	if freezes != 1 || query != "query one" {
		t.Fatalf("same epoch reselected memory: freezes=%d query=%q", freezes, query)
	}
	rows := service.components.tasks.ContextEpochStateFor(sessionID).MemoryUpdates
	if len(rows) != 1 || rows[0].Epoch != 0 || rows[0].Bytes != "memory:query one" {
		t.Fatalf("epoch0 memory rows = %+v", rows)
	}

	runtime.mu.Lock()
	runtime.compactSeq = 1
	runtime.mu.Unlock()
	prepare("query three")
	rows = service.components.tasks.ContextEpochStateFor(sessionID).MemoryUpdates
	var retire, add int
	for _, row := range rows {
		switch row.Kind {
		case task_context.MemoryUpdateRetire:
			retire++
		case task_context.MemoryUpdateAdd:
			add++
		}
	}
	if retire != 1 || add != 2 {
		t.Fatalf("epoch roll rows = %+v (retire=%d add=%d)", rows, retire, add)
	}
	content, err := task_context.FoldMemoryUpdates(rows, 1)
	if err != nil || content != "memory:query three" {
		t.Fatalf("epoch1 fold=%q err=%v rows=%+v", content, err, rows)
	}
	t.Logf("memory rows=%d freeze_once=%d roll_fold=%q", len(rows), freezes, content)
}
