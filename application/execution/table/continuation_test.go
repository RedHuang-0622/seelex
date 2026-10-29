package table

// continuation_test.go —— 续跑信号从**运行时**到**门**的这一段（docs/arch/goal-auto-continue.md
// §2.5/§2.8.2）：回合休止点 NotifyContinuation → 既有 AsyncConsumer → 扫一次门。
//
// 这里钉两件事：信号源真的被接到消费者上（不是只在 deps 结构里躺着），
// 以及 NotifyContinuation 是非阻塞的（回合收尾线程不被队列满拖住）。

import (
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/execution"
)

type continuationGateSpy struct {
	calls    chan string
	returned int
}

func (spy *continuationGateSpy) TriggerGoalContinuations(sessionID string, _ map[string]struct{}) int {
	spy.calls <- sessionID
	return spy.returned
}

func (spy *continuationGateSpy) Status(string) execution.ContinuationStatus {
	return execution.ContinuationStatus{}
}

func TestRuntimeContinuationSignalReachesGateOnce(t *testing.T) {
	spy := &continuationGateSpy{calls: make(chan string, 4)}
	runtime := NewRuntime(RuntimeDeps{Continuation: spy})
	stop := make(chan struct{})
	NewLifecycle(LifecycleDeps{
		Stop: stop,
		Async: AsyncConsumerDeps{
			ContinuationEvents: runtime.continuationEvents,
			TriggerContinuation: func(triggered map[string]struct{}, sessionID string) int {
				return spy.TriggerGoalContinuations(sessionID, triggered)
			},
			RetryDelay: 5 * time.Millisecond,
		},
	}).Start()

	runtime.NotifyContinuation("session-a")
	select {
	case sessionID := <-spy.calls:
		if sessionID != "session-a" {
			t.Fatalf("扫门的会话 = %q", sessionID)
		}
	case <-time.After(time.Second):
		t.Fatal("回合休止点信号没有打到续跑门上")
	}
	// 非阻塞：缓冲满之后继续发也不卡死（丢的是唤醒，不是判据）。
	for index := 0; index < 200; index++ {
		runtime.NotifyContinuation("session-a")
	}
	close(stop)
}

// 未装配续跑门 = 这条通道不存在：不发信号、读面是零值（关闭档行为不变）。
func TestRuntimeWithoutContinuationGateStaysInert(t *testing.T) {
	runtime := NewRuntime(RuntimeDeps{})
	runtime.NotifyContinuation("session-a")
	if status := runtime.ContinuationStatus("session-a"); status.GoalID != "" || status.Stalled {
		t.Fatalf("未装配时读面应为零值：%+v", status)
	}
	if view := runtime.ContinuationView("session-a"); view != nil {
		t.Fatalf("未装配时横条投影应为 nil：%+v", view)
	}
}
