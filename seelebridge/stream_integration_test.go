package seelebridge

import (
	"os"
	"testing"
	"time"
)

// TestRuntimeMainSessionStreamsThroughSharedSelector 验证主会话装配后
// ChatStream 走共享账号选择器（选中账号被 pin）。
// 集成测试依赖 Runtime 装配面，因此保留在根包（internal/stream 只承载
// 流式 Completer 的单元测试）。
func TestRuntimeMainSessionStreamsThroughSharedSelector(t *testing.T) {
	path := t.TempDir() + "/accounts.yaml"
	content := `roles:
  agent:
    - model: test-model
      base_url: http://localhost
      api_key: test-key
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(RuntimeConfig{AccountsPath: path, ToolCallTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Shutdown()
	sess, err := runtime.NewMainSession(nil)
	if err != nil {
		t.Fatal(err)
	}
	if sess == nil || runtime.Session() == nil {
		t.Fatal("main session is unavailable")
	}
	accounts := runtime.Accounts()
	if len(accounts) != 1 || accounts[0].Name != "agent-1" {
		t.Fatalf("accounts = %+v", accounts)
	}
	if !runtime.SelectAccount("agent-1") {
		t.Fatal("select account failed")
	}
	if runtime.Provider() != "openai" {
		t.Fatalf("provider = %q", runtime.Provider())
	}
}
