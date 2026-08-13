package seelebridge

import (
	"context"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/docker"
)

// Docker 守护进程自动恢复域已迁入 seelebridge/internal/docker（探针、
// daemon-down 判定、启动路径、恢复提示）。本文件只保留 Runtime 接线委托。

// ensureDockerForRuntime 是 tools 域的接线面：按 limits 配置执行自动恢复
// （disable_docker_auto_start 关闭时返回 nil 表示"不处理"）。
// dockerProbe 已注入（测试）时使用注入探针，否则用真实实现。
func (r *Runtime) ensureDockerForRuntime(ctx context.Context) error {
	if r == nil {
		return nil
	}
	return docker.EnsureForRuntime(ctx, r.limits.DisableDockerAutoStart, r.limits.DockerStartTimeoutSec, r.dockerProbe)
}

// dockerProbe 保留兼容名（测试注入点；内部使用 docker.Prober）。
type dockerProbe = docker.Prober
