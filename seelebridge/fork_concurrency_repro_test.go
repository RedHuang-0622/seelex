package seelebridge

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// countingBlockingCompleter counts how many Complete calls are actually
// in-flight at once. It proves the account pool enforces MaxConcurrency even
// when many subagent sessions have already started (and are shown as running).
type countingBlockingCompleter struct {
	mu          sync.Mutex
	inFlight    int
	maxInFlight int
	started     chan struct{}
	release     chan struct{}
	once        sync.Once
}

func newCountingBlockingCompleter() *countingBlockingCompleter {
	return &countingBlockingCompleter{started: make(chan struct{}), release: make(chan struct{})}
}

func (c *countingBlockingCompleter) Complete(ctx context.Context, _ []types.Message, _ []types.Tool) (types.Message, error) {
	c.mu.Lock()
	c.inFlight++
	if c.inFlight > c.maxInFlight {
		c.maxInFlight = c.inFlight
	}
	c.mu.Unlock()
	c.once.Do(func() { close(c.started) })
	select {
	case <-c.release:
	case <-ctx.Done():
		c.mu.Lock()
		c.inFlight--
		c.mu.Unlock()
		return types.Message{}, ctx.Err()
	}
	c.mu.Lock()
	c.inFlight--
	c.mu.Unlock()
	reply := "done"
	return types.Message{Role: "assistant", Content: &reply}, nil
}

func (c *countingBlockingCompleter) peakInFlight() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.maxInFlight
}

// TestForkManySubagentsSharedAccountQueued reproduces the user scenario: all
// subagents of one fork are routed to a single account with MaxConcurrency=1,
// so real concurrency is 1 and the rest must queue at acquire/limiter.
// Assertions:
//  1. every subagent is really executed (completer sees N requests); none is
//     canceled while queued;
//  2. the task registry holds N subagent tasks, all completed;
//  3. merge-back delivers exactly N blocks (mailbox + overflow, drop=0);
//  4. the tool returns completed, not context.Canceled ("task stopped").
func TestForkManySubagentsSharedAccountQueued(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	if _, err := runtime.NewMainSession(nil); err != nil {
		t.Fatalf("create main session: %v", err)
	}
	runtime.SetParentEvidenceProjection(ParentEvidenceProjection{
		SessionID: "main", Goal: "audit many modules", ConversationCount: 1,
	})

	const subagents = 20
	shared := newScriptedNodeCompleter("MODULE-AUDITED")
	runtime.pool.Unregister("agent-1")
	mustRegisterAccount(t, runtime, "shared-agent", shared)

	specs := make([]string, 0, subagents)
	for i := 0; i < subagents; i++ {
		specs = append(specs, fmt.Sprintf(`{"id":"s%02d","goal":"audit module %d"}`, i, i))
	}
	args := `{"subagents":[` + strings.Join(specs, ",") + `]}`
	started := time.Now()
	result, err := runtime.Agent().DirectDispatch(context.Background(), "fork_subagents", args)
	t.Logf("fork_subagents took %v", time.Since(started))
	if err != nil {
		t.Fatalf("fork_subagents failed: %v", err)
	}
	if !strings.Contains(result, `"status":"completed"`) {
		t.Fatalf("fork_subagents did not complete: %s", result)
	}

	// Assertion 1: all subagents were really executed.
	shared.mu.Lock()
	requestCount := len(shared.requests)
	shared.mu.Unlock()
	if requestCount != subagents {
		t.Fatalf("completer saw %d/%d requests (queued subagents canceled?)", requestCount, subagents)
	}

	// Assertion 2: merge-back delivered exactly N blocks.
	kept := runtime.DrainSubagentContexts()
	if len(kept) != subagents {
		t.Fatalf("merge-back lost results: mailbox kept %d of %d blocks (dropped=%d)",
			len(kept), subagents, runtime.subagentContextDropped())
	}

	// Assertion 3: all subagent tasks reached completed.
	tasks := runtime.TaskSnapshot()
	byID := make(map[string]dto.TaskRecord, len(tasks))
	for _, task := range tasks {
		if task.Kind == "subagent" {
			byID[task.ID] = task
		}
	}
	for i := 0; i < subagents; i++ {
		id := fmt.Sprintf("subagent:s%02d", i)
		record, ok := byID[id]
		if !ok {
			t.Fatalf("task registry missing %s (has %d subagent tasks)", id, len(byID))
		}
		if record.Status != dto.TaskCompleted {
			t.Fatalf("%s status = %v, want completed", id, record.Status)
		}
	}
}

// TestForkManySubagentsSharedAccountDeadlockProbe blocks subagents on a single
// MaxConcurrency=1 account and verifies the account pool truly serializes them
// (peak in-flight == 1) and that release lets all complete within the timeout.
//
// Note: task status is NOT a reliable queue signal here - markNodeStarted flips
// a node to running at first request assembly, which happens before the account
// acquire inside Complete. Waiting subagents therefore show running in the
// worktable while the account semaphore is their real wait queue.
func TestForkManySubagentsSharedAccountDeadlockProbe(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	if _, err := runtime.NewMainSession(nil); err != nil {
		t.Fatalf("create main session: %v", err)
	}
	runtime.SetParentEvidenceProjection(ParentEvidenceProjection{
		SessionID: "main", Goal: "audit many modules", ConversationCount: 1,
	})

	const subagents = 12
	blocking := newCountingBlockingCompleter()
	runtime.pool.Unregister("agent-1")
	mustRegisterAccount(t, runtime, "shared-agent", blocking)

	specs := make([]string, 0, subagents)
	for i := 0; i < subagents; i++ {
		specs = append(specs, fmt.Sprintf(`{"id":"s%02d","goal":"audit module %d"}`, i, i))
	}
	args := `{"subagents":[` + strings.Join(specs, ",") + `]}`

	done := make(chan struct{})
	var result string
	var forkErr error
	go func() {
		result, forkErr = runtime.Agent().DirectDispatch(context.Background(), "fork_subagents", args)
		close(done)
	}()

	// First subagent really started (blocked inside Complete).
	select {
	case <-blocking.started:
	case <-time.After(10 * time.Second):
		t.Fatal("first subagent never started")
	}
	// Give the other 11 sessions time to reach the account semaphore.
	time.Sleep(300 * time.Millisecond)

	// Release the account: all queued subagents must finish in order, no deadlock.
	close(blocking.release)
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("fork did not finish after release (deadlock?)")
	}
	if forkErr != nil {
		t.Fatalf("fork failed: %v", forkErr)
	}
	if !strings.Contains(result, `"status":"completed"`) {
		t.Fatalf("fork did not complete: %s", result)
	}
	if peak := blocking.peakInFlight(); peak != 1 {
		t.Fatalf("account pool allowed %d concurrent completes, want 1 (MaxConcurrency not enforced)", peak)
	}
	kept := runtime.DrainSubagentContexts()
	if len(kept) != subagents {
		t.Fatalf("merge-back lost results: mailbox kept %d of %d blocks", len(kept), subagents)
	}
	byID := make(map[string]dto.TaskRecord)
	for _, record := range runtime.TaskSnapshot() {
		if record.Kind == "subagent" {
			byID[record.ID] = record
		}
	}
	if len(byID) != subagents {
		t.Fatalf("subagent tasks = %d, want %d", len(byID), subagents)
	}
	for id, record := range byID {
		if record.Status != dto.TaskCompleted {
			t.Fatalf("%s status = %v, want completed", id, record.Status)
		}
	}
}

// TestForkManySubagentsFailFastCancel reproduces fail-fast cascade cancel:
// when one subagent fails, the error must surface as the original failure, not
// be masked as context.Canceled ("task stopped").
func TestForkManySubagentsFailFastCancel(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	if _, err := runtime.NewMainSession(nil); err != nil {
		t.Fatalf("create main session: %v", err)
	}
	runtime.SetParentEvidenceProjection(ParentEvidenceProjection{
		SessionID: "main", Goal: "audit many modules", ConversationCount: 1,
	})

	const subagents = 10
	failing := &failingCompleter{err: fmt.Errorf("boom: one subagent crashed")}
	ok := newScriptedNodeCompleter("MODULE-AUDITED")
	runtime.pool.Unregister("agent-1")
	mustRegisterAccount(t, runtime, "fail-agent", failing)
	mustRegisterAccount(t, runtime, "ok-agent", ok)

	planJSON := buildManyAgentPlanJSON(subagents)
	if result, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", planJSON); err != nil || !strings.Contains(result, `"status":"loaded"`) {
		t.Fatalf("plan_load: %v %s", err, result)
	}
	result, err := runtime.Agent().DirectDispatch(context.Background(), "plan_run", `{}`)
	t.Logf("plan_run result: %.200s\nerr=%v", result, err)
	if err == nil {
		t.Fatal("plan_run must return an error when a subagent fails")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error must carry the subagent failure cause, got: %v", err)
	}
	// Key assertion: the error must not be masked as context.Canceled.
	if strings.Contains(err.Error(), "context canceled") || strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("error was masked as cancel/timeout, real failure hidden: %v", err)
	}
}

// buildManyAgentPlanJSON generates a parallel DAG: start -> N x agent -> finish.
func buildManyAgentPlanJSON(n int) string {
	var sb strings.Builder
	sb.WriteString(`{"entry":"start","nodes":{"start":{"input":"start","kind":"auto"}`)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("s%d", i)
		sb.WriteString(fmt.Sprintf(`,"%s":{"input":"audit module %d","kind":"agent"}`, id, i))
	}
	sb.WriteString(`,"finish":{"input":"finish","kind":"auto"}},"edges":{"start":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf(`"s%d"`, i))
	}
	sb.WriteString(`]`)
	for i := 0; i < n; i++ {
		sb.WriteString(fmt.Sprintf(`,"s%d":["finish"]`, i))
	}
	sb.WriteString(`}}`)
	return sb.String()
}
