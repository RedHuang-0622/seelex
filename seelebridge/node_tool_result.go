// 子代理工具结果读回桥（P1 修复，2026-08-07）：节点会话的 result_ref
// 此前经共享内存归档器落点不明、主会话读不到（"result_ref 不可读"）。
// 现在节点会话的工具结果落到节点专属归档器，ref 带 node:<nodeID>: 前缀，
// 运行中/结束后均可经 Runtime.NodeToolResult 读回；主代理的 read_tool_result
// 解析 node: 前缀转交本桥（application/core/reference_tools.go）。
//
// 生命周期语义：内存态，与子代理树/节点上下文快照同生命周期（进程存活
// 期间可读；不落盘，与"子代理仅内存态存储"约定一致）。
package seelebridge

import (
	"context"
	"strings"

	"github.com/RedHuang-0622/seelex/seelexctx"
)

// nodeResultRefPrefix 是节点工具结果引用的前缀（ref = node:<nodeID>:result:<callID>）。
const nodeResultRefPrefix = "node:"

// nodeToolResultArchiver 是节点会话的 ToolResultArchiver 适配：
// 从 ctx 的 NodeScope 解析节点 ID，工具结果落到节点专属内存归档器并带
// node: 前缀；非节点上下文回退共享归档（行为与旧实现一致）。
type nodeToolResultArchiver struct {
	runtime *Runtime
	shared  seelexctx.ToolResultArchiver
}

// Store 实现 seelexctx.ToolResultArchiver（幂等由底层归档器保证）。
func (a nodeToolResultArchiver) Store(ctx context.Context, callID, tool, raw string) (string, error) {
	if scope, ok := NodeScopeFromContext(ctx); ok && scope.NodeID != "" && scope.Role == RoleSubAgent {
		arch := a.runtime.nodeToolResultArchiverFor(scope.NodeID)
		ref, err := arch.Store(ctx, callID, tool, raw)
		if err != nil {
			return "", err
		}
		return nodeResultRefPrefix + scope.NodeID + ":" + ref, nil
	}
	return a.shared.Store(ctx, callID, tool, raw)
}

// nodeToolResultArchiverFor 返回节点专属归档器（惰性创建；同一节点跨
// plan_run 复用，直到被下一次 fork 覆盖——与 nodeContextSnapshots 同生命周期）。
func (r *Runtime) nodeToolResultArchiverFor(nodeID string) *seelexctx.InMemoryToolResultArchiver {
	r.nodeSessionsMu.Lock()
	defer r.nodeSessionsMu.Unlock()
	arch := r.nodeToolArchivers[nodeID]
	if arch == nil {
		arch = seelexctx.NewInMemoryToolResultArchiver()
		r.nodeToolArchivers[nodeID] = arch
	}
	return arch
}

// NodeToolResult 读回节点子代理的工具结果原始内容（ref 必须带
// node:<nodeID>: 前缀）。返回 (内容, 是否存在)。只读节点归档器，安全。
func (r *Runtime) NodeToolResult(nodeID, ref string) (string, bool) {
	if r == nil || nodeID == "" || ref == "" {
		return "", false
	}
	r.nodeSessionsMu.Lock()
	arch := r.nodeToolArchivers[nodeID]
	r.nodeSessionsMu.Unlock()
	if arch == nil {
		return "", false
	}
	return arch.Read(strings.TrimPrefix(ref, nodeResultRefPrefix+nodeID+":"))
}

// NodeWorktreeInfo 是节点 worktree 现场的只读摘要（恢复数据面）：
// 节点失败/合并被拒时现场保留且注册表不释放——路径就是人工恢复入口。
type NodeWorktreeInfo struct {
	Path       string // worktree 工作目录（文件现场）
	Branch     string // seelex/<nodeID> 分支（改动提交后仍可 git merge 恢复）
	MainBranch string // 主工作区分支（merge 目标）
}

// NodeWorktreeInfoFor 返回节点 worktree 现场信息（无现场 → false）。
// 运行中返回当前 worktree；结束后成功路径已清理（false）；失败/被拒路径
// 现场保留（true，Path 即恢复入口）。
func (r *Runtime) NodeWorktreeInfoFor(nodeID string) (NodeWorktreeInfo, bool) {
	if r == nil || nodeID == "" {
		return NodeWorktreeInfo{}, false
	}
	wt := r.nodeWorktreeFor(nodeID)
	if wt == nil {
		return NodeWorktreeInfo{}, false
	}
	return NodeWorktreeInfo{Path: wt.Path, Branch: wt.Branch, MainBranch: wt.MainBranch}, true
}
