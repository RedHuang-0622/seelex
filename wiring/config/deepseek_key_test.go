package config

import (
	"os"
	"path/filepath"
	"testing"
)

// 账号档取 DeepSeek 密钥：判据（base_url 域名 / model 前缀）、角色顺序、容错。

func writeAccounts(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write accounts: %v", err)
	}
	return path
}

func TestDeepSeekSearchKeyByBaseURL(t *testing.T) {
	path := writeAccounts(t, `
roles:
  subagent:
    - model: gpt-x
      base_url: https://api.openai.com/v1
      api_key: sk-openai
  agent:
    - model: deepseek-v4
      base_url: https://api.deepseek.com/v1
      api_key: sk-deepseek-agent
`)
	// agent 排在 subagent 之前（偏好顺序），所以拿到的是 agent 那条。
	if got := deepSeekSearchKeyFrom(path); got != "sk-deepseek-agent" {
		t.Fatalf("DeepSeek key = %q, want sk-deepseek-agent", got)
	}
}

func TestDeepSeekSearchKeyByModelPrefix(t *testing.T) {
	path := writeAccounts(t, `
roles:
  goalplan:
    - model: DeepSeek-V4
      api_key: sk-by-model
`)
	if got := deepSeekSearchKeyFrom(path); got != "sk-by-model" {
		t.Fatalf("DeepSeek key = %q, want sk-by-model", got)
	}
}

func TestDeepSeekSearchKeySkipsEmptyKey(t *testing.T) {
	path := writeAccounts(t, `
roles:
  agent:
    - model: deepseek-v4
      base_url: https://api.deepseek.com/v1
      api_key: ""
  worker:
    - model: deepseek-v4
      base_url: https://api.deepseek.com/v1
      api_key: sk-worker
`)
	// agent 那条 key 是空的，不能当成"取到了"。
	if got := deepSeekSearchKeyFrom(path); got != "sk-worker" {
		t.Fatalf("DeepSeek key = %q, want sk-worker", got)
	}
}

func TestDeepSeekSearchKeyNoMatchOrUnreadable(t *testing.T) {
	if got := deepSeekSearchKeyFrom(""); got != "" {
		t.Fatalf("empty path must return empty, got %q", got)
	}
	if got := deepSeekSearchKeyFrom(filepath.Join(t.TempDir(), "missing.yaml")); got != "" {
		t.Fatalf("missing file must return empty, got %q", got)
	}
	path := writeAccounts(t, "roles:\n  agent:\n    - model: gpt-x\n      base_url: https://api.openai.com/v1\n      api_key: sk-openai\n")
	if got := deepSeekSearchKeyFrom(path); got != "" {
		t.Fatalf("no DeepSeek account must return empty, got %q", got)
	}
}
