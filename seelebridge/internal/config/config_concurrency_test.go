package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// TestRoleDefaultConcurrency verifies the 2026-09-07 role-aware defaults:
// subagent accounts get the framework-unbounded default (far above any real
// fork fan-out) while agent/goalplan stay at one; an explicit per-account
// max_concurrency overrides the role default.
func TestRoleDefaultConcurrency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	content := `
defaults:
  provider: openai
roles:
  agent:
    - model: agent-model
      base_url: https://example.test/v1
      api_key: placeholder-key
  subagent:
    - model: subagent-default
      base_url: https://example.test/v1
      api_key: placeholder-key
    - model: subagent-pinned
      base_url: https://example.test/v1
      api_key: placeholder-key
      max_concurrency: 4
  goalplan:
    - model: goal-model
      base_url: https://example.test/v1
      api_key: placeholder-key
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write accounts fixture: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	byName := make(map[string]model.AccountSpec, len(cfg.Specs))
	for _, spec := range cfg.Specs {
		byName[spec.Name] = spec
	}
	if got := byName["agent-1"].MaxConcurrency; got != 1 {
		t.Fatalf("agent-1 MaxConcurrency = %d, want 1", got)
	}
	if got := byName["goalplan-1"].MaxConcurrency; got != 1 {
		t.Fatalf("goalplan-1 MaxConcurrency = %d, want 1", got)
	}
	if got := byName["subagent-1"].MaxConcurrency; got != defaultSubagentMaxConcurrency {
		t.Fatalf("subagent-1 MaxConcurrency = %d, want default %d", got, defaultSubagentMaxConcurrency)
	}
	if got := byName["subagent-2"].MaxConcurrency; got != 4 {
		t.Fatalf("subagent-2 MaxConcurrency = %d, want explicit 4", got)
	}
}

// TestExplicitZeroConcurrencyRejected guards the upstream accountpool contract
// (Register rejects non-positive concurrency): an explicit zero must fail
// loudly instead of silently producing an account the pool refuses.
func TestExplicitZeroConcurrencyRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	content := `
defaults:
  provider: openai
roles:
  subagent:
    - model: subagent-model
      base_url: https://example.test/v1
      api_key: placeholder-key
      max_concurrency: 0
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write accounts fixture: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted max_concurrency=0, want error")
	}
}
