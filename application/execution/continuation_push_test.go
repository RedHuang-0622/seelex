package execution

// continuation_push_test.go —— push 通道的真实取消面（硬判据：push 立刻停掉在飞的
// **子会话**，不等轮边界；主会话自己这一轮不在它的射程内）。
//
// 两个方向都钉：在飞的子会話当场 ctx 被 cancel 并进返回值；已结束的子会话不被
// 谎报成"停掉了"。docs/arch/goal-auto-continue.md §2.8.3。

import (
	"context"
	"testing"

	sessionstate "github.com/RedHuang-0622/seelex/application/session/state"
	model "github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestInterruptChildSessionsCancelsRunningChildrenOnly(t *testing.T) {
	domain := sessionstate.NewDomain()
	defer domain.Close()

	childCtx, cancelChild := context.WithCancel(context.Background())
	running, err := sessionstate.NewSessionUnit("role-session-a")
	if err != nil {
		t.Fatal(err)
	}
	running.SetChatState(model.ChatState{Running: true, RequestID: "role-req"}, nil)
	running.SetCancel(cancelChild)
	domain.Register(running)

	idle, err := sessionstate.NewSessionUnit("role-session-b")
	if err != nil {
		t.Fatal(err)
	}
	domain.Register(idle)

	interrupt := InterruptChildSessions(
		func(mainSessionID string) ([]string, error) {
			if mainSessionID != "session-a" {
				t.Fatalf("子会话目录按主会话查：%q", mainSessionID)
			}
			return []string{"role-session-a", "role-session-b", "role-session-missing"}, nil
		},
		func(sessionIDs []string) []string { return sessionstate.CancelRunningTurns(domain, sessionIDs) },
	)

	interrupted := interrupt("session-a")
	if len(interrupted) != 1 || interrupted[0] != "role-session-a" {
		t.Fatalf("被打断的子会话 = %v，期望只有在飞的 role-session-a", interrupted)
	}
	select {
	case <-childCtx.Done():
	default:
		t.Fatal("在飞的子会话回合没有被立刻取消（push 必须当场生效，不等轮边界）")
	}
}

// 没有子会话目录读面 = 没有可用中断通道：返回 nil，不声称停掉了谁（不静默假装成功）。
func TestInterruptChildSessionsWithoutDirectoryReportsNothing(t *testing.T) {
	interrupt := InterruptChildSessions(nil, func([]string) []string { return []string{"x"} })
	if got := interrupt("session-a"); got != nil {
		t.Fatalf("没有目录读面却报告停掉了 %v", got)
	}
}
