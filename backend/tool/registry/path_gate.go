package registry

// path_gate.go —— 路径门**前移**（M2-C5 / P1）：在权限判定与审批之前先做
// ProjectScope 越界检查。
//
// 现场：以前顺序是 事件 → 权限判定（ask 就地弹审批）→ 诊断 → handler，而路径
// 越界只在 handler 里才被发现。后果是越界路径会先弹一次完全没用的审批，用户
// 批准之后仍然被拒。现在顺序是 事件 → 路径门 → 权限门 → 诊断 → handler：
// 越界直接拒，不弹窗。
//
// 判据不在这里复制：路径根解析仍只有 resolveScopedPath 一处（与 handler 同源），
// 本文件只负责"哪个工具的哪个入参是路径"这张表。解析失败（JSON 坏）不在此报错
// ——交给后面原有的参数校验，避免把同一类错误挪成两种文案。

import (
	"context"
	"encoding/json"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
)

// PathGate 返回路径检查中间件。scope 为 nil 时返回 nil（未装配 = 不启用）。
func PathGate(deps Deps) frameworktools.MetaMiddleware {
	if deps.ProjectScope == nil {
		return nil
	}
	return func(name string, _ frameworktools.ToolMeta, next frameworktools.ToolHandler) frameworktools.ToolHandler {
		return frameworktools.HandlerFunc(func(ctx context.Context, argsJSON string) (string, error) {
			// 远端面例外：显式指名 ssh 的调用，路径属**对端**命名空间，本地项目根的
			// 包含关系不成立（把远端 `/etc/nginx.conf` 判成"越界"会直接废掉远端面）。
			//
			// 只有**显式指名**才有这个例外：那是调用方自己说的"这次走远端"。
			// 剩余缺口（如实记录，未在本轮修）：选择器自动选中 ssh（项目配了连接且探针
			// 通、调用方未指名）时，本门仍在选择之前按本地根判——远端绝对路径会被误拒。
			// 根治要把"选面"挪到本门之前，或让本门按面取根；那是链序决策，见 README。
			if kind, explicit := explicitTransport(ctx, argsJSON); explicit && kind == TransportSSH {
				return next.Execute(ctx, argsJSON)
			}
			for _, target := range pathGateTargets(name, argsJSON) {
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
