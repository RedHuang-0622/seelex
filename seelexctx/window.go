// WindowPolicy 滑动窗口 N 的确定机制（plan.md §3.7.3 / 架构文档 §4.8.2）。
//
// N 表示保留在 provider 请求中的最新完整轮次数：窗口内轮次原样保留，
// 窗口外是唯一允许被压缩的部分。N 由配置 + provider 推导决定（非魔法数字）：
// 显式配置 rounds > provider 推导（clamp）> 出错时保守回退 MinRounds。
package seelexctx

import "context"

// WindowPolicy 决定滑动窗口轮数 N。
type WindowPolicy interface {
	WindowRounds(ctx context.Context, info ProviderContextInfo) (int, error)
}

// ProviderContextInfo 携带窗口决策的全部输入。
type ProviderContextInfo struct {
	ContextTokens  int // provider/model 上下文窗口
	AvgRoundTokens int // token_counter 按最近完整单元估算
	ReservedTokens int // system prompt + 栈块固定预留
	ConfigRounds   int // 用户显式配置 window.rounds（0 = 未配置）
}

// WindowConfig 是 seele.yaml 的 window 配置段。零值字段表示"未配置"，
// 回退到既定默认值（确认点 5）。
type WindowConfig struct {
	Rounds    int     `yaml:"rounds"`
	Ratio     float64 `yaml:"ratio"`
	MinRounds int     `yaml:"min_rounds"`
	MaxRounds int     `yaml:"max_rounds"`
}

// DefaultWindowConfig 返回确认点 5 的既定默认值：ratio=0.7、min_rounds=4、
// max_rounds=40。默认值只在这里定义（配置默认），决策代码不硬编码常量。
func DefaultWindowConfig() WindowConfig {
	return WindowConfig{Ratio: 0.7, MinRounds: 4, MaxRounds: 40}
}

// DefaultWindowPolicy 是 provider 推导策略：
//
//	N = clamp((ContextTokens × Ratio − Reserved) ÷ AvgRoundTokens, MinRounds, MaxRounds)
//
// 决策顺序（plan.md §3.7.3）：显式配置 rounds > provider 推导 > 保守回退。
type DefaultWindowPolicy struct {
	Config WindowConfig // 合并视图；零值字段回退默认
}

// NewDefaultWindowPolicy 从 window 配置段构建策略。零值字段回退默认值。
func NewDefaultWindowPolicy(config WindowConfig) DefaultWindowPolicy {
	return DefaultWindowPolicy{Config: config}
}

// WindowRounds 按决策顺序计算 N。
func (policy DefaultWindowPolicy) WindowRounds(_ context.Context, info ProviderContextInfo) (int, error) {
	defaults := DefaultWindowConfig()

	// 1. 显式配置 rounds 直接覆盖。
	if info.ConfigRounds > 0 {
		return info.ConfigRounds, nil
	}

	ratio := policy.Config.Ratio
	if ratio <= 0 {
		ratio = defaults.Ratio
	}
	minRounds := policy.Config.MinRounds
	if minRounds <= 0 {
		minRounds = defaults.MinRounds
	}
	maxRounds := policy.Config.MaxRounds
	if maxRounds <= 0 {
		maxRounds = defaults.MaxRounds
	}

	// 2. provider 推导；输入缺失时保守回退 MinRounds（返回错误供审计）。
	if info.ContextTokens <= 0 || info.AvgRoundTokens <= 0 {
		return minRounds, errWindowPolicyUnavailable
	}
	available := float64(info.ContextTokens)*ratio - float64(info.ReservedTokens)
	if available <= 0 {
		return minRounds, nil
	}
	rounds := int(available / float64(info.AvgRoundTokens))
	if rounds < minRounds {
		rounds = minRounds
	}
	if rounds > maxRounds {
		rounds = maxRounds
	}
	return rounds, nil
}

// errWindowPolicyUnavailable 表示 provider 上下文窗口或每轮估算不可用，
// 决策回退 MinRounds 并显式报告（审计用）。
var errWindowPolicyUnavailable = &windowPolicyError{"provider context or round estimate unavailable"}

type windowPolicyError struct{ reason string }

func (e *windowPolicyError) Error() string {
	return "window policy: " + e.reason
}
