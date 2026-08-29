package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadTolerantInvalidYAMLReturnsFallbackAndWarning 与真实事故同构：
// websearch 段冒号后缺空格导致整份 YAML 解析失败。容错加载应返回兜底账号
// 配置并把解析错误作为非致命警告，而不是让应用启动失败。
func TestLoadTolerantInvalidYAMLReturnsFallbackAndWarning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	// 与真实事故同构：websearch 段冒号后缺空格，且下一行还有同层键值，
	// yaml.v3 会在下一行报 “mapping values are not allowed in this context”。
	if err := os.WriteFile(path, []byte("websearch:\n  provider:bochaai\n  api_key: sk-dummy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadTolerant(path)
	if err == nil {
		t.Fatal("want non-fatal warning for invalid accounts yaml")
	}
	if !strings.Contains(err.Error(), "parse accounts") {
		t.Fatalf("warning = %v, want parse accounts context", err)
	}
	if len(cfg.Specs) != 1 || cfg.Specs[0].Name != "fallback" {
		t.Fatalf("tolerant load must return fallback specs, got %#v", cfg.Specs)
	}
}

func TestLoadTolerantNoRolesReturnsFallbackAndWarning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	if err := os.WriteFile(path, []byte("websearch:\n  provider: tavily\n  api_key: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadTolerant(path)
	if err == nil {
		t.Fatal("want non-fatal warning when no roles configured")
	}
	if len(cfg.Specs) != 1 || cfg.Specs[0].Name != "fallback" {
		t.Fatalf("tolerant load must return fallback specs, got %#v", cfg.Specs)
	}
}

func TestLoadTolerantMissingFileFallsBackWithoutWarning(t *testing.T) {
	cfg, err := LoadTolerant(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("missing file should fall back silently, got %v", err)
	}
	if len(cfg.Specs) != 1 || cfg.Specs[0].Name != "fallback" {
		t.Fatalf("want fallback specs, got %#v", cfg.Specs)
	}
}
