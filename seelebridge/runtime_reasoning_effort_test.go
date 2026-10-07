package seelebridge

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/agent/core/api"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// reasoningEffortOfAccount 读回账号池里某个账号当前**实际**会发到 wire 的思考强度
// （走 Seele 客户端的读取口，读的是请求装配真正读的那个字段——不是配置副本）。
func reasoningEffortOfAccount(t testing.TB, runtime *Runtime, name string) string {
	t.Helper()
	for _, entry := range runtime.pool.Entries() {
		if entry.Snapshot.ID != name {
			continue
		}
		client, ok := entry.Value.(*api.ChatClient)
		if !ok {
			t.Fatalf("account %s: 池内动态类型 = %T, want *api.ChatClient", name, entry.Value)
		}
		return client.ReasoningEffort()
	}
	t.Fatalf("account %s not found in pool", name)
	return ""
}

// TestRuntimeSetSessionReasoningEffortOnlyMovesSessionAccounts 钉住 Runtime 侧下发口
// （application/contract.ReasoningEffortPort 的 seelebridge 实现）的两件事：
//
//  1. 档位变化后，**跟随会话**的账号的 wire 思考强度被更新（agent-1 没写
//     reasoning_effort → 角色默认 session）；
//  2. **写死强度**的账号一个字节都不受影响：agent-2 显式 reasoning_effort: low、
//     subagent 默认 low、goalplan 默认 high 都不随会话档位漂移。
//
// 表驱动覆盖四个档 + 空串（空串 = 不下发，用于表达"档位未知就别猜一个值"）。
func TestRuntimeSetSessionReasoningEffortOnlyMovesSessionAccounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	content := `roles:
  agent:
    - model: main-model
      base_url: http://localhost
      api_key: test-key
    - model: pinned-model
      base_url: http://localhost
      api_key: test-key
      reasoning_effort: low
  subagent:
    - model: child-model
      base_url: http://localhost
      api_key: test-key
  goalplan:
    - model: plan-model
      base_url: http://localhost
      api_key: test-key
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(RuntimeConfig{AccountsPath: path, ToolCallTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Shutdown()

	// 建池初值：跟随会话的账号不下发（空串 = provider 用自己的默认），写死强度的
	// 账号直接带上配置值。
	if got := reasoningEffortOfAccount(t, runtime, "agent-1"); got != "" {
		t.Fatalf("agent-1 初值 = %q, want 空（session = 等运行时下发）", got)
	}

	cases := []struct {
		name      string
		effort    string
		wantMoved string
	}{
		{name: "lite 档", effort: dto.ReasoningEffortLow, wantMoved: dto.ReasoningEffortLow},
		{name: "medium 档", effort: dto.ReasoningEffortMedium, wantMoved: dto.ReasoningEffortMedium},
		{name: "high 档", effort: dto.ReasoningEffortHigh, wantMoved: dto.ReasoningEffortHigh},
		{name: "max 档", effort: dto.ReasoningEffortMax, wantMoved: dto.ReasoningEffortMax},
		{name: "空串 = 不下发", effort: "", wantMoved: ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			changed := runtime.SetSessionReasoningEffort(testCase.effort)
			if changed != 1 {
				t.Fatalf("changed = %d, want 1（只有 agent-1 跟随会话）", changed)
			}
			if got := reasoningEffortOfAccount(t, runtime, "agent-1"); got != testCase.wantMoved {
				t.Fatalf("agent-1 wire effort = %q, want %q", got, testCase.wantMoved)
			}
			pinned := map[string]string{
				"agent-2":    dto.ReasoningEffortLow,
				"subagent-1": dto.DefaultSubAgentReasoningEffort,
				"goalplan-1": dto.DefaultGoalPlanReasoningEffort,
			}
			for name, want := range pinned {
				if got := reasoningEffortOfAccount(t, runtime, name); got != want {
					t.Fatalf("写死强度的账号 %s 被会话档位改动了：%q, want %q", name, got, want)
				}
			}
		})
	}
}
