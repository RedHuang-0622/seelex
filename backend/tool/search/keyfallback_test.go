package search

import "testing"

// 兜底密钥测试：只补空、不覆盖、不碰标准协议策略。

func TestApplyKeyFallbackFillsMissingBuiltinKey(t *testing.T) {
	cfg := WebSearchConfig{
		MaxResults: 5,
		Strategies: []StrategyConfig{
			{Name: "ds", Type: "deepseek"},
		},
	}
	got := ApplyKeyFallback(cfg, func(vendor string) string {
		if vendor != "deepseek" {
			t.Fatalf("unexpected vendor %q", vendor)
		}
		return "sk-from-accounts"
	})
	if got.Strategies[0].APIKey != "sk-from-accounts" {
		t.Fatalf("expected fallback key, got %q", got.Strategies[0].APIKey)
	}
	// 不就地改写调用方的配置。
	if cfg.Strategies[0].APIKey != "" {
		t.Fatalf("ApplyKeyFallback must not mutate the input config, got %q", cfg.Strategies[0].APIKey)
	}
}

func TestApplyKeyFallbackNeverOverridesExplicitKey(t *testing.T) {
	cfg := WebSearchConfig{
		Strategies: []StrategyConfig{
			{Name: "ds", Type: "deepseek", APIKey: "sk-explicit"},
		},
	}
	called := false
	got := ApplyKeyFallback(cfg, func(string) string {
		called = true
		return "sk-from-accounts"
	})
	if got.Strategies[0].APIKey != "sk-explicit" {
		t.Fatalf("explicit key must win, got %q", got.Strategies[0].APIKey)
	}
	if called {
		t.Fatal("fallback must not be consulted when the key is already set")
	}
}

func TestApplyKeyFallbackSkipsStandardStrategies(t *testing.T) {
	cfg := WebSearchConfig{
		Strategies: []StrategyConfig{
			{Name: "gateway", Endpoint: "https://gateway.example/search"},
			{Name: "std", Type: "standard", Endpoint: "https://gateway.example/search"},
		},
	}
	got := ApplyKeyFallback(cfg, func(vendor string) string {
		t.Fatalf("standard strategies have no vendor, but fallback got %q", vendor)
		return "sk-nope"
	})
	for i, strategy := range got.Strategies {
		if strategy.APIKey != "" {
			t.Fatalf("strategy %d must stay keyless, got %q", i, strategy.APIKey)
		}
	}
}

func TestApplyKeyFallbackLegacyProviderShorthand(t *testing.T) {
	got := ApplyKeyFallback(WebSearchConfig{Provider: "deepseek"}, func(vendor string) string {
		if vendor != "deepseek" {
			t.Fatalf("unexpected vendor %q", vendor)
		}
		return "sk-legacy"
	})
	if got.APIKey != "sk-legacy" {
		t.Fatalf("expected legacy shorthand to receive the fallback key, got %q", got.APIKey)
	}
	// 简写补齐后能真的装配出策略（否则兜底等于没接上）。
	strategy, err := Assemble(got)
	if err != nil {
		t.Fatalf("Assemble after fallback: %v", err)
	}
	if _, ok := strategy.(*deepSeekStrategy); !ok {
		t.Fatalf("expected *deepSeekStrategy, got %T", strategy)
	}
}

func TestApplyKeyFallbackNilIsIdentity(t *testing.T) {
	cfg := WebSearchConfig{Provider: "deepseek"}
	if got := ApplyKeyFallback(cfg, nil); got.APIKey != "" {
		t.Fatalf("nil fallback must change nothing, got %q", got.APIKey)
	}
}
