package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

func writeAccountsYAML(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestLoadResolvesTemperaturePerAccount 钉住"每个账号独立配置"：
// 显式写了 0 的账号拿 0（温度用指针区分"没写"和"写了 0"，0 是合法值——
// DeepSeek 官方对编码/数学的推荐值就是 0.0）；没写的账号继承 defaults。
func TestLoadResolvesTemperaturePerAccount(t *testing.T) {
	path := writeAccountsYAML(t, `
defaults:
  provider: openai
  temperature: 0.3
roles:
  agent:
    - model: m
      base_url: https://example.invalid
      api_key: k
      temperature: 0
  subagent:
    - model: m
      base_url: https://example.invalid
      api_key: k
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := model.ResolveAccountSpec(cfg.Specs, model.RoleAgent)
	if err != nil {
		t.Fatal(err)
	}
	if agent.Temperature != 0 {
		t.Fatalf("explicit temperature 0 must survive (got %v)", agent.Temperature)
	}
	subagent, err := model.ResolveAccountSpec(cfg.Specs, model.RoleSubAgent)
	if err != nil {
		t.Fatal(err)
	}
	if subagent.Temperature != 0.3 {
		t.Fatalf("account without temperature must inherit defaults (got %v)", subagent.Temperature)
	}
}

// TestLoadDefaultsTemperatureWhenUnset：两级都没写 → 代码默认。
func TestLoadDefaultsTemperatureWhenUnset(t *testing.T) {
	path := writeAccountsYAML(t, `
defaults:
  provider: openai
roles:
  agent:
    - model: m
      base_url: https://example.invalid
      api_key: k
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := model.ResolveAccountSpec(cfg.Specs, model.RoleAgent)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Temperature != model.DefaultTemperature {
		t.Fatalf("temperature = %v, want code default %v", spec.Temperature, model.DefaultTemperature)
	}
}

// TestLoadRejectsTemperatureOutOfRange：范围外的温度在装载期就报错，而不是把
// 一个 provider 会拒的值带到运行期。
func TestLoadRejectsTemperatureOutOfRange(t *testing.T) {
	path := writeAccountsYAML(t, `
defaults:
  provider: openai
roles:
  agent:
    - model: m
      base_url: https://example.invalid
      api_key: k
      temperature: 3
`)
	if _, err := Load(path); err == nil {
		t.Fatal("temperature 3 must be rejected at load time")
	}
}
