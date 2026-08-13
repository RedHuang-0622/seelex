package seelebridge

import (
	"github.com/RedHuang-0622/seelex/seelebridge/worktree"
)

// 节点工具结果归档与 worktree 现场域已迁入 seelebridge/node（ToolResultArchiver）
// 与 seelebridge/worktree（NodeWorktreeInfo）。本文件只保留 Runtime 公开端口
// 委托与兼容别名。

// NodeWorktreeInfo 是节点 worktree 现场的只读摘要（恢复数据面）；
// 类型本体在 seelebridge/worktree 域。
type NodeWorktreeInfo = worktree.NodeWorktreeInfo

// NodeToolResult 读回节点子代理的工具结果原始内容（ref 必须带
// node:<nodeID>: 前缀）。只读节点归档器，安全。返回 (内容, 是否存在)。
func (r *Runtime) NodeToolResult(nodeID, ref string) (string, bool) {
	if r == nil || r.node == nil {
		return "", false
	}
	return r.node.ToolResult(nodeID, ref)
}

// NodeWorktreeInfoFor 返回节点 worktree 现场信息（无现场 → false）。
// 运行中返回当前 worktree；结束后成功路径已清理（false）；失败/被拒路径
// 现场保留（true，Path 即恢复入口）。
func (r *Runtime) NodeWorktreeInfoFor(nodeID string) (NodeWorktreeInfo, bool) {
	if r == nil || r.worktreeMgr == nil || nodeID == "" {
		return NodeWorktreeInfo{}, false
	}
	return r.worktreeMgr.Info(nodeID)
}
