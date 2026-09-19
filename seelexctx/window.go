// WindowPolicy 滑动窗口 N 的确定机制（plan.md §3.7.3 / 架构文档 §4.8.2）。
//
// N 表示保留在 provider 请求中的最新完整轮次数：窗口内轮次原样保留，
// 窗口外是唯一允许被压缩的部分。N 由配置 + provider 推导决定（非魔法数字）：
// 显式配置 rounds > provider 推导（clamp）> 出错时保守回退 MinRounds。
package seelexctx

import (
	"context"
	"math"
)

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
	Rounds int     `yaml:"rounds"`
	Ratio  float64 `yaml:"ratio"`
	// RetainTokens 是硬编码在配置里的「上下文保留窗口 token 数」（token1）。
	// 0 = 未配置 → 回退调用方给定的绝对数（通常 = 账号上下文窗口
	// context_window）。压缩后保留的上下文前缀 = min(token1, token2)，
	// 见 RetainedContextTokens。
	RetainTokens int `yaml:"retain_tokens"`
	// ForceCompactTokens 是硬压缩阈值：全量上下文 token 数（all_context，
	// 含已压入 compact_context 的部分）达到该值即**必须**自主压缩，不再等
	// provider 比例阈值。0 = 未配置（只用软压缩：比例阈值 + 保留窗口规则）。
	ForceCompactTokens int `yaml:"force_compact_tokens"`
	MinRounds          int `yaml:"min_rounds"`
	MaxRounds          int `yaml:"max_rounds"`
}

// DefaultWindowConfig 返回确认点 5 的既定默认值：ratio=0.7、min_rounds=4、
// max_rounds=40（retain_tokens 无默认值：0 = 未配置）。默认值只在这里定义
// （配置默认），决策代码不硬编码常量。
func DefaultWindowConfig() WindowConfig {
	return WindowConfig{Ratio: 0.7, MinRounds: 4, MaxRounds: 40}
}

// WithDefaults 用既定默认值补齐未配置字段（零值 = 未配置），
// 已配置字段（rounds / retain_tokens）原样保留。
func (config WindowConfig) WithDefaults() WindowConfig {
	defaults := DefaultWindowConfig()
	if config.Ratio <= 0 {
		config.Ratio = defaults.Ratio
	}
	if config.MinRounds <= 0 {
		config.MinRounds = defaults.MinRounds
	}
	if config.MaxRounds <= 0 {
		config.MaxRounds = defaults.MaxRounds
	}
	return config
}

// RetainedContextTokens 决定保留在 provider 请求中的上下文前缀 token 预算，
// 规则 = 两个窗口取较小者 min(token1, token2)：
//
//	token1 = RetainTokens：硬编码在配置里的「上下文保留窗口 token 数」；
//	         未配置（<= 0）时取 fallbackTokens（通常 = 账号上下文窗口）。
//	token2 = Ratio × allContextTokens：占比窗口（全量上下文 × 可用比例）。
//
// 未配置（<= 0）的候选不参与比较；两个候选都不可用时全量保留（比值只增不
// 缩的场景）。返回值 ∈ [1, allContextTokens]；allContextTokens <= 0 时返回 0
// （调用方不做窗口决策，保持原目标）。窗口外部分（allContextTokens − 返回值）
// 尽数交给 compact_context 折叠，原始轮次仍在会话存储里按引用回读。
func (config WindowConfig) RetainedContextTokens(allContextTokens, fallbackTokens int) int {
	if allContextTokens <= 0 {
		return 0
	}
	config = config.WithDefaults()
	configuredTokens := config.RetainTokens
	if configuredTokens <= 0 {
		configuredTokens = fallbackTokens
	}
	ratioTokens := 0
	if config.Ratio > 0 {
		ratioTokens = int(float64(allContextTokens) * config.Ratio)
	}
	var retained int
	switch {
	case configuredTokens > 0 && ratioTokens > 0:
		retained = min(configuredTokens, ratioTokens)
	case configuredTokens > 0:
		retained = configuredTokens
	case ratioTokens > 0:
		retained = ratioTokens
	default:
		retained = allContextTokens
	}
	if retained > allContextTokens {
		retained = allContextTokens
	}
	if retained < 1 {
		retained = 1
	}
	return retained
}

// MustCompact 报告全量上下文 token 数（all_context）是否达到硬压缩阈值
// window.force_compact_tokens：达到即必须自主压缩（硬策略），与 provider
// 比例阈值无关；未配置（<= 0）时返回 false，压缩只由软策略（比例阈值 +
// 保留窗口 min(token1, token2)）驱动。
func (config WindowConfig) MustCompact(allContextTokens int) bool {
	if config.ForceCompactTokens <= 0 || allContextTokens <= 0 {
		return false
	}
	return allContextTokens >= config.ForceCompactTokens
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

	// 1. 显式配置 rounds 直接覆盖（审计 R4 修正：配置段 window.rounds
	// 此前未被消费——WindowRounds 只读 info.ConfigRounds，而运行时
	// ProviderContextInfo.ConfigRounds 恒 0，配置对象 Rounds 是死接线）。
	if policy.Config.Rounds > 0 {
		return policy.Config.Rounds, nil
	}
	// info.ConfigRounds 兼容其他提供方通道（ContextWindowPolicy 等）。
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
	rawRounds := available / float64(info.AvgRoundTokens)
	nearestRound := math.Round(rawRounds)
	if math.Abs(rawRounds-nearestRound) < 1e-9 {
		rawRounds = nearestRound
	}
	rounds := int(rawRounds)
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
