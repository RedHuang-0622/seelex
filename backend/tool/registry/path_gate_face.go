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
//	                              路径属对端命名空间，越界与否由 `ssh.userspace` 判。
//	                              本门是**那道闸门的落点**（2026-10-2x）：越界候选不再
//	                              只是"交接事实"，而是"没有 ssh.userspace 就拒"，并
//	                              带可结构化区分的错误码；远端面本身另要 `ssh` 通道位。
//	                              （RemotePathHandoff 仍然写进 ctx，供诊断与用例读。）
//
// 相对路径：本地面照旧按项目根解析（`../..` 这类越界必须在这里就拒，不能等到 handler
// 才拒——那会先弹一次审批）。**远端面上相对路径不按本地根判**：它是远端相对路径，
// 与本地根没有关系（远端相对路径的越界问题归对端 shell 的 cwd，不在本门猜）。
//
// 没有决策时（未装配决策段 / 直接用 PathGate 的调用面）退回旧口径：显式指名 ssh 视为
// 远端面，其余本地面——行为与旧实现逐字一致。

import (
	"context"
	"errors"
	"path"
	"path/filepath"
	"strings"

	"github.com/RedHuang-0622/seelex/backend/limit/gate"
	"github.com/RedHuang-0622/seelex/backend/limit/policy"
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
// 它的角色在 2026-10-2x 变了：以前是**唯一的**结论（"交给权限门，本门不下判断"），
// 现在它只是**留痕**——判据已经在路径门里落地（requireRemoteFacePermission），越界
// 而无 ssh.userspace 的调用根本走不到这里。保留这一格是为了诊断与用例能读出"这次
// 为什么用了 userspace 权限"（哪几条路径在外面）。
type RemotePathHandoff struct {
	Face PathGateFace
	// RemoteRoot 是本次用的远端项目根（空 = 未知，此时按本地根当筛子得出的候选）。
	RemoteRoot string
	// Candidates 是已按 ssh.userspace 过问过的路径（原样，未改写）。
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

// remoteEscapeCandidates 筛出"远端根/本地根都不包含"的绝对路径（纯字符串判定）。
//
// 三条口径：
//   - 相对路径**不筛**：远端面上它是远端相对路径，与本地根无关；
//   - 远端根已知时按**远端语义（POSIX）**比对：远端根 `/srv/app` 与目标 `/srv/app/x`
//     是根内；`/etc/nginx.conf` 是越界。这一条必须用 POSIX 判，不能用宿主语义
//     （filepath）——Windows 宿主上 `filepath.IsAbs("/etc/x")` 是 false，会把远端
//     绝对路径当成相对路径，闸门于是静默失效；
//   - 远端根未知时按本地根当筛子（本地根都容不下的绝对路径，才是需要 userspace
//     过问的那一类）。宿主语义在这里是对的：拿本地根比本地形态的路径。
func remoteEscapeCandidates(deps Deps, ctx context.Context, remoteRoot string, targets []pathTarget) []string {
	root := remoteRoot
	if root == "" {
		root = localPathRoot(deps, ctx)
	}
	candidates := make([]string, 0, len(targets))
	for _, target := range targets {
		if target.path == "" || !remotePathIsAbs(target.path) {
			continue
		}
		if root == "" {
			// 没有可用的根：无权判定，整条交出去（宁可不判，也不误拒）。
			candidates = append(candidates, target.path)
			continue
		}
		if remotePathInside(root, target.path) {
			continue
		}
		candidates = append(candidates, target.path)
	}
	return candidates
}

// remotePathIsAbs 报告路径是不是"远端面上看得懂的绝对路径"。
//
// 两个来源缺一不可：POSIX 形态（`/etc/x`，远端真正在用的那一种）与宿主形态
// （`C:\x`，只在用例/Windows 远端里出现）。只判宿主形态会让真实远端路径在 Windows
// 上被判成相对路径（闸门失效）；只判 POSIX 形态会让既有用例与本机形态的远端根失去
// 判据。
func remotePathIsAbs(candidate string) bool {
	if strings.HasPrefix(candidate, "/") {
		return true
	}
	return filepath.IsAbs(candidate)
}

// remotePathInside 判"candidate 是否落在 root 内"（远端路径用远端语义）。
func remotePathInside(root, candidate string) bool {
	if strings.HasPrefix(root, "/") {
		cleanRoot := path.Clean(root)
		cleanCandidate := path.Clean(candidate)
		if cleanCandidate == cleanRoot {
			return true
		}
		return strings.HasPrefix(cleanCandidate, strings.TrimSuffix(cleanRoot, "/")+"/")
	}
	// 宿主形态的根（用例 / Windows 远端）：仍走那一处判据，不另写一份。
	if _, err := security.ResolveInside(root, candidate); err != nil {
		return false
	}
	return true
}

// ── 两道按面判定的闸门（本工作项的核心）────────────────────────────────

// 按面判定的错误码。它们**不是** TransportError 的码：那五个码说的是"连不上/认证
// 失败"，而这里说的是"权限不足"。两面混用会让"被拒"与"连不上"在同一处被判成同一件
// 事（transport.go 的注释把这条边界写成了结构性约束）。
const (
	// CodePermSSHRequired 表示这次调用要打远端，但主体没有 ssh 通道位。
	CodePermSSHRequired = "PERM_SSH_REQUIRED"
	// CodePermSSHUserspaceRequired 表示这次调用要走远端项目根之外，但主体没有
	// ssh.userspace 位。
	CodePermSSHUserspaceRequired = "PERM_SSH_USERSPACE_REQUIRED"
)

// FacePermissionError 是"按面判定的权限不足"（结构化：带组名与固定码）。
//
// 结构化而不是一句文案：界面/用例/工具层要能分开回答"缺的是通道位还是越界位"，
// 并把这个结论原样报给宿主人类（缺哪一格就补哪一格）。
type FacePermissionError struct {
	// Code 是固定码（CodePermSSHRequired / CodePermSSHUserspaceRequired）。
	Code string
	// Group 是缺位的路由组名（policy.GroupSSH / policy.GroupSSHUserspace）。
	Group string
	// Paths 是触发这次拒绝的远端路径（通道位缺失时为空）。
	Paths []string
	// Msg 是给人读的下一步。
	Msg string
}

func (e *FacePermissionError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Msg
}

// FacePermissionErrorCode 返回按面判定的错误码（不是这类错误 → 空串）。
func FacePermissionErrorCode(err error) string {
	var face *FacePermissionError
	if errors.As(err, &face) && face != nil {
		return face.Code
	}
	return ""
}

// IsFacePermissionError 报告 err 是否是按面判定的拒绝（可传组名做进一步区分）。
func IsFacePermissionError(err error, groups ...string) bool {
	var face *FacePermissionError
	if !errors.As(err, &face) || face == nil {
		return false
	}
	if len(groups) == 0 {
		return true
	}
	for _, group := range groups {
		if face.Group == group {
			return true
		}
	}
	return false
}

// requireRemoteFacePermission 是远端面上的两道门（顺序即优先级）。
//
//	① 通道位：远端面本身要 `ssh`（x）。没有就拒——这是"这次调用打到对端"这件事的
//	   授权，与它做什么无关。
//	② 越界位：路径走出远端根时再要 `ssh.userspace`（r|w|x）。没有就拒，**绝不静默
//	   放行到对端**（静默放行等于把"远端项目根"这条边界当成装饰）。
//
// 判据来自 Deps.FacePermission（装配层接的是权限门本体，与 Enforce 同一份向量），
// 因此这里不会长出第二套"谁能干什么"。未装配（nil）或判据不接管（decided=false）时
// 退回旧行为：只留痕、不拒绝——与"没装这道门"逐字一致。
func requireRemoteFacePermission(deps Deps, ctx context.Context, candidates []string) error {
	if deps.FacePermission == nil {
		return nil
	}
	if granted, decided := deps.FacePermission(ctx, policy.GroupSSH); decided && !granted {
		return &FacePermissionError{
			Code: CodePermSSHRequired, Group: policy.GroupSSH,
			Msg: "这次调用要打到远端执行面，但当前主体没有 ssh 通道权限（位 x）；" +
				"在权限装配里授予 ssh，或显式指名 transport=local 在本机执行",
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	if granted, decided := deps.FacePermission(ctx, policy.GroupSSHUserspace); decided && !granted {
		return &FacePermissionError{
			Code: CodePermSSHUserspaceRequired, Group: policy.GroupSSHUserspace,
			Paths: append([]string(nil), candidates...),
			Msg: "这些路径在远端项目根之外（远端用户空间），当前主体没有 ssh.userspace 权限；" +
				"改用远端项目根内的路径（或在项目连接配置里填对远端工作目录），或授予 ssh.userspace",
		}
	}
	return nil
}
