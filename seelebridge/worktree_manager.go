package seelebridge

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/Seele/workplan/sugar/approve"
	"github.com/RedHuang-0622/seelex/seelebridge/security"
)

// ─── 子代理 worktree 生命周期管理器（Runtime 装配件拆分 Step 1）───
//
// 原 Runtime 直接持有 wt *worktreeState（worktree.go）。本组件把 worktree 注册表
// 与生命周期（begin → finish → release）收进独立组件：git 子进程天然串行，组件内
// 单锁即可；git 执行经可注入的 git 字段（默认 gitRunner，测试可替换为 fake），
// 不依赖 Runtime / ProjectScope / PlanEventSink——项目根、阶段事件与审批门经
// worktreeManagerDeps 注入，保持单向依赖。
//
// 失败现场语义保留：Release 仅在成功路径由调用方触发；任何 Finish 错误返回后
// worktree 保留在磁盘，供前端“工作区现场”展示与手动恢复。
type worktreeManagerDeps struct {
	Root  func() string                                    // 项目根（原 r.projectScope.Root）
	Phase func(ctx context.Context, nodeID, status string) // 阶段事件（原 r.appendNodePhase）
	Gate  func() approve.ApprovalGate                      // 合并审批门（原 r.currentApprovalGate）
}

type worktreeManager struct {
	mu        sync.Mutex
	worktrees map[string]*nodeWorktree // nodeID → worktree（仅 RoleSubAgent 节点）
	git       func(root string, args ...string) (string, error)
	deps      worktreeManagerDeps
}

func newWorktreeManager(deps worktreeManagerDeps) *worktreeManager {
	return &worktreeManager{
		worktrees: make(map[string]*nodeWorktree),
		git:       gitRunner,
		deps:      deps,
	}
}

// Close 幂等关闭：组件无后台 goroutine（git 子进程由调用方串行），空实现满足
// Shutdown 关闭契约，便于后续演进为 actor。
func (w *worktreeManager) Close() {}

// worktreeFor 返回节点的 worktree（无 → nil）。
func (w *worktreeManager) worktreeFor(nodeID string) *nodeWorktree {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.worktrees[nodeID]
}

// Begin 为节点创建 worktree（降级返回 nil）：
//   - entry 节点（RoleAgent）共享主工作区；
//   - 非 git 仓库 → 降级共享工作区；
//   - 创建成功 → 注册并返回；创建失败 → 降级（不阻断执行）。
func (w *worktreeManager) Begin(scope NodeScope, nodeID string) *nodeWorktree {
	if scope.Role != RoleSubAgent {
		return nil
	}
	root := w.deps.Root()
	if root == "" || !w.isGitRepository(root) {
		return nil
	}
	mainBranch, err := w.git(root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || mainBranch == "" {
		return nil
	}
	baseCommit, err := w.git(root, "rev-parse", "HEAD")
	if err != nil {
		return nil
	}
	wtPath := filepath.Join(filepath.Dir(root), fmt.Sprintf("%s-seelex-%s", filepath.Base(root), nodeID))
	branch := "seelex/" + nodeID
	if _, err := w.git(root, "worktree", "add", "-b", branch, wtPath, "HEAD"); err != nil {
		if _, cleanErr := w.git(root, "worktree", "remove", "--force", wtPath); cleanErr == nil {
			_, _ = w.git(root, "branch", "-D", branch)
			if _, retryErr := w.git(root, "worktree", "add", "-b", branch, wtPath, "HEAD"); retryErr != nil {
				return nil
			}
		} else {
			return nil
		}
	}
	wt := &nodeWorktree{Path: wtPath, Branch: branch, BaseCommit: baseCommit, MainBranch: mainBranch}
	w.mu.Lock()
	w.worktrees[nodeID] = wt
	w.mu.Unlock()
	return wt
}

// Finish 收尾：变基兜底 → 提交判定 → 合并审批 → merge → 清理。
// 返回错误时节点 failed 且 worktree 保留现场。
func (w *worktreeManager) Finish(ctx context.Context, nodeID string, wt *nodeWorktree) error {
	root := w.deps.Root()
	w.deps.Phase(ctx, nodeID, "rebasing")
	behind, err := w.branchBehindBase(wt)
	if err != nil {
		return err
	}
	if behind {
		if out, rebaseErr := w.git(wt.Path, "rebase", wt.MainBranch); rebaseErr != nil {
			conflicts, _ := w.conflictFilesIn(wt.Path)
			return fmt.Errorf("worktree %q: rebase onto %s failed (resolve conflicts in %s): %v\n%s\n冲突文件: %v", nodeID, wt.MainBranch, wt.Path, rebaseErr, out, conflicts)
		}
	}
	commits, err := w.commitCountSince(wt)
	if err != nil {
		return err
	}
	if commits == 0 {
		dirty, err := w.worktreeDirty(wt)
		if err != nil {
			return fmt.Errorf("worktree %q: check dirty state: %w", nodeID, err)
		}
		if dirty {
			return fmt.Errorf("worktree %q: subagent left uncommitted changes (finish protocol git add -A && git commit 未执行); files preserved in %s", nodeID, wt.Path)
		}
		return w.cleanup(root, wt)
	}
	summary, err := w.diffStat(wt)
	if err != nil {
		summary = "diff stat unavailable"
	}
	if err := w.approve(ctx, nodeID, wt, summary); err != nil {
		return err
	}
	w.deps.Phase(ctx, nodeID, "merging")
	if out, mergeErr := w.git(root, "merge", "--no-edit", wt.Branch); mergeErr != nil {
		conflicts, _ := w.conflictFilesIn(root)
		return fmt.Errorf("worktree %q: merge %s into %s failed (resolve conflicts in the main workspace, or git merge --abort): %v\n%s\n冲突文件: %v", nodeID, wt.Branch, wt.MainBranch, mergeErr, out, conflicts)
	}
	return w.cleanup(root, wt)
}

// Release 在节点结束时从注册表移除（成功路径已清理；失败路径保留现场但解除注册，
// 避免后续节点引用已失效路径）。
func (w *worktreeManager) Release(nodeID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.worktrees, nodeID)
}

// Info 返回节点 worktree 现场信息（无现场 → false）。
func (w *worktreeManager) Info(nodeID string) (NodeWorktreeInfo, bool) {
	wt := w.worktreeFor(nodeID)
	if wt == nil {
		return NodeWorktreeInfo{}, false
	}
	return NodeWorktreeInfo{Path: wt.Path, Branch: wt.Branch, MainBranch: wt.MainBranch}, true
}

// approve 合并前审批：复用 SetPlanApprovalGate 注入的审批门；gate 未注入 → 放行。
func (w *worktreeManager) approve(ctx context.Context, nodeID string, wt *nodeWorktree, summary string) error {
	gate := w.deps.Gate()
	if gate == nil {
		return nil
	}
	decision, err := gate.Ask(ctx, approve.Question{
		ID:      "merge-" + nodeID,
		Content: fmt.Sprintf("子代理 %s 的改动将合并进主工作区（%s）。\n%s", nodeID, wt.MainBranch, summary),
		Options: approve.Choices("approve", "reject"),
	})
	if err != nil {
		return err
	}
	choice, _ := decision.(string)
	if choice != "approve" {
		return fmt.Errorf("worktree %q: merge rejected by user (changes preserved in %s)", nodeID, wt.Path)
	}
	return nil
}

func (w *worktreeManager) isGitRepository(root string) bool {
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		return true
	}
	_, err := w.git(root, "rev-parse", "--is-inside-work-tree")
	return err == nil
}

func (w *worktreeManager) branchBehindBase(wt *nodeWorktree) (bool, error) {
	out, err := w.git(wt.Path, "rev-list", "--count", "HEAD.."+wt.MainBranch)
	if err != nil {
		return false, err
	}
	count, convErr := strconv.Atoi(strings.TrimSpace(out))
	if convErr != nil {
		return false, convErr
	}
	return count > 0, nil
}

func (w *worktreeManager) commitCountSince(wt *nodeWorktree) (int, error) {
	out, err := w.git(wt.Path, "rev-list", "--count", wt.BaseCommit+"..HEAD")
	if err != nil {
		return 0, err
	}
	count, convErr := strconv.Atoi(strings.TrimSpace(out))
	if convErr != nil {
		return 0, convErr
	}
	return count, nil
}

func (w *worktreeManager) worktreeDirty(wt *nodeWorktree) (bool, error) {
	out, err := w.git(wt.Path, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func (w *worktreeManager) conflictFilesIn(dir string) ([]string, error) {
	out, err := w.git(dir, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func (w *worktreeManager) diffStat(wt *nodeWorktree) (string, error) {
	out, err := w.git(wt.Path, "diff", "--stat", wt.BaseCommit+"..HEAD")
	if err != nil {
		return "", err
	}
	return out, nil
}

func (w *worktreeManager) cleanup(root string, wt *nodeWorktree) error {
	if _, err := w.git(root, "worktree", "remove", "--force", wt.Path); err != nil {
		return err
	}
	_, _ = w.git(root, "branch", "-D", wt.Branch)
	return nil
}

// gitRunner 执行 git 命令（worktree 测试可用真实 git；命令经 ConfigureHiddenCommand
// 隐藏窗口，Windows 兼容）。60s 超时防挂起。
func gitRunner(root string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	security.ConfigureHiddenCommand(cmd)
	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("git %v: timed out after 60s", args)
	}
	if err != nil {
		return strings.TrimSpace(errOut.String()), fmt.Errorf("git %v: %w", args, err)
	}
	return strings.TrimSpace(out.String()), nil
}

// cleanupWorktree 删除 worktree 及其分支（合并完成后或无可合并提交时）。
// 包级薄包装：worktree_test.go 直接调用；组件内部走 w.cleanup 以支持 fake git 注入。
func cleanupWorktree(root string, wt *nodeWorktree) error {
	if _, err := gitRunner(root, "worktree", "remove", "--force", wt.Path); err != nil {
		return err
	}
	_, _ = gitRunner(root, "branch", "-D", wt.Branch)
	return nil
}

// conflictFilesIn 列出目录（worktree 或主工作区）中的冲突文件
// （rebase/merge 失败后诊断用；rebase 冲突在 worktree，merge 冲突在主工作区）。
// 包级薄包装：worktree_test.go 直接调用；组件内部走 w.conflictFilesIn。
func conflictFilesIn(dir string) ([]string, error) {
	out, err := gitRunner(dir, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}
