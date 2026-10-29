package limit

// continuation.go — goal 未收口 + 被硬中断时的自动续跑开关块。
//
// 纪律照抄 AsyncExecLimits（见 limits.go 同名字段注释）：
//   - **零值 / 整块缺失 = 关**，关就是关（一次都不扫、一个回合都不起，
//     不静默降级成"只记日志"）；出厂档要显式写 `continuation.enabled: true`。
//   - 关闭是回滚臂：把这一位改回去，所有续跑行为（含 ⏸/⟳ 控制面）一起消失，
//     不需要改代码。
//
// 设计（含暂停/继续语义、防呆四条、审计落点）：docs/arch/goal-auto-continue.md。

import (
	"fmt"
	"time"
)

// ContinuationLimits 是自动续跑的开关与上限块（limits.continuation）。
type ContinuationLimits struct {
	// Enabled 是整块能力的开关（缺省 false）。
	Enabled bool `yaml:"enabled"`
	// MaxAttemptsPerGoal 是**同一 goal 连续续跑**的次数上限。触顶后记一条
	// `truncated` 打点就停（不静默重试、不降级成"再试一次"）。
	//
	// 0/负值 = 用 DefaultContinuationMaxAttempts（**不是**"无限"）：上限是
	// 硬不变量，漏写这个数不能让宿主无限烧钱。
	// 人工恢复（GUI 的 ⟳ / Resume）会把计数清零，但必须同时记一条
	// `goal.resume`（manual=true）打点——重置是有痕的动作。
	MaxAttemptsPerGoal int `yaml:"max_attempts_per_goal"`
	// BackoffSec 是**第 2 次及以后**续跑前的退避基数（秒）：
	// 第 n 次（n≥2）等待 BackoffSec × 2^(n-2)，封顶 MaxBackoffSec。
	// **第 1 次不退避**（0 延迟立即续），这是设计 §4.1 的产品拍板：
	// 用户刚被中断，先给一次"接着干"，失败后才拉开间隔。
	BackoffSec int `yaml:"backoff_sec"`
	// MaxBackoffSec 是退避封顶（秒）。0/负值 = DefaultContinuationMaxBackoffSec。
	MaxBackoffSec int `yaml:"max_backoff_sec"`
}

const (
	// DefaultContinuationMaxAttempts 是 MaxAttemptsPerGoal 的零值兜底。
	DefaultContinuationMaxAttempts = 3
	// DefaultContinuationMaxBackoffSec 是 MaxBackoffSec 的零值兜底（10 分钟）。
	DefaultContinuationMaxBackoffSec = 600
)

// On 报告自动续跑是否打开（零值 = 关）。
func (c ContinuationLimits) On() bool { return c.Enabled }

// Attempts 返回有效次数上限（0/负值 → 兜底常量）。
func (c ContinuationLimits) Attempts() int {
	if c.MaxAttemptsPerGoal > 0 {
		return c.MaxAttemptsPerGoal
	}
	return DefaultContinuationMaxAttempts
}

// MaxBackoff 返回有效退避封顶。
func (c ContinuationLimits) MaxBackoff() time.Duration {
	seconds := c.MaxBackoffSec
	if seconds <= 0 {
		seconds = DefaultContinuationMaxBackoffSec
	}
	return time.Duration(seconds) * time.Second
}

// Backoff 返回"已经续过 attemptsDone 次之后，下一次续跑前要等多久"。
//
//   - attemptsDone <= 0：0（首次立即续，见 BackoffSec 注释）；
//   - BackoffSec <= 0：0（显式不退避——上限仍然是硬拦，见 Attempts）；
//   - 其余：BackoffSec × 2^(attemptsDone-1)，封顶 MaxBackoff。
func (c ContinuationLimits) Backoff(attemptsDone int) time.Duration {
	if attemptsDone <= 0 || c.BackoffSec <= 0 {
		return 0
	}
	maxBackoff := c.MaxBackoff()
	delay := time.Duration(c.BackoffSec) * time.Second
	for i := 1; i < attemptsDone; i++ {
		delay *= 2
		if delay >= maxBackoff {
			return maxBackoff
		}
	}
	if delay > maxBackoff {
		return maxBackoff
	}
	return delay
}

// Validate 校验显式写下的数字（启动期调用；零值合法 = 用兜底）。
// 拼错/负数显式失败，不静默降级成"从不续跑"。
func (c ContinuationLimits) Validate() error {
	if c.MaxAttemptsPerGoal < 0 {
		return fmt.Errorf("limits.continuation.max_attempts_per_goal 不能为负（%d）", c.MaxAttemptsPerGoal)
	}
	if c.BackoffSec < 0 {
		return fmt.Errorf("limits.continuation.backoff_sec 不能为负（%d）", c.BackoffSec)
	}
	if c.MaxBackoffSec < 0 {
		return fmt.Errorf("limits.continuation.max_backoff_sec 不能为负（%d）", c.MaxBackoffSec)
	}
	return nil
}
