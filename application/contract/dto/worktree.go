package dto

// NodeWorktreeInfo 是节点 worktree 现场的只读摘要（恢复数据面）：
// 节点失败/合并被拒时现场保留且注册表不释放——路径就是人工恢复入口。
//
// 四栏都是**收尾要用的事实**，一栏都不能缺：`MainBranch` 决定这次合回哪条分支（M2）、
// `BaseCommit` 是变基与"有没有提交过"的判据基线。缺栏的成因只有一处——**弱登记先到**
// （记录只写了 Path/Branch 的那一份先进了注册表，见 `worktree_weak_registration_merge_test.go`），
// 因此记录侧（`sessionstore.NodeWorktreeRecord`）必须把四栏写全，而不是在这里靠 git 现算。
type NodeWorktreeInfo struct {
	Path       string // worktree 工作目录（文件现场）
	Branch     string // seelex/<nodeID> 分支（改动提交后仍可 git merge 恢复）
	MainBranch string // 主工作区分支（merge 目标）
	BaseCommit string // 建现场时的 HEAD（变基与提交判定的基线）
}
