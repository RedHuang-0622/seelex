package limit

import (
	"testing"
	"time"
)

// 零值 = 关：整块缺失 / 结构零值都关着，关着不带任何隐式默认。
func TestContinuationZeroValueStaysOff(t *testing.T) {
	if (Limits{}).WithDefaults().Continuation.On() {
		t.Fatal("结构零值被悄悄打开：漏配 continuation 块就等于自动续跑，回滚臂会失效")
	}
	if off := (ContinuationLimits{}); off.On() {
		t.Fatal("ContinuationLimits 零值必须是关（与 async_exec 同一套纪律）")
	}
}

// 上限/退避的零值兜底是硬数，不能读成"无限"。
func TestContinuationAttemptsAndBackoffClamp(t *testing.T) {
	off := ContinuationLimits{}
	if got := off.Attempts(); got != DefaultContinuationMaxAttempts {
		t.Fatalf("Attempts() = %d, want %d", got, DefaultContinuationMaxAttempts)
	}
	if got := off.MaxBackoff(); got != DefaultContinuationMaxBackoffSec*time.Second {
		t.Fatalf("MaxBackoff() = %v", got)
	}
	// 首次续跑不退避（设计 §4.1 拍板：先给一次"接着干"）。
	first := ContinuationLimits{BackoffSec: 30}
	if delay := first.Backoff(0); delay != 0 {
		t.Fatalf("首次续跑退避 = %v, want 0", delay)
	}
	// 第 2 次起指数退避并封顶。
	limited := ContinuationLimits{BackoffSec: 30, MaxBackoffSec: 100}
	cases := []struct {
		done int
		want time.Duration
	}{
		{1, 30 * time.Second},
		{2, 60 * time.Second},
		{3, 100 * time.Second}, // 120s 被 100s 封顶
		{9, 100 * time.Second},
	}
	for _, testCase := range cases {
		if got := limited.Backoff(testCase.done); got != testCase.want {
			t.Fatalf("Backoff(%d) = %v, want %v", testCase.done, got, testCase.want)
		}
	}
	// 显式不退避（backoff_sec=0）也不无限：上限仍然是硬拦。
	noBackoff := ContinuationLimits{MaxAttemptsPerGoal: 1}
	if delay := noBackoff.Backoff(5); delay != 0 {
		t.Fatalf("backoff_sec=0 时退避 = %v, want 0", delay)
	}
}

func TestContinuationValidateRejectsNegative(t *testing.T) {
	if err := (ContinuationLimits{Enabled: true}).Validate(); err != nil {
		t.Fatalf("零值必须校验通过：%v", err)
	}
	for _, bad := range []ContinuationLimits{
		{MaxAttemptsPerGoal: -1},
		{BackoffSec: -1},
		{MaxBackoffSec: -1},
	} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("%+v 应校验失败", bad)
		}
	}
}
