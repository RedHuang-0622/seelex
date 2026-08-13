package dto

// NodeWorktreeInfo 是节点 worktree 现场的只读摘要（恢复数据面）：
// 节点失败/合并被拒时现场保留且注册表不释放——路径就是人工恢复入口。
type NodeWorktreeInfo struct {
	Path       string // worktree 工作目录（文件现场）
	Branch     string // seelex/<nodeID> 分支（改动提交后仍可 git merge 恢复）
	MainBranch string // 主工作区分支（merge 目标）
}
