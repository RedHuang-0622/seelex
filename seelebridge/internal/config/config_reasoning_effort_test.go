package config

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// specFor 取某个角色的账号规格（测试里反复要用）。
func specFor(t *testing.T, cfg Config, role model.AccountRole) model.AccountSpec {
	t.Helper()
	spec, err := model.ResolveAccountSpec(cfg.Specs, role)
	if err != nil {
		t.Fatalf("resolve %s: %v", role, err)
	}
	return spec
}

// TestLoadReasoningEffortRoleDefaults 钉住"什么都没配"时的角色分档：
// agent 跟随会话档位、subagent 低、goalplan 高。
//
// 这三条是**策略**，不是一个全局常数能表达的——所以它们必须在装载期就被补成
// 具体值，而不是把空串带到运行期让每个调用点各猜一次。
func TestLoadReasoningEffortRoleDefaults(t *testing.T) {
	path := writeAccountsYAML(t, `
defaults:
  provider: openai
roles:
  agent:
    - model: m
      base_url: https://example.invalid
      api_key: k
  subagent:
    - model: m
      base_url: https://example.invalid
      api_key: k
  goalplan:
    - model: m
      base_url: https://example.invalid
      api_key: k
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		role model.AccountRole
		want string
	}{
		{model.RoleAgent, dto.ReasoningEffortSession},
		{model.RoleSubAgent, dto.DefaultSubAgentReasoningEffort},
		{model.RoleGoalPlan, dto.DefaultGoalPlanReasoningEffort},
	}
	for _, c := range cases {
		if got := specFor(t, cfg, c.role).ReasoningEffort; got != c.want {
			t.Errorf("%s reasoning_effort = %q, want %q", c.role, got, c.want)
		}
	}
}

// TestLoadReasoningEffortPrecedence 钉住三级优先序：账号级 > defaults > 角色默认。
// 第二行与第三行是同一件事的两面：defaults 一旦显式出现，它会盖掉**角色默认**
// （goalplan 拿到 medium 而不是自己的 high），但盖不掉账号级显式值。
func TestLoadReasoningEffortPrecedence(t *testing.T) {
	path := writeAccountsYAML(t, `
defaults:
  provider: openai
  reasoning_effort: medium
roles:
  agent:
    - model: m
      base_url: https://example.invalid
      api_key: k
  subagent:
    - model: m
      base_url: https://example.invalid
      api_key: k
      reasoning_effort: high
  goalplan:
    - model: m
      base_url: https://example.invalid
      api_key: k
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := specFor(t, cfg, model.RoleAgent).ReasoningEffort; got != dto.ReasoningEffortMedium {
		t.Errorf("defaults must fill the agent role: got %q", got)
	}
	if got := specFor(t, cfg, model.RoleSubAgent).ReasoningEffort; got != dto.ReasoningEffortHigh {
		t.Errorf("account level must win over defaults: got %q", got)
	}
	if got := specFor(t, cfg, model.RoleGoalPlan).ReasoningEffort; got != dto.ReasoningEffortMedium {
		t.Errorf("defaults must win over the goalplan role default: got %q", got)
	}
}

// TestLoadRejectsInvalidReasoningEffort：不认识的强度在装载期就报错，
// 而不是把一个 provider 会拒的值带到线上（那会变成一次运行期 400）。
func TestLoadRejectsInvalidReasoningEffort(t *testing.T) {
	path := writeAccountsYAML(t, `
defaults:
  provider: openai
roles:
  agent:
    - model: m
      base_url: https://example.invalid
      api_key: k
      reasoning_effort: hgih
`)
	if _, err := Load(path); err == nil {
		t.Fatal("reasoning_effort hgih must be rejected at load time")
	}
}
