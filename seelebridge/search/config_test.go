package search

import (
	"os"
	"path/filepath"
	"testing"
)

// 夹具写的是**根级字段**：search_engine.yaml 不再套 `websearch:` 段
// （段名跟着文件走，独立文件里再套一层只是把每行埋深两格）。
func writeSearchEngine(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_Defaults(t *testing.T) {
	cfg := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if cfg.Provider != "" {
		t.Errorf("expected no default provider (engine-agnostic), got %q", cfg.Provider)
	}
	if cfg.MaxResults != 5 {
		t.Errorf("expected default max_results 5, got %d", cfg.MaxResults)
	}
	if !cfg.IncludeAnswer {
		t.Error("expected IncludeAnswer default true")
	}
	if cfg.SearchDepth != "advanced" {
		t.Errorf("expected default search_depth 'advanced', got %q", cfg.SearchDepth)
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	path := writeSearchEngine(t, "{{{invalid yaml")
	cfg := Load(path)
	if cfg.Provider != "" {
		t.Errorf("expected no default provider, got %q", cfg.Provider)
	}
}

func TestLoad_PartialOverride(t *testing.T) {
	path := writeSearchEngine(t, `
api_key: "sk-test-key"
max_results: 10
`)
	cfg := Load(path)
	if cfg.Provider != "" {
		t.Errorf("expected no provider override, got %q", cfg.Provider)
	}
	if cfg.APIKey != "sk-test-key" {
		t.Errorf("expected API key 'sk-test-key', got %q", cfg.APIKey)
	}
	if cfg.MaxResults != 10 {
		t.Errorf("expected max_results 10, got %d", cfg.MaxResults)
	}
	// include_answer 未显式配置时必须保留默认 true（修复历史无条件覆盖为 false）。
	if !cfg.IncludeAnswer {
		t.Error("expected IncludeAnswer to keep default true when not configured")
	}
	if cfg.SearchDepth != "advanced" {
		t.Errorf("expected default search_depth 'advanced', got %q", cfg.SearchDepth)
	}
}

func TestLoad_FullOverride(t *testing.T) {
	path := writeSearchEngine(t, `
provider: "tavily"
api_key: "sk-test-key"
max_results: 3
include_answer: false
search_depth: "basic"
timeout: 20
`)
	cfg := Load(path)
	if cfg.Provider != "tavily" {
		t.Errorf("expected provider 'tavily', got %q", cfg.Provider)
	}
	if cfg.APIKey != "sk-test-key" {
		t.Errorf("expected API key 'sk-test-key', got %q", cfg.APIKey)
	}
	if cfg.MaxResults != 3 {
		t.Errorf("expected max_results 3, got %d", cfg.MaxResults)
	}
	if cfg.IncludeAnswer {
		t.Error("expected IncludeAnswer false")
	}
	if cfg.SearchDepth != "basic" {
		t.Errorf("expected search_depth 'basic', got %q", cfg.SearchDepth)
	}
	if cfg.TimeoutSeconds != 20 {
		t.Errorf("expected timeout 20, got %d", cfg.TimeoutSeconds)
	}
}

func TestLoad_EmptyAPIKeyKeepsDefault(t *testing.T) {
	path := writeSearchEngine(t, `
api_key: ""
max_results: 8
`)
	cfg := Load(path)
	if cfg.APIKey != "" {
		t.Errorf("expected empty API key, got %q", cfg.APIKey)
	}
	if cfg.MaxResults != 8 {
		t.Errorf("expected max_results 8, got %d", cfg.MaxResults)
	}
}

func TestLoad_ZeroMaxResultsKeepsDefault(t *testing.T) {
	path := writeSearchEngine(t, `
max_results: 0
`)
	cfg := Load(path)
	if cfg.MaxResults != 5 {
		t.Errorf("expected default max_results 5, got %d", cfg.MaxResults)
	}
}

func TestLoad_Strategies(t *testing.T) {
	path := writeSearchEngine(t, `
active: "searxng"
strategies:
  - name: searxng
    endpoint: https://searx.example.org/search
    api_key: replace-with-key
`)
	cfg := Load(path)
	if cfg.Active != "searxng" {
		t.Errorf("expected active 'searxng', got %q", cfg.Active)
	}
	if len(cfg.Strategies) != 1 {
		t.Fatalf("expected 1 strategy, got %d", len(cfg.Strategies))
	}
	sc := cfg.Strategies[0]
	if sc.Name != "searxng" || sc.Endpoint != "https://searx.example.org/search" || sc.APIKey != "replace-with-key" {
		t.Errorf("unexpected strategy: %+v", sc)
	}
}

func TestLoad_StrategiesApikeyAlias(t *testing.T) {
	path := writeSearchEngine(t, `
strategies:
  - name: my-search
    endpoint: https://search.example.org/search
    apikey: alias-key
`)
	cfg := Load(path)
	if len(cfg.Strategies) != 1 {
		t.Fatalf("expected 1 strategy, got %d", len(cfg.Strategies))
	}
	if cfg.Strategies[0].APIKey != "alias-key" {
		t.Errorf("expected apikey alias to map to APIKey, got %q", cfg.Strategies[0].APIKey)
	}
}

func TestLoad_StrategiesType(t *testing.T) {
	path := writeSearchEngine(t, `
strategies:
  - name: bocha
    type: bochaai
    api_key: sk-bocha
  - name: local
    endpoint: https://search.example.org/search
    api_key: key
`)
	cfg := Load(path)
	if len(cfg.Strategies) != 2 {
		t.Fatalf("expected 2 strategies, got %d", len(cfg.Strategies))
	}
	if cfg.Strategies[0].Type != "bochaai" || cfg.Strategies[0].APIKey != "sk-bocha" {
		t.Errorf("unexpected first strategy: %+v", cfg.Strategies[0])
	}
	// 未声明 type 的策略保持零值（装配时走标准协议）。
	if cfg.Strategies[1].Type != "" {
		t.Errorf("expected empty type for standard strategy, got %q", cfg.Strategies[1].Type)
	}
}
