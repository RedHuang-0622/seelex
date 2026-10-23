package registry

// path_gate.go —— 路径门**前移**（M2-C5 / P1）：在权限判定与审批之前先做
// ProjectScope 越界检查。
//
// 现场：以前顺序是 事件 → 权限判定（ask 就地弹审批）→ 诊断 → handler，而路径
// 越界只在 handler 里才被发现。后果是越界路径会先弹一次完全没用的审批，用户
// 批准之后仍然被拒。现在顺序是 事件 → **执行面决策** → 路径门 → 权限门 → 建连 →
// 诊断 → handler：越界直接拒，不弹窗；而"按哪个根判"由决策段给的执行面决定
// （本地面 = 项目根；远端面不按本地根判，见 path_gate_face.go）。
//
// 判据不在这里复制：路径根解析仍只有 resolveScopedPath 一处（与 handler 同源），
// 本文件只负责"哪个工具的哪个入参是路径"这张表，以及按面分派。解析失败（JSON 坏）
// 不在此报错——交给后面原有的参数校验，避免把同一类错误挪成两种文案。

import (
	"context"
	"encoding/json"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
)

// PathGate 返回路径检查中间件。scope 为 nil 时返回 nil（未装配 = 不启用）。
//
// 判据按**已定执行面**取根（见 path_gate_face.go）：本地面仍按项目根判、越界直接拒；
// 远端面不做本地根判定（远端路径属对端命名空间，越界候选交给权限门的 ssh.userspace）。
func PathGate(deps Deps) frameworktools.MetaMiddleware {
	if deps.ProjectScope == nil {
		return nil
	}
	return func(name string, _ frameworktools.ToolMeta, next frameworktools.ToolHandler) frameworktools.ToolHandler {
		return frameworktools.HandlerFunc(func(ctx context.Context, argsJSON string) (string, error) {
			targets := pathGateTargets(name, argsJSON)
			if pathGateFace(ctx, argsJSON) == PathGateFaceRemote {
				// 远端面：**不做本地根判定**。远端根已知时按它筛，未知时按本地根当筛子。
				// 筛出来的越界候选要过 ssh.userspace（没有就拒，不静默放行），远端面本身
				// 还要过 ssh 通道位——两道门都在 requireRemoteFacePermission 里。
				remoteRoot := pathGateRemoteRoot(ctx)
				candidates := remoteEscapeCandidates(deps, ctx, remoteRoot, targets)
				if err := requireRemoteFacePermission(deps, ctx, candidates); err != nil {
					return "", err
				}
				if len(candidates) > 0 {
					ctx = WithRemotePathHandoff(ctx, RemotePathHandoff{
						Face: PathGateFaceRemote, RemoteRoot: remoteRoot, Candidates: candidates,
					})
				}
				return next.Execute(ctx, argsJSON)
			}
			for _, target := range targets {
				if _, err := resolveScopedPath(deps, ctx, target.path, target.write); err != nil {
					return "", err
				}
			}
			return next.Execute(ctx, argsJSON)
		})
	}
}

type pathTarget struct {
	path  string
	write bool
}

// pathGateTargets 是"工具名 → 路径入参"的唯一清单。没有路径语义的工具返回 nil，
// 中间件直接放行。bash 的 workdir 按读口径解析（与 ResolveWorkdir 同义：空值取
// 会话根）；命令正文里的路径不在这里猜——那属权限模式表的职责。
func pathGateTargets(toolName, argsJSON string) []pathTarget {
	switch toolName {
	case ReadFileToolName, GrepSearchToolName, GlobToolName:
		var args struct {
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(argsJSON), &args) != nil || args.Path == "" {
			return nil
		}
		return []pathTarget{{path: args.Path}}
	case WriteFileToolName, EditFileToolName:
		var args struct {
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(argsJSON), &args) != nil || args.Path == "" {
			return nil
		}
		return []pathTarget{{path: args.Path, write: true}}
	case BashToolName, "bash_bg":
		var args struct {
			Workdir string `json:"workdir"`
		}
		if json.Unmarshal([]byte(argsJSON), &args) != nil || args.Workdir == "" {
			return nil
		}
		return []pathTarget{{path: args.Workdir}}
	case "read_batch":
		var args struct {
			Paths []string `json:"paths"`
		}
		if json.Unmarshal([]byte(argsJSON), &args) != nil {
			return nil
		}
		targets := make([]pathTarget, 0, len(args.Paths))
		for _, path := range args.Paths {
			if path != "" {
				targets = append(targets, pathTarget{path: path})
			}
		}
		return targets
	}
	return nil
}
