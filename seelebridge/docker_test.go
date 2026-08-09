package seelebridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── docker 守护进程自动恢复（docker.go，2026-08-07 根治）─────────────

func TestIsDockerDaemonDown(t *testing.T) {
	cases := []struct {
		name   string
		stdout string
		stderr string
		want   bool
	}{
		{"windows pipe not found", "", "failed to connect to the docker API at npipe:////./pipe/dockerDesktopLinuxEngine: The system cannot find the file specified.", true},
		{"default context pipe", "", "error during connect: Get \"http://%2F%2F.%2Fpipe%2Fdocker_engine\": open //./pipe/docker_engine: The system cannot find the file", true},
		{"daemon not running", "", "Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?", true},
		{"connection refused", "dial tcp 127.0.0.1:2375: connect: connection refused", "", true},
		{"normal docker error", "Error response from daemon: No such container: abc", "", false},
		{"empty output", "", "", false},
		{"non-docker error", "command not found: docker", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDockerDaemonDown(tc.stdout, tc.stderr); got != tc.want {
				t.Fatalf("isDockerDaemonDown = %v, want %v", got, tc.want)
			}
		})
	}
}

// requireDockerRecoveryEnv 跳过依赖 daemon 状态的恢复测试：测试前提是
// “daemon 未启动”，而真实环境 Docker 可能在运行（前提不成立会误报）；
// 显式设 SEELEX_DOCKER_RECOVERY_TESTS=1 才运行。
func requireDockerRecoveryEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("SEELEX_DOCKER_RECOVERY_TESTS") == "" {
		t.Skip("set SEELEX_DOCKER_RECOVERY_TESTS=1 to run docker-recovery tests (daemon-state dependent)")
	}
}

func TestDockerCLIPathFallbacks(t *testing.T) {
	originalLookup, originalFixed := dockerCLILookup, dockerFixedPaths
	defer func() { dockerCLILookup, dockerFixedPaths = originalLookup, originalFixed }()

	// LookPath 命中 → 返回 PATH 结果。
	dockerCLILookup = func(string) (string, error) { return "C:/tools/docker.exe", nil }
	if got := dockerCLIPath(); got != "C:/tools/docker.exe" {
		t.Fatalf("lookup hit = %q", got)
	}
	// LookPath 失败 + 固定路径存在 → 回退固定路径。
	dockerCLILookup = func(string) (string, error) { return "", errors.New("not found") }
	fixed := filepath.Join(t.TempDir(), "docker.exe")
	if err := os.WriteFile(fixed, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	dockerFixedPaths = []string{fixed}
	if got := dockerCLIPath(); got != fixed {
		t.Fatalf("fixed-path fallback = %q", got)
	}
	// LookPath 失败 + 固定路径不存在 → ""（不触发自动恢复）。
	dockerFixedPaths = nil
	if got := dockerCLIPath(); got != "" {
		t.Fatalf("missing docker must yield empty path, got %q", got)
	}
}

func TestEnsureDockerDaemonReadyImmediately(t *testing.T) {
	prober := dockerProber{
		Up:    func(context.Context) bool { return true },
		Start: func(context.Context) error { t.Fatal("must not start when ready"); return nil },
	}
	if err := ensureDockerDaemon(context.Background(), prober, 5*time.Second); err != nil {
		t.Fatalf("ready daemon must not error: %v", err)
	}
}

func TestEnsureDockerDaemonStartsAndWaits(t *testing.T) {
	var mu sync.Mutex
	started := false
	prober := dockerProber{
		Up: func(context.Context) bool {
			mu.Lock()
			defer mu.Unlock()
			return started
		},
		Start: func(context.Context) error {
			mu.Lock()
			started = true
			mu.Unlock()
			return nil
		},
	}
	if err := ensureDockerDaemon(context.Background(), prober, 5*time.Second); err != nil {
		t.Fatalf("start-then-ready must succeed: %v", err)
	}
}

func TestEnsureDockerDaemonStartFails(t *testing.T) {
	prober := dockerProber{
		Up:    func(context.Context) bool { return false },
		Start: func(context.Context) error { return errors.New("Docker Desktop not installed") },
	}
	err := ensureDockerDaemon(context.Background(), prober, 3*time.Second)
	if err == nil || !strings.Contains(err.Error(), "start Docker Desktop failed") {
		t.Fatalf("start failure must propagate, got %v", err)
	}
}

func TestEnsureDockerDaemonTimeout(t *testing.T) {
	prober := dockerProber{
		Up:    func(context.Context) bool { return false },
		Start: func(context.Context) error { return nil },
	}
	start := time.Now()
	err := ensureDockerDaemon(context.Background(), prober, 3*time.Second)
	if err == nil || !strings.Contains(err.Error(), "did not become ready") {
		t.Fatalf("timeout must report, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("timeout path took too long: %v", elapsed)
	}
}

func TestDockerHintMentionsRecovery(t *testing.T) {
	if !strings.Contains(dockerHint(nil), "docker 自动恢复") {
		t.Fatalf("hint must mention auto recovery: %s", dockerHint(nil))
	}
	if !strings.Contains(dockerHint(errors.New("boom")), "boom") {
		t.Fatalf("hint must carry start error: %s", dockerHint(errors.New("boom")))
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
	original := dockerCLILookup
	defer func() { dockerCLILookup = original }()
	bin := t.TempDir()
	fake := fakeDockerCLI(t, bin)
	dockerCLILookup = func(string) (string, error) { return fake, nil }

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
	runtime.dockerProbe = dockerProber{
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
	original := dockerCLILookup
	defer func() { dockerCLILookup = original }()
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
	dockerCLILookup = func(string) (string, error) { return fake, nil }

	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	if err := runtime.BindProjectRoot(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	prober := dockerProber{
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
	original := dockerCLILookup
	defer func() { dockerCLILookup = original }()
	bin := t.TempDir()
	fake := fakeDockerCLI(t, bin)
	dockerCLILookup = func(string) (string, error) { return fake, nil }

	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	if err := runtime.BindProjectRoot(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	runtime.limits.DisableDockerAutoStart = true
	startCalled := false
	runtime.dockerProbe = dockerProber{
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
