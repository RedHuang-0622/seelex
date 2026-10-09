package seelebridge

// 红灯复现（用户口径 2026-10-04）：装配层压缩进行中，LLM 读后感为空（模型返回空内容）
// 或读后感调用失败（402 余额不足 / 其它运行时失败）时，生产推帧入口
// Runtime.PushCompactionFrame 仍然把 chapter2 降级出的**本地帧**压进会话压缩栈——
// 栈顶被一帧没有模型读后感的帧占掉，违反「只有出了读后感才更新 compact stack top」。
//
// 期望契约（本文件钉死）：
//  1. LLM 读后感为空/调用失败 → PushCompactionFrame **报错返回**，错误携带真实原因
//     （402 的原文 / 其它失败的原文 / 空读后感），**不归档、不推帧**，压缩栈原样；
//  2. ReadbackCompactionSummary（读数闸）同样**报错返回**而不是交回 source=local 的
//     回执——调用方（context_runtime 的 noSummary 闸）需要的是失败原因，不是一份
//     伪装成功的本地回执；
//  3. 读后感 LLM 调用**不得带时间限制**（无 deadline）：大上下文的重放要跑多久就跑
//     多久，失败靠错误返回表达，不靠超时截断。
//
// 开关关闭（no-summarizer）是另一回事：那是结构配置事实、模型从未被叫到，它的
// 降级帧语义由 TestCompactionSummarySwitchClosedKeepsLocalCompact 单独钉住，不在
// 本文件口径内。

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
)

// readbackGateCompleter 是读数闸/推帧这一跳的确定性 completer：记录调用次数与
// 「ctx 是否带了 deadline」（要求 3 的钉子），err / reply 参数化三种失败形态。
type readbackGateCompleter struct {
	mu          sync.Mutex
	calls       int
	sawDeadline bool
	err         error
	reply       string
}

func (c *readbackGateCompleter) Complete(ctx context.Context, _ []types.Message, _ []types.Tool) (types.Message, error) {
	c.mu.Lock()
	c.calls++
	if _, ok := ctx.Deadline(); ok {
		c.sawDeadline = true
	}
	c.mu.Unlock()
	if c.err != nil {
		return types.Message{}, c.err
	}
	reply := c.reply
	return types.Message{Role: "assistant", Content: &reply}, nil
}

func (c *readbackGateCompleter) recorded() (calls int, sawDeadline bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls, c.sawDeadline
}

// TestPushCompactionFrameWithEmptyReadbackRefusesStackFrame：读后感为空/失败 →
// 推帧入口必须报错返回且栈原样。三种形态：402 余额不足、其它调用失败、模型返回空读后感。
func TestPushCompactionFrameWithEmptyReadbackRefusesStackFrame(t *testing.T) {
	cases := []struct {
		name      string
		completer *readbackGateCompleter
		wantErr   []string
	}{
		{
			name:      "402余额不足",
			completer: &readbackGateCompleter{err: errors.New("ChatClient stream: HTTP 402 payment required (insufficient balance)")},
			wantErr:   []string{"402", "payment required"},
		},
		{
			name:      "其他调用失败",
			completer: &readbackGateCompleter{err: errors.New("connect: connection refused")},
			wantErr:   []string{"connect: connection refused"},
		},
		{
			name:      "模型返回空读后感",
			completer: &readbackGateCompleter{reply: "   "},
			wantErr:   []string{"empty summary"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newCompactionSwitchFixtureWith(t, true, testCase.completer)
			receipt, err := fixture.runtime.PushCompactionFrame(context.Background(), fixture.session, CompactionFrameRequest{
				Overflow:      compactionSwitchOverflow(),
				ReplayHistory: compactionSwitchReplayHistory(),
			})
			if err == nil {
				t.Fatalf("读后感为空/失败时推帧必须报错返回，实际 err=nil receipt=%+v", receipt)
			}
			for _, want := range testCase.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("错误必须携带真实失败原因 %q，实际 %q", want, err.Error())
				}
			}
			if stack := fixture.store.Snapshot().CompactStack; len(stack) != 0 {
				t.Fatalf("没有模型读后感就不得更新压缩栈，实际栈有 %d 帧（栈顶 %+v）", len(stack), stack[len(stack)-1])
			}
			if calls, sawDeadline := testCase.completer.recorded(); sawDeadline {
				t.Fatalf("读后感 LLM 调用不得带时间限制（ctx 带了 deadline），调用 %d 次", calls)
			}
		})
	}
}

// TestReadbackCompactionSummaryReturnsReadbackFailure：读数闸同口径——LLM 读后感
// 拿不到时**报错返回**（错误带真实原因），而不是交回 source=local 的"伪成功"回执；
// 读数闸保持无副作用（不归档、不压栈）。
func TestReadbackCompactionSummaryReturnsReadbackFailure(t *testing.T) {
	cases := []struct {
		name      string
		completer *readbackGateCompleter
		wantErr   []string
	}{
		{
			name:      "402余额不足",
			completer: &readbackGateCompleter{err: errors.New("ChatClient stream: HTTP 402 payment required (insufficient balance)")},
			wantErr:   []string{"402", "payment required"},
		},
		{
			name:      "其他调用失败",
			completer: &readbackGateCompleter{err: errors.New("connect: connection refused")},
			wantErr:   []string{"connect: connection refused"},
		},
		{
			name:      "模型返回空读后感",
			completer: &readbackGateCompleter{reply: "   "},
			wantErr:   []string{"empty summary"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newCompactionSwitchFixtureWith(t, true, testCase.completer)
			receipt, err := fixture.runtime.ReadbackCompactionSummary(context.Background(), fixture.session, CompactionFrameRequest{
				Overflow:      compactionSwitchOverflow(),
				ReplayHistory: compactionSwitchReplayHistory(),
			})
			if err == nil {
				t.Fatalf("读数闸拿不到模型读后感必须报错返回，实际 err=nil receipt=%+v", receipt)
			}
			for _, want := range testCase.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("错误必须携带真实失败原因 %q，实际 %q", want, err.Error())
				}
			}
			if receipt.SummarySource == "replay" {
				t.Fatalf("读数失败不得报成 replay 成功：%+v", receipt)
			}
			if stack := fixture.store.Snapshot().CompactStack; len(stack) != 0 {
				t.Fatalf("读数闸无副作用：不得压栈，实际栈有 %d 帧", len(stack))
			}
			if _, sawDeadline := testCase.completer.recorded(); sawDeadline {
				t.Fatal("读后感 LLM 调用不得带时间限制（ctx 带了 deadline）")
			}
		})
	}
}

// TestPushCompactionFrameReadbackHasNoDeadline：成功臂的时间限制钉子——读后感
// 调用不带 deadline（重放跑多久就跑多久），成功时照常推帧（replay 帧进栈）。
func TestPushCompactionFrameReadbackHasNoDeadline(t *testing.T) {
	completer := &readbackGateCompleter{reply: compactionSwitchReply}
	fixture := newCompactionSwitchFixtureWith(t, true, completer)
	receipt, err := fixture.runtime.PushCompactionFrame(context.Background(), fixture.session, CompactionFrameRequest{
		Overflow:      compactionSwitchOverflow(),
		ReplayHistory: compactionSwitchReplayHistory(),
	})
	if err != nil {
		t.Fatalf("PushCompactionFrame: %v", err)
	}
	if receipt.SummarySource != "replay" {
		t.Fatalf("成功臂 summary_source = %q，want replay", receipt.SummarySource)
	}
	if _, sawDeadline := completer.recorded(); sawDeadline {
		t.Fatal("读后感 LLM 调用不得带时间限制（ctx 带了 deadline）")
	}
}
