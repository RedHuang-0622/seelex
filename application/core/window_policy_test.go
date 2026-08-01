package core

import (
	"context"
	"os"
	"path/filepath"
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
	// 中间值直接返回。
	rounds, err = policy.WindowRounds(context.Background(), ProviderContextInfo{
		ContextTokens: 200000, AvgRoundTokens: 5000, ReservedTokens: 10000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rounds != 26 { // (140000-10000)/5000 = 26
		t.Fatalf("rounds = %d, want 26", rounds)
	}
}

func TestDefaultWindowPolicyConfigOverrideWins(t *testing.T) {
	policy := NewDefaultWindowPolicy(WindowConfig{})
	// 显式配置 rounds 直接覆盖 provider 推导。
	rounds, err := policy.WindowRounds(context.Background(), ProviderContextInfo{
		ContextTokens: 200000, AvgRoundTokens: 3000, ReservedTokens: 10000, ConfigRounds: 12,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rounds != 12 {
		t.Fatalf("rounds = %d, want explicit 12", rounds)
	}
}

func TestDefaultWindowPolicyFallsBackToMinRounds(t *testing.T) {
	policy := NewDefaultWindowPolicy(WindowConfig{})
	// 输入缺失（上下文窗口 / 每轮估算不可用）→ 保守回退 MinRounds。
	rounds, err := policy.WindowRounds(context.Background(), ProviderContextInfo{})
	if err == nil {
		t.Fatalf("invalid inputs must report an error, got rounds=%d", rounds)
	}
	if rounds != 4 {
		t.Fatalf("fallback rounds = %d, want min 4", rounds)
	}
	// 预算不足以容纳任何轮次 → MinRounds 兜底。
	rounds, err = policy.WindowRounds(context.Background(), ProviderContextInfo{
		ContextTokens: 1000, AvgRoundTokens: 5000, ReservedTokens: 10000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rounds != 4 {
		t.Fatalf("negative headroom rounds = %d, want min 4", rounds)
	}
}

func TestDefaultWindowPolicyUsesConfigRatioAndBounds(t *testing.T) {
	policy := NewDefaultWindowPolicy(WindowConfig{Ratio: 0.5, MinRounds: 2, MaxRounds: 8})
	rounds, err := policy.WindowRounds(context.Background(), ProviderContextInfo{
		ContextTokens: 200000, AvgRoundTokens: 5000, ReservedTokens: 10000,
	})
	if err != nil {
		t.Fatal(err)
	}
	// (200000 × 0.5 − 10000) ÷ 5000 = 18 → 钳制到 8。
	if rounds != 8 {
		t.Fatalf("rounds = %d, want 8", rounds)
	}
	// 部分配置（只给 bounds）→ ratio 回退默认 0.7。
	partial := NewDefaultWindowPolicy(WindowConfig{MinRounds: 2, MaxRounds: 8})
	rounds, err = partial.WindowRounds(context.Background(), ProviderContextInfo{
		ContextTokens: 100000, AvgRoundTokens: 10000, ReservedTokens: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	// (100000 × 0.7) ÷ 10000 = 7（bounds 内）。
	if rounds != 7 {
		t.Fatalf("partial config rounds = %d, want 7", rounds)
	}
}

func TestDefaultWindowConfigDecidedDefaults(t *testing.T) {
	defaults := DefaultWindowConfig()
	if defaults.Ratio != 0.7 || defaults.MinRounds != 4 || defaults.MaxRounds != 40 {
		t.Fatalf("decided defaults = %+v", defaults)
	}
}

func TestLoadWindowConfigParsesSection(t *testing.T) {
	dir := t.TempDir()
	// 文件不存在 → 零值配置（未配置，走默认）。
	config, err := LoadWindowConfig(filepath.Join(dir, "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if config != (WindowConfig{}) {
		t.Fatalf("missing config = %+v, want zero", config)
	}
	// 显式 window 段解析。
	path := filepath.Join(dir, "seele.yaml")
	content := `permission:
  rules: []
window:
  rounds: 10
  ratio: 0.5
  min_rounds: 2
  max_rounds: 20
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err = LoadWindowConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.Rounds != 10 || config.Ratio != 0.5 || config.MinRounds != 2 || config.MaxRounds != 20 {
		t.Fatalf("parsed config = %+v", config)
	}
	// 部分字段 → 未设置字段保持零值（不覆盖默认）。
	partial := `window:
  rounds: 6
`
	if err := os.WriteFile(path, []byte(partial), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err = LoadWindowConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.Rounds != 6 || config.Ratio != 0 || config.MinRounds != 0 || config.MaxRounds != 0 {
		t.Fatalf("partial config = %+v", config)
	}
	// 非法 yaml → 显式报错。
	if err := os.WriteFile(path, []byte("window: [broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadWindowConfig(path); err == nil {
		t.Fatal("invalid yaml must fail explicitly")
	}
}
