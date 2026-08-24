package search

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_Defaults(t *testing.T) {
	cfg := LoadConfig(filepath.Join(t.TempDir(), "missing.yaml"))
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

func TestLoadConfig_InvalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid_ws.yaml")
	if err := os.WriteFile(path, []byte("{{{invalid yaml"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := LoadConfig(path)
	if cfg.Provider != "" {
		t.Errorf("expected no default provider, got %q", cfg.Provider)
	}
}

func TestLoadConfig_PartialOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial_ws.yaml")
	content := `
websearch:
  api_key: "sk-test-key"
  max_results: 10
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := LoadConfig(path)
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

func TestLoadConfig_FullOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "full_ws.yaml")
	content := `
websearch:
  provider: "tavily"
  api_key: "sk-test-key"
  max_results: 3
  include_answer: false
  search_depth: "basic"
  timeout: 20
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := LoadConfig(path)
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

func TestLoadConfig_EmptyAPIKeyKeepsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty_key_ws.yaml")
	content := `
websearch:
  api_key: ""
  max_results: 8
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := LoadConfig(path)
	if cfg.APIKey != "" {
		t.Errorf("expected empty API key, got %q", cfg.APIKey)
	}
	if cfg.MaxResults != 8 {
		t.Errorf("expected max_results 8, got %d", cfg.MaxResults)
	}
}

func TestLoadConfig_ZeroMaxResultsKeepsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zero_max_ws.yaml")
	content := `
websearch:
  max_results: 0
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := LoadConfig(path)
	if cfg.MaxResults != 5 {
		t.Errorf("expected default max_results 5, got %d", cfg.MaxResults)
	}
}

func TestLoadConfig_Strategies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "strategies_ws.yaml")
	content := `
websearch:
  active: "searxng"
  strategies:
    - name: searxng
      endpoint: https://searx.example.org/search
      api_key: replace-with-key
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := LoadConfig(path)
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

func TestLoadConfig_StrategiesApikeyAlias(t *testing.T) {
	path := filepath.Join(t.TempDir(), "strategies_alias_ws.yaml")
	content := `
websearch:
  strategies:
    - name: my-search
      endpoint: https://search.example.org/search
      apikey: alias-key
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := LoadConfig(path)
	if len(cfg.Strategies) != 1 {
		t.Fatalf("expected 1 strategy, got %d", len(cfg.Strategies))
	}
	if cfg.Strategies[0].APIKey != "alias-key" {
		t.Errorf("expected apikey alias to map to APIKey, got %q", cfg.Strategies[0].APIKey)
	}
}
