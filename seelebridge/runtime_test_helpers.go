package seelebridge

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestRuntime(t testing.TB) *Runtime {
	t.Helper()
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	content := `roles:
  agent:
    - model: test-model
      base_url: http://localhost
      api_key: test-key-not-used
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// ToolCallTimeout 放宽到 30s：注册表 WithCallTimeout 会封顶单次工具调用，
	// 而 scoped bash 测试显式要求 10s 窗口（PowerShell 冷启动在并行测试负载下
	// 可能超过 1s）。工具自身的显式超时仍是生效边界。
	runtime, err := NewRuntime(RuntimeConfig{AccountsPath: path, ToolCallTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	// 测试基座默认 goal skill 激活（plan 工具可见——多数 plan 测试需要）。
	runtime.SetRuntimeVisibilityProjection(RuntimeVisibilityProjection{GoalSkillActive: true})
	return runtime
}
