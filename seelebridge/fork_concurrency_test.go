package seelebridge

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/types"
)

// blockingNodeCompleter 阻塞到 release 才返回（模拟真实子代理执行时长，
// 让并发上限下的队列状态可观察）。
type blockingNodeCompleter struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingNodeCompleter() *blockingNodeCompleter {
	return &blockingNodeCompleter{started: make(chan struct{}), release: make(chan struct{})}
}

func (c *blockingNodeCompleter) Complete(ctx context.Context, _ []types.Message, _ []types.Tool) (types.Message, error) {
	c.once.Do(func() { close(c.started) })
	select {
	case <-c.release:
	case <-ctx.Done():
		return types.Message{}, ctx.Err()
	}
	reply := "done"
	return types.Message{Role: "assistant", Content: &reply}, nil
}

// TestForkSubagentsExceedsConcurrencyLimitQueued 远超并发上限的 mock：
// 2 个账号（各 MaxConcurrency=1）跑 8 个子代理。运行中恰好 2 个 running、
// 其余全部 queued（无死锁、状态透明）；释放后全部 completed。
func TestForkSubagentsExceedsConcurrencyLimitQueued(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	first := newBlockingNodeCompleter()
	second := newBlockingNodeCompleter()
	injectScriptedCompleters(t, runtime, map[string]agent.Completer{"child-1": first, "child-2": second})

	const subagents = 8
	specs := make([]string, 0, subagents)
	for index := 0; index < subagents; index++ {
		specs = append(specs, fmt.Sprintf(`{"id":"s%02d","goal":"任务 %d"}`, index, index))
	}
	args := `{"subagents":[` + strings.Join(specs, ",") + `]}`

	done := make(chan struct{})
	var result string
	var forkErr error
	go func() {
		result, forkErr = runtime.Agent().DirectDispatch(context.Background(), "fork_subagents", args)
		close(done)
	}()

	// 前两个节点真正启动（会话挂载 → queued 转 running）。
	for name, completer := range map[string]*blockingNodeCompleter{"first": first, "second": second} {
		select {
		case <-completer.started:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s subagent never started", name)
		}
	}
	// 运行中：全部节点会话已挂载（running 语义=会话已分配），只有 2 个真正
	// 在执行（阻塞 completer）。无死锁判据 = 释放后 fork 必然完成。
	deadline := time.Now().Add(5 * time.Second)
	running := 0
	for {
		running = 0
		for _, record := range runtime.TaskSnapshot() {
			if record.Kind == "subagent" && record.Status == TaskRunning {
				running++
			}
		}
		if running >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected at least 2 running subagents, got %d", running)
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(first.release)
	close(second.release)

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("fork did not finish after release (deadlock?)")
	}
	if forkErr != nil {
		t.Fatalf("fork failed: %v", forkErr)
	}
	if !strings.Contains(result, `"status":"completed"`) {
		t.Fatalf("result must complete: %s", result)
	}
	// 终态：全部 completed，参与者挂好。
	byID := make(map[string]TaskRecord)
	for _, record := range runtime.TaskSnapshot() {
		if record.Kind == "subagent" {
			byID[record.ID] = record
		}
	}
	if len(byID) != subagents {
		t.Fatalf("subagent tasks = %d, want %d", len(byID), subagents)
	}
	for id, record := range byID {
		if record.Status != TaskCompleted {
			t.Fatalf("%s status = %v, want completed", id, record.Status)
		}
	}
}

// TestTaskRegistryHighConcurrencyBurst 注册表 mailbox 高并发突发：
// 32 个 goroutine 并发 Add/SetStatus，全部落库、无丢失、无死锁。
func TestTaskRegistryHighConcurrencyBurst(t *testing.T) {
	registry := newTaskRegistry()
	defer registry.Close()
	const workers = 32
	var wg sync.WaitGroup
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			key := fmt.Sprintf("goal:%d", index)
			task, _, err := registry.Add(TaskSpec{Key: key, Phase: TaskPhaseTask, Task: fmt.Sprintf("t%d", index), Kind: "task"})
			if err != nil {
				return
			}
			if task.ID != "" {
				_, _ = registry.SetStatus(task.ID, TaskRunning, "started")
			}
		}(index)
	}
	wg.Wait()
	if got := len(registry.Snapshot()); got != workers {
		t.Fatalf("tasks = %d, want %d", got, workers)
	}
}
