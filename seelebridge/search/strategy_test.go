package search

import (
	"context"
	"strings"
	"testing"
)

func TestAssemble_NoStrategies(t *testing.T) {
	_, err := Assemble(WebSearchConfig{})
	if err == nil {
		t.Fatal("expected error when no strategies configured")
	}
	if !strings.Contains(err.Error(), "未配置任何 websearch 代理策略") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAssemble_LegacyTavilyWithoutKey(t *testing.T) {
	_, err := Assemble(WebSearchConfig{Provider: "tavily"})
	if err == nil {
		t.Fatal("expected error for empty API key")
	}
	if err.Error() != "web_search: API Key 未配置" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAssemble_LegacyTavilyOnlyAPIKey(t *testing.T) {
	// 只配置 api_key、未声明 strategies/provider 时，按历史行为走 tavily 兼容策略。
	p, err := Assemble(WebSearchConfig{APIKey: "sk-test"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "tavily" {
		t.Fatalf("expected name 'tavily', got %q", p.Name())
	}
}

func TestAssemble_MinimalStrategy(t *testing.T) {
	cfg := WebSearchConfig{
		MaxResults: 5,
		Strategies: []StrategyConfig{
			{Name: "my-search", Endpoint: "https://search.example.org", APIKey: "key-1"},
		},
	}
	p, err := Assemble(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "my-search" {
		t.Fatalf("expected name 'my-search', got %q", p.Name())
	}
}

func TestAssemble_ActiveSelection(t *testing.T) {
	cfg := WebSearchConfig{
		Active: "second",
		Strategies: []StrategyConfig{
			{Name: "first", Endpoint: "https://a", APIKey: "k1"},
			{Name: "second", Endpoint: "https://b", APIKey: "k2"},
		},
	}
	p, err := Assemble(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "second" {
		t.Fatalf("expected active strategy 'second', got %q", p.Name())
	}
}

func TestAssemble_ActiveNotFound(t *testing.T) {
	cfg := WebSearchConfig{
		Active: "nope",
		Strategies: []StrategyConfig{
			{Name: "first", Endpoint: "https://a", APIKey: "k1"},
		},
	}
	_, err := Assemble(cfg)
	if err == nil || !strings.Contains(err.Error(), "未找到代理策略") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func TestAssemble_MissingEndpoint(t *testing.T) {
	cfg := WebSearchConfig{
		Strategies: []StrategyConfig{
			{Name: "no-endpoint", APIKey: "k1"},
		},
	}
	_, err := Assemble(cfg)
	if err == nil || !strings.Contains(err.Error(), "缺少 endpoint") {
		t.Fatalf("expected missing endpoint error, got %v", err)
	}
}

func TestLimitResults(t *testing.T) {
	if got := limitResults(5, 0); got != 5 {
		t.Errorf("0 -> %d, want 5", got)
	}
	if got := limitResults(5, -1); got != 5 {
		t.Errorf("-1 -> %d, want 5", got)
	}
	if got := limitResults(5, 20); got != 5 {
		t.Errorf("20 -> %d, want 5", got)
	}
	if got := limitResults(5, 3); got != 3 {
		t.Errorf("3 -> %d, want 3", got)
	}
}

func TestWebSearch_NoStrategy(t *testing.T) {
	_, err := WebSearch(context.Background(), WebSearchConfig{}, "test query", 5)
	if err == nil {
		t.Fatal("expected error when no strategy configured")
	}
}
