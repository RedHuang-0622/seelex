// docker 守护进程自动恢复（2026-08-07 根治：真实环境有 docker CLI 但
// Docker Desktop 守护进程未运行是常见状态——bash 工具直接报原始 pipe 错误
// 对模型毫无帮助。现在：命令失败且错误匹配 daemon-down 模式 → 自动启动
// Docker Desktop → 轮询就绪 → 由调用方重跑一次命令）。
//
// 启动路径（Windows）：优先 `docker desktop start`（Docker ≥ 27 官方 CLI
// 启动命令，不开完整 GUI）；回退直接拉起 Docker Desktop.exe。非 Windows
// 平台不自动启动（显式报错，让模型知道环境受限）。
package seelebridge

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/seelebridge/security"
)

// dockerDesktopExe 是 Docker Desktop 的固定安装路径（回退启动路径）。
const dockerDesktopExe = `C:\Program Files\Docker\Docker\Docker Desktop.exe`

// dockerDaemonDownMarkers 是"守护进程未运行"的典型错误模式
// （Windows 命名管道不存在 / 连接失败），命中即触发自动恢复。
var dockerDaemonDownMarkers = []string{
	"dockerDesktopLinuxEngine", // Docker Desktop 引擎管道
	"docker_engine",            // 默认上下文管道
	"error during connect",
	"cannot find the file specified", // npipe 不存在
	"the system cannot find the file",
	"daemon is not running",
	"is the docker daemon running",
	"connection refused",
	"npipe", // 命名管道连接失败
}

// isDockerDaemonDown 判断 bash 命令的失败输出是否指向"docker 守护进程
// 未运行"（CLI 存在但 daemon 不在）。输出为空 → false（无法判定不触发）。
func isDockerDaemonDown(stdout, stderr string) bool {
	haystack := strings.ToLower(stdout + "\n" + stderr)
	for _, marker := range dockerDaemonDownMarkers {
		if strings.Contains(haystack, marker) {
			return true
		}
	}
	return false
}

// dockerCLILookup 是 docker CLI 的 PATH 探测（测试可覆盖；并行测试禁止）。
var dockerCLILookup = exec.LookPath

// dockerFixedPaths 是 Docker Desktop 的固定安装路径回退（测试可覆盖）。
var dockerFixedPaths = []string{
	`C:\Program Files\Docker\Docker\resources\bin\docker.exe`,
	`C:\Program Files\Docker\Docker\resources\bin\docker`,
}

// dockerCLIPath 定位 docker CLI：PATH 优先，回退 Docker Desktop 固定路径。
// 找不到 → ""（环境无 docker，不触发自动恢复）。
func dockerCLIPath() string {
	if path, err := dockerCLILookup("docker"); err == nil {
		return path
	}
	for _, candidate := range dockerFixedPaths {
		if security.FileExists(candidate) {
			return candidate
		}
	}
	return ""
}

// dockerProber 抽象守护进程探测/启动（测试注入点；生产用真实实现）。
type dockerProber struct {
	// Up 返回守护进程是否就绪（幂等，可反复调用）。
	Up func(ctx context.Context) bool
	// Start 启动守护进程（幂等；已启动/启动中返回 nil）。
	Start func(ctx context.Context) error
}

// realDockerProber 是生产探针：docker info 快速探测 + Windows 启动路径。
func realDockerProber() dockerProber {
	return dockerProber{
		Up: func(ctx context.Context) bool {
			if dockerCLIPath() == "" {
				return false
			}
			probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			cmd := exec.CommandContext(probeCtx, dockerCLIPath(), "info", "--format", "{{.ServerVersion}}")
			security.ConfigureHiddenCommand(cmd)
			return cmd.Run() == nil
		},
		Start: func(ctx context.Context) error {
			if runtime.GOOS != "windows" {
				return fmt.Errorf("docker daemon auto-start is not supported on %s", runtime.GOOS)
			}
			// 优先官方 CLI 启动命令（不开完整 GUI）；失败回退直接拉起
			// Docker Desktop（引擎启动入口，GUI 窗口可能短暂出现）。
			if cli := dockerCLIPath(); cli != "" {
				if cmd := exec.CommandContext(ctx, cli, "desktop", "start"); cmd.Run() == nil {
					return nil
				}
			}
			if !security.FileExists(dockerDesktopExe) {
				return fmt.Errorf("docker: Docker Desktop is not installed at %s", dockerDesktopExe)
			}
			cmd := exec.CommandContext(ctx, dockerDesktopExe)
			security.ConfigureHiddenCommand(cmd)
			return cmd.Start()
		},
	}
}

// ensureDockerDaemon 确保守护进程就绪：未就绪 → Start → 轮询 Up 直到
// waitTimeout。已就绪直接返回（不重复启动）。返回错误时附带启动输出说明。
func ensureDockerDaemon(ctx context.Context, prober dockerProber, waitTimeout time.Duration) error {
	if prober.Up == nil || prober.Start == nil {
		return fmt.Errorf("docker: prober is not configured")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if prober.Up(probeCtx) {
		return nil
	}
	startCtx, startCancel := context.WithTimeout(ctx, 15*time.Second)
	err := prober.Start(startCtx)
	startCancel()
	if err != nil {
		return fmt.Errorf("docker: start Docker Desktop failed: %w", err)
	}
	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return fmt.Errorf("docker: waiting for daemon canceled: %w", ctx.Err())
		default:
		}
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		ready := prober.Up(probeCtx)
		cancel()
		if ready {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("docker: daemon did not become ready within %v (Docker Desktop 启动慢或 WSL 后端异常)", waitTimeout)
}

// ensureDockerForRuntime 是 Runtime 的接线面：按 limits 配置执行自动恢复
// （disable_docker_auto_start 关闭时返回 nil 表示"不处理"）。
// dockerProbe 已注入（测试）时使用注入探针，否则用真实实现。
func (r *Runtime) ensureDockerForRuntime(ctx context.Context) error {
	if r == nil || r.limits.DisableDockerAutoStart {
		return nil
	}
	timeout := time.Duration(r.limits.DockerStartTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	prober := r.dockerProbe
	if prober.Up == nil || prober.Start == nil {
		prober = realDockerProber()
	}
	return ensureDockerDaemon(ctx, prober, timeout)
}

// dockerHint 生成模型可读的恢复说明（重跑仍失败时附加）。
func dockerHint(startErr error) string {
	var builder strings.Builder
	builder.WriteString("\n[docker 自动恢复] 检测到 Docker 守护进程未运行，已尝试自动启动 Docker Desktop")
	if startErr != nil {
		builder.WriteString("但失败: ")
		builder.WriteString(startErr.Error())
		builder.WriteString("。请手动启动 Docker Desktop 后重试，或检查 WSL 后端。")
	} else {
		builder.WriteString("；重跑仍失败，请检查 Docker Desktop 是否成功启动（启动可能需要 1-2 分钟）。")
	}
	return builder.String()
}
