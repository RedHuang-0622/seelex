package seelebridge

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestSubagentCreatesOwnSession（UC7）：plan 节点 = 独立 SubagentSession——
// NewSubagentSessionWithID 创建独立框架 Session（own loop/历史），注册进
// subagentSessions registry，ID 与节点一致；主会话/其它子代理互不影响。
func TestSubagentCreatesOwnSession(t *testing.T) {
	runtime := newTestRuntimeWithSubagents(t)
	defer runtime.Shutdown()

	first, err := runtime.NewSubagentSessionWithID("node-a", nil)
	if err != nil {
		t.Fatalf("NewSubagentSessionWithID: %v", err)
	}
	if first == nil || first.SessionID() != "node-a" {
		t.Fatalf("subagent session = %v, want ID node-a", first)
	}
	second, err := runtime.NewSubagentSessionWithID("node-b", nil)
	if err != nil {
		t.Fatalf("second subagent: %v", err)
	}
	if second.SessionID() == first.SessionID() {
		t.Fatalf("subagent sessions share ID %q（非独立）", first.SessionID())
	}

	// registry 内可见（own history/视图可读）。
	if got := runtime.SubagentSessionCount(); got != 2 {
		t.Fatalf("subagent session count = %d, want 2", got)
	}
	conversation, ok := runtime.subagentSessions.Conversation("node-a")
	if !ok {
		t.Fatal("node-a conversation should be readable from registry")
	}
	if conversation == nil {
		t.Fatal("node-a conversation must not be nil")
	}

	// 重复创建拒绝（双注册保护）。
	if _, err := runtime.NewSubagentSessionWithID("node-a", nil); err == nil {
		t.Fatal("duplicate subagent session creation must be rejected")
	}

	// 空 ID 拒绝。
	if _, err := runtime.NewSubagentSessionWithID("", nil); err == nil {
		t.Fatal("empty subagent session ID must be rejected")
	}
}

// TestSubagentReclaimAndMergeBack（UC8）：子代理结束经 Unregister 回收
// （导出快照），merge-back 经 Runtime mailbox 回到主会话（DrainSubagentContexts）。
func TestSubagentReclaimAndMergeBack(t *testing.T) {
	runtime := newTestRuntimeWithSubagents(t)
	defer runtime.Shutdown()

	if _, err := runtime.NewSubagentSessionWithID("node-merge", nil); err != nil {
		t.Fatalf("NewSubagentSessionWithID: %v", err)
	}
	// 子代理结论 merge-back 主会话（UC8：经会话端口/Runtime mailbox 写回）。
	runtime.enqueueSubagentContext("node-merge produced finding F1")
	pending := runtime.DrainSubagentContexts()
	if len(pending) != 1 || pending[0] != "node-merge produced finding F1" {
		t.Fatalf("merge-back payloads = %+v", pending)
	}

	// 回收：Unregister 导出结束快照并移除注册。
	snapshot := runtime.subagentSessions.Unregister("node-merge")
	_ = snapshot // 新会话无执行历史时结束快照可为 nil；重点是注册移除
	if got := runtime.SubagentSessionCount(); got != 0 {
		t.Fatalf("subagent session count after unregister = %d, want 0", got)
	}
	if runtime.subagentSessions.Session("node-merge") != nil {
		t.Fatal("unregistered subagent session still live")
	}
}

// TestRuntimeShutdownConcurrentWithSubagentSessions（R5 子代理面）：注册多个
// 子代理会话后并发 Shutdown 可终止、无 panic。
func TestRuntimeShutdownConcurrentWithSubagentSessions(t *testing.T) {
	runtime := newTestRuntimeWithSubagents(t)
	for index := 0; index < 6; index++ {
		if _, err := runtime.NewSubagentSessionWithID(fmt.Sprintf("node-%d", index), nil); err != nil {
			t.Fatalf("create subagent %d: %v", index, err)
		}
	}
	var wg sync.WaitGroup
	for index := 0; index < 4; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runtime.Shutdown()
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("runtime concurrent Shutdown deadlock (timeout)")
	}
}
