package seelebridge

import (
	"context"
	"os"
	"strings"
	"testing"
)

// ── CommandSandbox（切片 10，docs/2026-07-28-project-session-scope/sandbox-research.md）──

// TestScrubEnvironmentRemovesCredentials 验证凭据环境变量清洗：
// API key/secret/token 类变量不传给子进程，基础变量保留。
func TestScrubEnvironmentRemovesCredentials(t *testing.T) {
	environ := []string{
		"PATH=/usr/bin",
		"OPENAI_API_KEY=sk-secret",
		"ANTHROPIC_API_KEY=sk-ant-secret",
		"AWS_SECRET_ACCESS_KEY=aws-secret",
		"MY_TOKEN=token-value",
		"PASSWORD=plain",
		"SystemRoot=C:\\Windows",
		"HOME=/home/user",
		"CREDENTIAL_STORE=x",
	}
	scrubbed := scrubEnvironment(environ)
	for _, want := range []string{"PATH", "SystemRoot", "HOME"} {
		found := false
		for _, entry := range scrubbed {
			if strings.HasPrefix(entry, want+"=") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("basic variable %q must be preserved, scrubbed = %v", want, scrubbed)
		}
	}
	for _, forbidden := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "AWS_SECRET_ACCESS_KEY", "MY_TOKEN", "PASSWORD", "CREDENTIAL_STORE"} {
		for _, entry := range scrubbed {
			if strings.HasPrefix(entry, forbidden+"=") {
				t.Errorf("credential variable %q must be scrubbed, got %v", forbidden, scrubbed)
			}
		}
	}
}

// TestSandboxPrepareScrubsEnvAndSetsRoot 验证 native sandbox Prepare：
// cwd = 项目根、env 清洗凭据但透传本地环境（PATH 等基础变量保留）、
// 能力报告正确（cwd-gate 非 OS 级 + 环境透传契约）。
func TestSandboxPrepareScrubsEnvAndSetsRoot(t *testing.T) {
	// 注入凭据变量验证子进程看不到。
	_ = os.Setenv("SEELEX_TEST_API_KEY", "leak-check")
	defer os.Unsetenv("SEELEX_TEST_API_KEY")

	sandbox := newNativeProjectCWD()
	cmd, caps, err := sandbox.Prepare(context.Background(), "C:\\project", "echo hi", 30)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Dir != "C:\\project" {
		t.Fatalf("cmd.Dir = %q, want project root", cmd.Dir)
	}
	// 环境透传契约：PATH 等基础变量原样保留（本地工具链可用），凭据已清洗。
	pathFound := false
	for _, entry := range cmd.Env {
		if strings.HasPrefix(entry, "SEELEX_TEST_API_KEY=") {
			t.Fatalf("credential env leaked into command: %v", cmd.Env)
		}
		if strings.HasPrefix(entry, "PATH=") {
			pathFound = true
		}
	}
	if !pathFound {
		t.Fatalf("PATH must be passed through (local toolchain availability), env = %v", cmd.Env)
	}
	if caps.Isolation != "cwd-gate" || !caps.EnvScrubbed || !caps.EnvPassthrough {
		t.Fatalf("capabilities = %+v, want cwd-gate + scrubbed + env passthrough", caps)
	}
}

// TestIsSensitiveEnvName 验证敏感名判定覆盖常见凭据模式。
func TestIsSensitiveEnvName(t *testing.T) {
	for _, name := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "api_key", "GITHUB_TOKEN", "AWS_SECRET_ACCESS_KEY", "DB_PASSWORD"} {
		if !isSensitiveEnvName(name) {
			t.Errorf("%q must be sensitive", name)
		}
	}
	for _, name := range []string{"PATH", "SystemRoot", "HOME", "LANG", "SEELEX_CONFIG"} {
		if isSensitiveEnvName(name) {
			t.Errorf("%q must not be sensitive", name)
		}
	}
}
