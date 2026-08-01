package seelexctx

import (
	"context"
	"testing"
)

func TestDefaultWindowPolicyFormulaClamps(t *testing.T) {
	policy := NewDefaultWindowPolicy(WindowConfig{})
	// N = clamp((200000 × 0.7 − 10000) ÷ 3000, 4, 40) = clamp(43, 4, 40) = 40。
	rounds, err := policy.WindowRounds(context.Background(), ProviderContextInfo{
		ContextTokens: 200000, AvgRoundTokens: 3000, ReservedTokens: 10000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rounds != 40 {
		t.Fatalf("rounds = %d, want clamped max 40", rounds)
	}
	// 单轮 token 很大 → 商 < MinRounds → 钳制到 MinRounds=4。
	rounds, err = policy.WindowRounds(context.Background(), ProviderContextInfo{
		ContextTokens: 200000, AvgRoundTokens: 100000, ReservedTokens: 10000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rounds != 4 {
		t.Fatalf("rounds = %d, want clamped min 4", rounds)
	}
}

func TestDefaultWindowPolicyConfigOverrideAndFallback(t *testing.T) {
	policy := NewDefaultWindowPolicy(WindowConfig{})
	// 显式配置 rounds 直接覆盖。
	rounds, err := policy.WindowRounds(context.Background(), ProviderContextInfo{
		ContextTokens: 200000, AvgRoundTokens: 3000, ReservedTokens: 10000, ConfigRounds: 12,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rounds != 12 {
		t.Fatalf("rounds = %d, want explicit 12", rounds)
	}
	// 输入缺失 → 保守回退 MinRounds 且显式报错。
	rounds, err = policy.WindowRounds(context.Background(), ProviderContextInfo{})
	if err == nil {
		t.Fatalf("invalid inputs must report an error, got rounds=%d", rounds)
	}
	if rounds != 4 {
		t.Fatalf("fallback rounds = %d, want min 4", rounds)
	}
}

func TestDefaultWindowConfigDecidedDefaults(t *testing.T) {
	defaults := DefaultWindowConfig()
	if defaults.Ratio != 0.7 || defaults.MinRounds != 4 || defaults.MaxRounds != 40 {
		t.Fatalf("decided defaults = %+v", defaults)
	}
}
