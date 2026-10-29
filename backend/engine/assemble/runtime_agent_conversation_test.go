package assemble

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// 2026-10-29：用户实测「给他发消息他不收、没有及时反馈」。后半段根因是子代理这条
// 会话写面把「前端愿意等回执多久」当成了「回合能跑多久」：WaitMS 直接 context.WithTimeout
// 包住 ChatStream，于是回合跑到预算点被掐断，而回执还报 accepted+timed_out——前端只能
// 对一个已经死掉的回合轮询。
//
// 这两条用例钉住的就是那个语义：等待预算到点只代表"这次没等到"，不代表把回合杀掉。
func TestRunDetachedWithBudgetTimeoutDoesNotKillRun(t *testing.T) {
	var finished atomic.Bool
	started := make(chan struct{})

	reply, timedOut, err := runDetachedWithBudget(context.Background(), 30, func(context.Context) (string, error) {
		close(started)
		time.Sleep(120 * time.Millisecond)
		finished.Store(true)
		return "回合后续产出", nil
	})
	if err != nil {
		t.Fatalf("超时不该是错误：%v", err)
	}
	if !timedOut {
		t.Fatal("回合比等待预算慢时必须报 timedOut")
	}
	if reply != "" {
		t.Fatalf("timedOut 时不该带回复，实际 %q", reply)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("run 根本没启动")
	}
	// 关键断言：等待预算用完之后，回合必须继续跑完（不是被取消）。
	deadline := time.Now().Add(2 * time.Second)
	for !finished.Load() {
		if time.Now().After(deadline) {
			t.Fatal("等待预算到点后回合被取消了：这正是「他不收」的根因")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRunDetachedWithBudgetReturnsReplyInsideBudget(t *testing.T) {
	reply, timedOut, err := runDetachedWithBudget(context.Background(), 2000, func(context.Context) (string, error) {
		return "快回合", nil
	})
	if err != nil {
		t.Fatalf("不该有错误：%v", err)
	}
	if timedOut {
		t.Fatal("预算内完成的回合不该报 timedOut")
	}
	if reply != "快回合" {
		t.Fatalf("回复丢失：%q", reply)
	}
}

func TestRunDetachedWithBudgetSurvivesRPCContextCancel(t *testing.T) {
	// 桥面 RPC 的 ctx 会随一次调用结束而取消（关闭弹窗、切页）。取消这次 RPC 不能顺手
	// 杀掉已经收下消息的回合——否则用户看到的是"发出去了，却永远没有下文"。
	var finished atomic.Bool
	ctx, cancel := context.WithCancel(context.Background())

	// waitMS 很小：一定先返回 timedOut，此时回合仍在跑。
	_, timedOut, err := runDetachedWithBudget(ctx, 20, func(runCtx context.Context) (string, error) {
		select {
		case <-runCtx.Done():
			return "", runCtx.Err()
		case <-time.After(200 * time.Millisecond):
		}
		finished.Store(true)
		return "拖过 RPC 的回合", nil
	})
	if err != nil || !timedOut {
		t.Fatalf("应先拿到超时回执：timedOut=%v err=%v", timedOut, err)
	}
	// 模拟这次 RPC 结束：取消外层 ctx。
	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for !finished.Load() {
		if time.Now().After(deadline) {
			t.Fatal("外层 ctx 取消把回合带走了：消息收下了却永远没有下文")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
