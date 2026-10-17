package task_context

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/prompt"
)

// D1 的红→绿对照：一条**安静**的长命令（还在跑，但没有新输出）被反复取回时，
// 载荷必须逐字节相同（含时间戳就每轮白烧一次前缀缓存），于是语义进展 epoch 不动。
// 修复前这里直接累加"无进展轮次"，几轮就把还在跑的回合判死；修复后判据改成
// "这一轮是否有在途后台执行被查询"。
//
// 空转由谁封顶：tool-call / tool-round 两个上限（见 budget 里的另两轴）——
// 本用例把它们的预算给足，只留无进展这一轴，才能单独看它。
func TestQuietAsyncPollingIsNotCountedAsNoProgress(t *testing.T) {
	cases := []struct {
		name       string
		pending    int
		wantStop   bool
		wantReason string
	}{
		{"无在途执行仍按原判据终止", 0, true, "no observable progress"},
		{"有在途后台执行不得被判死", 1, false, ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			sessionID := "sess-async"
			requestID := "req-async"
			coordinator := NewCoordinator(Deps{
				CurrentSessionID: func() string { return sessionID },
				AsyncPending:     func(string) int { return testCase.pending },
			})
			ctx := WithSessionID(context.Background(), sessionID)

			coordinator.stateMu.Lock()
			runtime := coordinator.sessionStateLocked(sessionID)
			// 进展计数固定在同一个 epoch：模拟"两次取回逐字节相同"。
			runtime.taskExecution = &TaskExecutionState{RequestID: requestID, ProgressEpoch: 7}
			coordinator._StartReActBudgetForLocked(sessionID, requestID, prompt.ReActBudget{
				MaxToolRounds: 0, MaxToolCalls: 0, MaxNoProgressRounds: 3,
			})
			allowed := true
			for turn := range 10 {
				if !coordinator._AllowNextReActIteration(ctx, turn) {
					allowed = false
					break
				}
			}
			reason := ""
			if err := coordinator._ReActBudgetError(requestID); err != nil {
				reason = err.Error()
			}
			coordinator.stateMu.Unlock()

			if allowed == testCase.wantStop {
				t.Fatalf("allowed=%v，want 停止=%v（reason=%q）", allowed, testCase.wantStop, reason)
			}
			if testCase.wantReason != "" && !strings.Contains(reason, testCase.wantReason) {
				t.Fatalf("终止原因 = %q，want 含 %q", reason, testCase.wantReason)
			}
			if testCase.wantReason == "" && reason != "" {
				t.Fatalf("还在跑的后台命令被判停: %q", reason)
			}
		})
	}
}

// 未注入在途读面（能力关闭 / 测试桩）时，判据必须退回原行为——不得因为
// "读不到"就当有在途执行。
func TestNoAsyncReaderFallsBackToPlainNoProgressBudget(t *testing.T) {
	coordinator := NewCoordinator(Deps{CurrentSessionID: func() string { return "sess-x" }})
	ctx := WithSessionID(context.Background(), "sess-x")
	coordinator.stateMu.Lock()
	defer coordinator.stateMu.Unlock()
	runtime := coordinator.sessionStateLocked("sess-x")
	runtime.taskExecution = &TaskExecutionState{RequestID: "req-x", ProgressEpoch: 4}
	coordinator._StartReActBudgetForLocked("sess-x", "req-x", prompt.ReActBudget{MaxNoProgressRounds: 2})

	// 第一轮只登记基线 epoch（此时还没有"重复"可言），之后每轮各计一次：
	// 上限 2 → 最迟第 3 轮必须停。
	stopped := false
	for turn := range 3 {
		if !coordinator._AllowNextReActIteration(ctx, turn) {
			stopped = true
			break
		}
	}
	if !stopped {
		t.Fatal("没有 AsyncPending 读面时却一路放行（等于无进展预算被整体关掉）")
	}
	if err := coordinator._ReActBudgetError("req-x"); err == nil ||
		!strings.Contains(err.Error(), "no observable progress") {
		t.Fatalf("终止原因不符: %v", err)
	}
}
