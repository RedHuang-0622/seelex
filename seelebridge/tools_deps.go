package seelebridge

import "github.com/RedHuang-0622/seelex/seelebridge/tools"

// scopedToolsDeps 把 Runtime 能力面注入 tools 域（Deps 全部为闭包，域内不依赖根包）。
func (r *Runtime) scopedToolsDeps() tools.Deps {
	return tools.Deps{
		RegisterTool:           r.RegisterTool,
		ProjectScope:           r.projectScope,
		FileSystem:             r.filesystem,
		GrepMaxResults:         r.limits.GrepMaxResults,
		WalkTimeoutSec:         r.limits.WalkTimeoutSec,
		ToolCallTimeout:        r.toolCallTimeout,
		ToolCallTimeoutSec:     r.limits.ToolCallTimeoutSec,
		DisableDockerAutoStart: r.limits.DisableDockerAutoStart,
		ObserveBash:            r.observeBash,
		EnsureDocker:           r.ensureDockerForRuntime,
		DockerDaemonDown:       isDockerDaemonDown,
		DockerCLIPath:          dockerCLIPath,
		DockerHint:             dockerHint,
	}
}
