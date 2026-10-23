package registry

// path_gate_face.go —— 路径门**按已定执行面取根**（本工作项的第二个落点）。
//
// 之前的口径：路径门一律按**本地**项目根判包含关系。问题是"这次走不走远端"要等
// 权限门之后的选择段才知道，于是**自动选中 ssh** 的调用（项目配了连接 + 探针通 +
// 调用方未指名）会被本地根判成越界直接拒——远端绝对路径（/etc/nginx/nginx.conf）
// 根本不是本地路径。旧口径只给**显式指名 ssh** 开了豁免，自动选中那一格是误拒。
//
// 现在：决策段已经在路径门之前把"这次走哪个面"算成 ctx 上的事实
// （transport_decision.go），路径门按面取根：
//
//	本地面（local，含所有降级）→ **项目根**（resolveScopedPath，判据仍是那一处），
//	                              越界直接拒（P1：不先弹一次没用的审批）。
//	远端面（ssh）             → **远端项目根**；远端根未知时**不做本地根判定**。远端
//	                              路径属对端命名空间，越界与否由权限门的 ssh.userspace
//	                              组判——本门只把"本地根会判成越界"的候选记成 ctx 事实
//	                              （RemotePathHandoff），不自己下结论。
//
// 相对路径：本地面照旧按项目根解析（`../..` 这类越界必须在这里就拒，不能等到 handler
// 才拒——那会先弹一次审批）。**远端面上相对路径不按本地根判**：它是远端相对路径，
// 与本地根没有关系。
//
// 没有决策时（未装配决策段 / 直接用 PathGate 的调用面）退回旧口径：显式指名 ssh 视为
// 远端面，其余本地面——行为与旧实现逐字一致。

import (
	"context"
	"path/filepath"

	"github.com/RedHuang-0622/seelex/backend/limit/gate"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// PathGateFace 是一次路径门判定使用的执行面。
type PathGateFace string

const (
	// PathGateFaceLocal 表示本地面：按项目根判包含关系（越界直接拒）。
	PathGateFaceLocal PathGateFace = "local"
	// PathGateFaceRemote 表示远端面：路径属对端命名空间，不做本地根判定。
	PathGateFaceRemote PathGateFace = "remote"
)

// pathGateFace 取本次判定的执行面（顺序即优先级）。
//
//  1. ctx 上有决策（决策段跑过）→ 按决策的面；
//  2. 没有决策但调用方显式指名了 ssh → 远端面（旧口径的豁免，保留）；
//  3. 其余 → 本地面。
func pathGateFace(ctx context.Context, argsJSON string) PathGateFace {
	if decision, ok := TransportDecisionFromContext(ctx); ok {
		if decision.Remote() {
			return PathGateFaceRemote
		}
		return PathGateFaceLocal
	}
	if kind, explicit := explicitTransport(ctx, argsJSON); explicit && kind == TransportSSH {
		return PathGateFaceRemote
	}
	return PathGateFaceLocal
}

// pathGateRemoteRoot 返回本次判定要用的远端项目根（空 = 未知）。
func pathGateRemoteRoot(ctx context.Context) string {
	decision, ok := TransportDecisionFromContext(ctx)
	if !ok {
		return ""
	}
	return decision.Endpoint.RemoteRoot
}

// localPathRoot 返回本地面判定的项目根（纯读，不做任何文件系统访问）。
//
// 判据与 resolveScopedPath 同源：worktree 节点（SceneScope.WorkspaceID）优先，否则
// 按执行 ctx 的会话键取项目根。这里只用来**筛**远端面上的越界候选，不参与放行/拒绝
// ——放行/拒绝仍只有 resolveScopedPath 一处。
func localPathRoot(deps Deps, ctx context.Context) string {
	if scope, ok := dto.SceneScopeFromContext(ctx); ok && scope.SceneID != "" && scope.WorkspaceID != "" {
		return scope.WorkspaceID
	}
	if deps.ProjectScope == nil {
		return ""
	}
	key := security.DefaultScopeKey
	if deps.SessionKey != nil {
		key = deps.SessionKey(ctx)
	}
	return deps.ProjectScope.RootFor(key)
}

// RemotePathHandoff 是远端面上"本地根会判成越界"的路径清单。
//
// 它不是拒绝理由（远端路径本来就可能在本地根之外），而是**交给权限门**的候选：
// 权限门按 `ssh.userspace` 组判"允许不允许在远端项目之外活动"。本轮只交付这份交接
// 事实与判定顺序；`ssh.userspace` 的规则行（组表条目）尚未接入，因此这里不假装
// 已经判过（见 README 的已知缺口）。
type RemotePathHandoff struct {
	Face PathGateFace
	// RemoteRoot 是本次用的远端项目根（空 = 未知，此时按本地根当筛子得出的候选）。
	RemoteRoot string
	// Candidates 是需要在远端侧按 ssh.userspace 过问的路径（原样，未改写）。
	Candidates []string
}

type remotePathHandoffKey struct{}

// WithRemotePathHandoff 把交接事务写进 ctx（权限门/诊断可读）。
func WithRemotePathHandoff(ctx context.Context, handoff RemotePathHandoff) context.Context {
	return context.WithValue(ctx, remotePathHandoffKey{}, handoff)
}

// RemotePathHandoffFromContext 读回这次调用的远端路径交接（没有则 ok=false）。
func RemotePathHandoffFromContext(ctx context.Context) (RemotePathHandoff, bool) {
	if ctx == nil {
		return RemotePathHandoff{}, false
	}
	handoff, ok := ctx.Value(remotePathHandoffKey{}).(RemotePathHandoff)
	return handoff, ok
}

// remoteEscapeCandidates 筛出"本地根/远端根都不包含"的绝对路径（纯字符串判定）。
//
// 两条口径：
//   - 相对路径**不筛**：远端面上它是远端相对路径，与本地根无关；
//   - 远端根已知时按远端根比对（根内 = 不需要过问），未知时按本地根当筛子——本地根
//     都容不下的绝对路径，才是需要 ssh.userspace 过问的那一类。
func remoteEscapeCandidates(deps Deps, ctx context.Context, remoteRoot string, targets []pathTarget) []string {
	root := remoteRoot
	if root == "" {
		root = localPathRoot(deps, ctx)
	}
	candidates := make([]string, 0, len(targets))
	for _, target := range targets {
		if target.path == "" || !filepath.IsAbs(target.path) {
			continue
		}
		if root == "" {
			// 没有可用的根：无权判定，整条交给权限门（宁可不判，也不误拒）。
			candidates = append(candidates, target.path)
			continue
		}
		if _, err := security.ResolveInside(root, target.path); err != nil {
			candidates = append(candidates, target.path)
		}
	}
	return candidates
}
