package seelexctx

import "testing"

// TestRetainedContextTokensMinOfTwoWindows 钉住压缩保留窗口的决策规则：
// 保留的上下文前缀 token 预算 = min(token1, token2) ——
//
//	token1 = 配置里的硬编码保留窗口 token 数（window.retain_tokens；未配置回退账号窗口）
//	token2 = 占比窗口 = ratio × 全量上下文 token 数
//
// 窗口外部分尽数交给 compact_context 折叠（不进 provider 请求前缀）。
func TestRetainedContextTokensMinOfTwoWindows(t *testing.T) {
	cases := []struct {
		name         string
		all          int
		retainTokens int
		fallback     int
		ratio        float64
		want         int
		wantReason   string
	}{
		{
			name: "token1 紧：配置保留窗口 token 数获胜", all: 300_000,
			retainTokens: 120_000, fallback: 200_000, ratio: 0.7, want: 120_000,
			wantReason: "min(120000, 0.7×300000=210000)",
		},
		{
			name: "token2 紧：占比窗口获胜", all: 100_000,
			retainTokens: 120_000, fallback: 200_000, ratio: 0.7, want: 70_000,
			wantReason: "min(120000, 70000)",
		},
		{
			name: "retain_tokens 未配置 → token1 回退账号上下文窗口", all: 300_000,
			retainTokens: 0, fallback: 200_000, ratio: 0.7, want: 200_000,
			wantReason: "min(200000, 210000)",
		},
		{
			name: "retain_tokens 未配置 且 占比更紧", all: 200_000,
			retainTokens: 0, fallback: 200_000, ratio: 0.7, want: 140_000,
			wantReason: "min(200000, 140000)",
		},
		{
			name: "ratio 未配置 → 用默认比例 0.7", all: 200_000,
			retainTokens: 0, fallback: 200_000, ratio: 0, want: 140_000,
			wantReason: "WithDefaults 补 ratio=0.7",
		},
		{
			name: "两候选同值", all: 100_000,
			retainTokens: 70_000, fallback: 0, ratio: 0.7, want: 70_000,
			wantReason: "min(70000, 70000)",
		},
		{
			name: "ratio > 1 时钳制到全量上下文", all: 100_000,
			retainTokens: 300_000, fallback: 0, ratio: 1.5, want: 100_000,
			wantReason: "min(300000, 150000) 超过全量 → 钳到 100000",
		},
		{
			name: "全量上下文不可用 → 不决策", all: 0,
			retainTokens: 120_000, fallback: 200_000, ratio: 0.7, want: 0,
			wantReason: "调用方保持原目标",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			config := WindowConfig{RetainTokens: testCase.retainTokens, Ratio: testCase.ratio}
			got := config.RetainedContextTokens(testCase.all, testCase.fallback)
			if got != testCase.want {
				t.Fatalf("RetainedContextTokens(all=%d, fallback=%d) = %d, want %d (%s)",
					testCase.all, testCase.fallback, got, testCase.want, testCase.wantReason)
			}
			if got > testCase.all {
				t.Fatalf("保留前缀 %d 超过全量上下文 %d", got, testCase.all)
			}
		})
	}
}

// TestWindowConfigWithDefaultsKeepsConfiguredKnobs：补默认值只补"未配置"的
// 比例/轮数上下限，不覆盖已配置的 rounds 与 retain_tokens。
func TestWindowConfigWithDefaultsKeepsConfiguredKnobs(t *testing.T) {
	applied := WindowConfig{Rounds: 12, RetainTokens: 90_000, ForceCompactTokens: 150_000}.WithDefaults()
	if applied.Rounds != 12 || applied.RetainTokens != 90_000 || applied.ForceCompactTokens != 150_000 {
		t.Fatalf("已配置字段被覆盖：%+v", applied)
	}
	if applied.Ratio != 0.7 || applied.MinRounds != 4 || applied.MaxRounds != 40 {
		t.Fatalf("未配置字段未补默认值：%+v", applied)
	}
	if (WindowConfig{}).WithDefaults() != DefaultWindowConfig() {
		t.Fatalf("零值配置应等价于默认配置")
	}
}

// TestWindowConfigMustCompact：硬压缩阈值（force_compact_tokens）——全量上下文
// token 数达到即必须自主压缩；未配置时压缩只由软策略驱动。
func TestWindowConfigMustCompact(t *testing.T) {
	configured := WindowConfig{ForceCompactTokens: 150_000}
	cases := []struct {
		name   string
		config WindowConfig
		all    int
		want   bool
	}{
		{name: "达到硬阈值", config: configured, all: 150_000, want: true},
		{name: "超过硬阈值", config: configured, all: 200_000, want: true},
		{name: "未达硬阈值", config: configured, all: 149_999, want: false},
		{name: "未配置硬阈值", config: WindowConfig{}, all: 1_000_000, want: false},
		{name: "全量上下文不可用", config: configured, all: 0, want: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.config.MustCompact(testCase.all); got != testCase.want {
				t.Fatalf("MustCompact(%d) = %v, want %v (hard=%d)",
					testCase.all, got, testCase.want, testCase.config.ForceCompactTokens)
			}
		})
	}
}
