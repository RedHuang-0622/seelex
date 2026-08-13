package docker

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

// ── docker 守护进程自动恢复（internal/docker，2026-08-07 根治）──────

func TestIsDaemonDown(t *testing.T) {
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
			if got := IsDaemonDown(tc.stdout, tc.stderr); got != tc.want {
				t.Fatalf("IsDaemonDown = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCLIPathFallbacks(t *testing.T) {
	originalLookup, originalFixed := CLILookup, fixedPaths
	defer func() { CLILookup, fixedPaths = originalLookup, originalFixed }()

	// LookPath 命中 → 返回 PATH 结果。
	CLILookup = func(string) (string, error) { return "C:/tools/docker.exe", nil }
	if got := CLIPath(); got != "C:/tools/docker.exe" {
		t.Fatalf("lookup hit = %q", got)
	}
	// LookPath 失败 + 固定路径存在 → 回退固定路径。
	CLILookup = func(string) (string, error) { return "", errors.New("not found") }
	fixed := filepath.Join(t.TempDir(), "docker.exe")
	if err := os.WriteFile(fixed, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	fixedPaths = []string{fixed}
	if got := CLIPath(); got != fixed {
		t.Fatalf("fixed-path fallback = %q", got)
	}
	// LookPath 失败 + 固定路径不存在 → ""（不触发自动恢复）。
	fixedPaths = nil
	if got := CLIPath(); got != "" {
		t.Fatalf("missing docker must yield empty path, got %q", got)
	}
}

func TestEnsureDaemonReadyImmediately(t *testing.T) {
	prober := Prober{
		Up:    func(context.Context) bool { return true },
		Start: func(context.Context) error { t.Fatal("must not start when ready"); return nil },
	}
	if err := EnsureDaemon(context.Background(), prober, 5*time.Second); err != nil {
		t.Fatalf("ready daemon must not error: %v", err)
	}
}

func TestEnsureDaemonStartsAndWaits(t *testing.T) {
	var mu sync.Mutex
	started := false
	prober := Prober{
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
	if err := EnsureDaemon(context.Background(), prober, 5*time.Second); err != nil {
		t.Fatalf("start-then-ready must succeed: %v", err)
	}
}

func TestEnsureDaemonStartFails(t *testing.T) {
	prober := Prober{
		Up:    func(context.Context) bool { return false },
		Start: func(context.Context) error { return errors.New("Docker Desktop not installed") },
	}
	err := EnsureDaemon(context.Background(), prober, 3*time.Second)
	if err == nil || !strings.Contains(err.Error(), "start Docker Desktop failed") {
		t.Fatalf("start failure must propagate, got %v", err)
	}
}

func TestEnsureDaemonTimeout(t *testing.T) {
	prober := Prober{
		Up:    func(context.Context) bool { return false },
		Start: func(context.Context) error { return nil },
	}
	start := time.Now()
	err := EnsureDaemon(context.Background(), prober, 3*time.Second)
	if err == nil || !strings.Contains(err.Error(), "did not become ready") {
		t.Fatalf("timeout must report, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("timeout path took too long: %v", elapsed)
	}
}

func TestHintMentionsRecovery(t *testing.T) {
	if !strings.Contains(Hint(nil), "docker 自动恢复") {
		t.Fatalf("hint must mention auto recovery: %s", Hint(nil))
	}
	if !strings.Contains(Hint(errors.New("boom")), "boom") {
		t.Fatalf("hint must carry start error: %s", Hint(errors.New("boom")))
	}
}
