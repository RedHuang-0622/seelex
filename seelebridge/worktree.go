package seelebridge

import "context"

// ─── Runtime worktree 委托入口（worktree 生命周期实现在 worktree_manager.go）───
// Step 1 拆分后，Runtime 只保留委托方法；字段访问全部收敛到 worktreeManager 组件。

// nodeWorktree 是单个节点的 worktree 现场（plan_run 生命周期内有效）。
type nodeWorktree struct {
	Path       string // worktree 工作目录（NodeScope.WorkspaceID 指向）
	Branch     string // seelex/<nodeID>
	BaseCommit string // 创建时 HEAD（合并提交判定基线）
	MainBranch string // 主工作区当前分支（rebase/merge 目标）
}

// nodeWorktreeFor 返回节点的 worktree（无 → nil）。
func (r *Runtime) nodeWorktreeFor(nodeID string) *nodeWorktree {
	if r == nil || r.worktreeMgr == nil {
		return nil
	}
	return r.worktreeMgr.worktreeFor(nodeID)
}

// beginNodeWorktree 为节点创建 worktree（降级返回 nil；语义见 worktreeManager.Begin）。
func (r *Runtime) beginNodeWorktree(scope NodeScope, nodeID string) *nodeWorktree {
	if r == nil || r.worktreeMgr == nil {
		return nil
	}
	return r.worktreeMgr.Begin(scope, nodeID)
}

// finishNodeWorktree 收尾：变基兜底 → 提交判定 → 合并审批 → merge → 清理。
func (r *Runtime) finishNodeWorktree(ctx context.Context, nodeID string, wt *nodeWorktree) error {
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
