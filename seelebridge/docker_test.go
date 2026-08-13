package seelebridge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/docker"
)

// ── docker 守护进程自动恢复（Runtime 接线，2026-08-07 根治）──────────
// 纯函数测试（daemon-down 判定/CLI 路径/探针/提示）已随域迁入
// internal/docker；本文件只保留 Runtime 端到端恢复测试。

// requireDockerRecoveryEnv 跳过依赖 daemon 状态的恢复测试：测试前提是
// "daemon 未启动"，而真实环境 Docker 可能在运行（前提不成立会误报）；
// 显式设 SEELEX_DOCKER_RECOVERY_TESTS=1 才运行。
func requireDockerRecoveryEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("SEELEX_DOCKER_RECOVERY_TESTS") == "" {
		t.Skip("set SEELEX_DOCKER_RECOVERY_TESTS=1 to run docker-recovery tests (daemon-state dependent)")
	}
}

// fakeDockerCLI 写一个假 docker 脚本：首次 info 报 daemon-down（匹配恢复
// 模式），touch started 文件后成功。供 scopedBash 恢复重试端到端测试。
func fakeDockerCLI(t *testing.T, dir string) string {
	t.Helper()
	script := `#!/usr/bin/env bash
if [ "$1" = "info" ]; then
  if [ -f "$(dirname "$0")/started" ]; then
    echo "Server: 29.1.2-fake"
    exit 0
  fi
  echo "failed to connect to the docker API at npipe:////./pipe/dockerDesktopLinuxEngine: The system cannot find the file specified." >&2
  exit 1
fi
echo "fake-docker"
exit 0
`
	path := filepath.Join(dir, "docker")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestScopedBashDockerRecoveryRetries 端到端：bash 命令失败且匹配
// daemon-down → 自动启动（注入探针）→ 重跑成功返回真实结果。
func TestScopedBashDockerRecoveryRetries(t *testing.T) {
	requireDockerRecoveryEnv(t)
	original := docker.CLILookup
	defer func() { docker.CLILookup = original }()
	bin := t.TempDir()
	fake := fakeDockerCLI(t, bin)
	docker.CLILookup = func(string) (string, error) { return fake, nil }

	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	if err := runtime.BindProjectRoot(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	// 注入探针：启动后 daemon 就绪（假 CLI 以 started 文件区分首跑/重跑；
	// Start 同时创建该文件，重跑才能成功）。
	var mu sync.Mutex
	started := false
	startedFile := filepath.Join(bin, "started")
	runtime.dockerProbe = docker.Prober{
		Up: func(context.Context) bool {
			mu.Lock()
			defer mu.Unlock()
			return started
		},
		Start: func(context.Context) error {
			mu.Lock()
			started = true
			mu.Unlock()
			return os.WriteFile(startedFile, []byte("1"), 0o644)
		},
	}

	// 命令里的 docker 由 bash 的 PATH 解析 → 把假 CLI 目录前置到 PATH。
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	result, err := runtime.Agent().DirectDispatch(context.Background(), "bash",
		`{"command":"docker info --format {{.ServerVersion}}","timeout":30}`)
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if !strings.Contains(result, "29.1.2-fake") || strings.Contains(result, "docker 自动恢复") {
		t.Fatalf("retry must return real docker output, got: %s", result)
	}
	mu.Lock()
	wasStarted := started
	mu.Unlock()
	if !wasStarted {
		t.Fatal("docker auto-start must have been invoked")
	}
}

// TestScopedBashDockerRecoveryFailsWithHint 恢复后重跑仍失败 → 附带恢复
// 提示（模型可据此行动），不静默吞错。
func TestScopedBashDockerRecoveryFailsWithHint(t *testing.T) {
	requireDockerRecoveryEnv(t)
	original := docker.CLILookup
	defer func() { docker.CLILookup = original }()
	bin := t.TempDir()
	// 假 CLI：永远报 daemon-down（started 文件也不救）。
	script := `#!/usr/bin/env bash
echo "failed to connect to the docker API at npipe:////./pipe/dockerDesktopLinuxEngine: The system cannot find the file specified." >&2
exit 1
`
	fake := filepath.Join(bin, "docker")
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	docker.CLILookup = func(string) (string, error) { return fake, nil }

	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	if err := runtime.BindProjectRoot(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	prober := docker.Prober{
		Up:    func(context.Context) bool { return true }, // 启动后立即"就绪"，重跑仍失败
		Start: func(context.Context) error { return nil },
	}
	runtime.dockerProbe = prober
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	result, err := runtime.Agent().DirectDispatch(context.Background(), "bash",
		`{"command":"docker info","timeout":30}`)
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if !strings.Contains(result, "[docker 自动恢复]") {
		t.Fatalf("retry failure must carry recovery hint, got: %s", result)
	}
}

// TestScopedBashDockerRecoveryDisabled 配置关闭自动恢复 → 原样返回失败。
func TestScopedBashDockerRecoveryDisabled(t *testing.T) {
	requireDockerRecoveryEnv(t)
	original := docker.CLILookup
	defer func() { docker.CLILookup = original }()
	bin := t.TempDir()
	fake := fakeDockerCLI(t, bin)
	docker.CLILookup = func(string) (string, error) { return fake, nil }

	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	if err := runtime.BindProjectRoot(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	runtime.limits.DisableDockerAutoStart = true
	startCalled := false
	runtime.dockerProbe = docker.Prober{
		Up:    func(context.Context) bool { return false },
		Start: func(context.Context) error { startCalled = true; return nil },
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	result, err := runtime.Agent().DirectDispatch(context.Background(), "bash",
		`{"command":"docker info","timeout":30}`)
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if startCalled {
		t.Fatal("auto-start must be skipped when disabled")
	}
	if strings.Contains(result, "[docker 自动恢复]") {
		t.Fatalf("disabled recovery must not add hint, got: %s", result)
	}
	if !strings.Contains(result, "dockerDesktopLinuxEngine") {
		t.Fatalf("raw failure must be preserved, got: %s", result)
	}
}
