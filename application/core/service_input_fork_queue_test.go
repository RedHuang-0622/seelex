package core

import (
	"context"
	"testing"
)

// forkQueueRuntime 模拟 seelebridge Runtime 的"fork 在飞"读面（ForkInFlight 恒真）。
//
// 退役前这个读面就是**输入门控**的判据：fork_subagents 在跑 → Submit 返回
// ErrForkRunningChat。退役后它只是**观察面**（有几个子代理作业在跑），不再挡输入。
type forkQueueRuntime struct {
	*fakeRuntime
	forkOn bool
}

func (runtime *forkQueueRuntime) ForkInFlight(string) bool { return runtime.forkOn }

// TestSubmitQueuedWhileForkRunning（2026-10-05 退役 fork 门控）钉住新链路：fork/subagent
// 作业在飞时提交**照常收下**，走**既有那条排队链路**——本会话有回合在跑就进队列，回合
// 收尾整批提升为下一轮。退役前这里被 ErrForkRunningChat 顶回来（"子代理在跑你就别说话"）。
func TestSubmitQueuedWhileForkRunning(t *testing.T) {
	engine := &blockingEngine{fakeEngine: &fakeEngine{}, blockCh: make(chan struct{})}
	runtime := &forkQueueRuntime{fakeRuntime: &fakeRuntime{}, forkOn: true}
	service := mustNew(t, Dependencies{
		Engine:   engine,
		Runtime:  runtime,
		Plugins:  &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills:   fakeSkills{},
		Sessions: fakeSessions{},
	})
	t.Cleanup(service.Shutdown)

	if err := service.Submit(context.Background(), "first"); err != nil {
		t.Fatalf("Submit(first): %v", err)
	}
	waitForSnapshot(t, service, func(snapshot Snapshot) bool { return snapshot.Chat.Running })

	// fork 在飞：提交必须收下并排队（不再拒绝）。
	if err := service.Submit(context.Background(), "continue while fork runs"); err != nil {
		t.Fatalf("fork 在跑时 Submit = %v, want nil（输入进队列，不拒绝）", err)
	}
	snapshot := waitForSnapshot(t, service, func(snapshot Snapshot) bool { return snapshot.Chat.QueuedCount == 1 })
	if len(snapshot.Chat.InputQueue) != 1 || snapshot.Chat.InputQueue[0] != "continue while fork runs" {
		t.Fatalf("排队投影 = %v, want [continue while fork runs]", snapshot.Chat.InputQueue)
	}

	// 回合收尾：排队内容整批提升为下一轮（既有链路，本改动没碰它）。
	close(engine.blockCh)
	waitForSnapshot(t, service, func(snapshot Snapshot) bool { return snapshot.Chat.QueuedCount == 0 })
}
