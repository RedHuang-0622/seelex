package core

import (
	"context"
	"errors"
	"testing"
)

// forkGateRuntime 模拟 seelebridge Runtime 的 fork 执行标记面。
type forkGateRuntime struct {
	*fakeRuntime
	forkOn bool
}

func (runtime *forkGateRuntime) ForkInFlight(string) bool { return runtime.forkOn }

// TestSubmitRejectedWhileForkRunning 钉住“fork 运行时禁止同会话继续对话”
// 的门控：fork_subagents 执行中 Submit 返回 ErrForkRunningChat（含排队输入），
// fork 结束后恢复。
func TestSubmitRejectedWhileForkRunning(t *testing.T) {
	runtime := &forkGateRuntime{fakeRuntime: &fakeRuntime{}}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))
	defer service.Shutdown()

	runtime.forkOn = true
	if err := service.Submit(context.Background(), "continue while fork runs"); !errors.Is(err, ErrForkRunningChat) {
		t.Fatalf("submit while fork running = %v, want ErrForkRunningChat", err)
	}
	runtime.forkOn = false
	if err := service.Submit(context.Background(), "after fork finished"); err != nil {
		t.Fatalf("submit after fork finished: %v", err)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
}
