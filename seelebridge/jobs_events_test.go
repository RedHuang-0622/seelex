package seelebridge

import (
	"context"
	"sync"
	"testing"
	"time"

	frameworkevent "github.com/RedHuang-0622/Seele/event"
	"github.com/RedHuang-0622/Seele/jobs"
)

// TestJobsEventStreamProjectsLifecycleIntoSessionLog 钉住任务 0 的回退口径：
// 作业生命周期事件由 **Seelex 侧**构建（订阅 jobs.Events() + Snapshot 投影），
// 而不是框架侧经 event.Sink 发——框架侧构造期定不下会话、序号全局，事件 append
// 不到会话事件流的尾部，只能回填。
//
// 用"执行体阻塞在闸门上"把 running 与 completed 分成两段（纯瞬时的作业会被快照投影
// 合并成一个终态事件——投影是**状态变化**，不是逐帧回放），从而确定性地断言：
// 状态投影（running → completed）、会话归属（agent.runtime session_id =
// 作业 Scope.Session）、序号非零（按会话可排序）。
func TestJobsEventStreamProjectsLifecycleIntoSessionLog(t *testing.T) {
	release := make(chan struct{})
	manager, err := jobs.New(jobs.WithExecutor(jobs.ExecutorFunc{
		JobKind: "test",
		Run: func(_ context.Context, _ jobs.Spec, sink jobs.Sink) error {
			sink.Note("running\n")
			<-release
			sink.Complete(jobs.StateDone, "ok")
			return nil
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })

	var mu sync.Mutex
	var appends []frameworkevent.Event
	stream := newJobsEventStream(manager, func() func(context.Context, frameworkevent.Event) error {
		return func(_ context.Context, event frameworkevent.Event) error {
			mu.Lock()
			appends = append(appends, event)
			mu.Unlock()
			return nil
		}
	})
	stream.start()
	t.Cleanup(stream.close)

	statuses := func() map[frameworkevent.Status]bool {
		mu.Lock()
		defer mu.Unlock()
		seen := map[frameworkevent.Status]bool{}
		for _, event := range appends {
			seen[event.Status] = true
		}
		return seen
	}
	await := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			if ok() {
				return
			}
			if time.Now().After(deadline) {
				mu.Lock()
				snapshot := append([]frameworkevent.Event(nil), appends...)
				mu.Unlock()
				t.Fatalf("事件流未投影出 %s：%#v", what, snapshot)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	if _, err := manager.Dispatch(context.Background(), jobs.Spec{
		Kind: "test", Scope: jobs.Scope{Session: "session-1"}, Description: "冒烟作业",
	}); err != nil {
		t.Fatal(err)
	}
	await("running", func() bool { return statuses()[frameworkevent.StatusRunning] })

	close(release)
	await("completed", func() bool { return statuses()[frameworkevent.StatusCompleted] })

	mu.Lock()
	defer mu.Unlock()
	for _, event := range appends {
		if event.Source != jobsEventSource {
			t.Fatalf("source = %q, want %q", event.Source, jobsEventSource)
		}
		if event.Type != frameworkevent.TypeLifecycle {
			t.Fatalf("type = %q, want lifecycle", event.Type)
		}
		sessionID := ""
		for _, location := range event.Locations {
			if location.Kind == "agent.runtime" {
				sessionID = location.IDs["session_id"]
			}
		}
		if sessionID != "session-1" {
			t.Fatalf("事件缺 agent.runtime 会话归属（append 不到会话尾部）：%#v", event.Locations)
		}
		if event.Sequence == 0 {
			t.Fatalf("事件缺序号（按会话不可排序）：%#v", event)
		}
	}
}
