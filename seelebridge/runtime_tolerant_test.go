package seelebridge

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNewRuntimeToleratesInvalidAccountsConfig 保证 accounts.yaml 损坏时
// Runtime 仍能装配（内置兜底账号），并把解析错误作为启动警告暴露给上层，
// 供 GUI/TUI 展示而不是启动即退出。
func TestNewRuntimeToleratesInvalidAccountsConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	if err := os.WriteFile(path, []byte("websearch:\n  provider:bochaai\n  api_key: sk-dummy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(RuntimeConfig{AccountsPath: path})
	if err != nil {
		t.Fatalf("NewRuntime should tolerate invalid accounts yaml, got: %v", err)
	}
	defer runtime.Shutdown()
	if warnings := runtime.StartupWarnings(); len(warnings) == 0 {
		t.Fatal("want startup warning for invalid accounts yaml")
	}
	if runtime.Model() == "" {
		t.Fatal("fallback account should provide a model")
	}
}
