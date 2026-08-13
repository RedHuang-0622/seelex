package seelebridge

import (
	"context"

	"github.com/RedHuang-0622/seelex/seelebridge/worktree"
)

// Runtime worktree 委托入口：worktree 生命周期实现已迁入 seelebridge/worktree 域，
// 根包只保留委托方法。NodeWorktree 是单个节点的 worktree 现场（plan_run 生命周期内有效）。
type NodeWorktree = worktree.NodeWorktree

// beginNodeWorktree 为节点创建 worktree（降级返回 nil；语义见 worktree.WorktreeManager.Begin）。
func (r *Runtime) beginNodeWorktree(scope NodeScope, nodeID string) *worktree.NodeWorktree {
	if r == nil || r.worktreeMgr == nil {
		return nil
	}
	return r.worktreeMgr.Begin(scope, nodeID)
}

// finishNodeWorktree 收尾：变基仓库 → 提交判定 → 合并审批 → merge → 清理。
func (r *Runtime) finishNodeWorktree(ctx context.Context, nodeID string, wt *worktree.NodeWorktree) error {
	if r == nil || r.worktreeMgr == nil {
		return nil
	}
	return r.worktreeMgr.Finish(ctx, nodeID, wt)
}

// releaseNodeWorktree 在节点结束时从注册表移除（成功路径已清理；失败路径保留现场）。
func (r *Runtime) releaseNodeWorktree(nodeID string) {
	if r == nil || r.worktreeMgr == nil {
		return
	}
	r.worktreeMgr.Release(nodeID)
}
