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
)

// ── 子代理 worktree 生命周期（docs/2026-08-03-subagent-fork-architecture/plan.md §3）──
// 每个 Plan kind:agent 节点在独立 git worktree 中执行：
// 开（worktree add）→ 切（NodeScope.WorkspaceID = worktree 路径，scoped_tools
// 按节点分根）→ 干（子代理 Session）→ 变基（子代理自己做，框架兜底）→
// 合并审批（approve gate）→ merge 回主工作区 → 清理。
// 降级：非 git 仓库 / worktree 创建失败 → 共享工作区（路径门禁语义不变）；
// 失败节点保留现场（worktree 不清理，供排查）。

// nodeWorktree 是单个节点的 worktree 现场（plan_run 生命周期内有效）。
type nodeWorktree struct {
	Path       string // worktree 工作目录（NodeScope.WorkspaceID 指向）
	Branch     string // seelex/<nodeID>
	BaseCommit string // 创建时 HEAD（合并/提交判定基线）
	MainBranch string // 主工作区当前分支（rebase/merge 目标）
}

// gitRunner 执行 git 命令（worktree 测试可用真实 git；命令经 configureHiddenCommand
// 隐藏窗口，Windows 兼容）。60s 超时防挂死。
func gitRunner(root string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	configureHiddenCommand(cmd)
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

// isGitRepository 判断根目录是否为 git 工作树（.git 存在或 rev-parse 成功）。
func isGitRepository(root string) bool {
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		return true
	}
	_, err := gitRunner(root, "rev-parse", "--is-inside-work-tree")
	return err == nil
}

// ── Runtime worktree 状态 ─────────────────────────────────────────────

// worktreeMu 保护 worktrees 注册表（plan_run 内节点并发写）。
// git 子进程操作由 gitRunner 串行隔离（git 自身有锁，分支互不冲突）。
type worktreeState struct {
	mu        sync.Mutex
	worktrees map[string]*nodeWorktree // nodeID → worktree（仅 RoleSubAgent 节点）
}

func newWorktreeState() *worktreeState {
	return &worktreeState{worktrees: make(map[string]*nodeWorktree)}
}

// nodeWorktreeFor 返回节点的 worktree（无 → nil）。
func (r *Runtime) nodeWorktreeFor(nodeID string) *nodeWorktree {
	r.wt.mu.Lock()
	defer r.wt.mu.Unlock()
	return r.wt.worktrees[nodeID]
}

// beginNodeWorktree 为节点创建 worktree（降级返回 nil）：
//   - entry 节点（RoleAgent）共享主工作区；
//   - 非 git 仓库 → 降级共享工作区；
//   - 创建成功 → 注册 + 返回；创建失败 → 降级（不阻断执行）。
func (r *Runtime) beginNodeWorktree(scope NodeScope, nodeID string) *nodeWorktree {
	if scope.Role != RoleSubAgent {
		return nil
	}
	root := r.projectScope.Root()
	if root == "" || !isGitRepository(root) {
		return nil
	}
	mainBranch, err := gitRunner(root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || mainBranch == "" {
		return nil
	}
	baseCommit, err := gitRunner(root, "rev-parse", "HEAD")
	if err != nil {
		return nil
	}
	// worktree 路径：主仓库 sibling 目录 <root>-seelex-<nodeID>（避免 Windows
	// 长路径与主仓库内嵌套的 git 递归）。
	wtPath := filepath.Join(filepath.Dir(root), fmt.Sprintf("%s-seelex-%s", filepath.Base(root), nodeID))
	branch := "seelex/" + nodeID
	if _, err := gitRunner(root, "worktree", "add", "-b", branch, wtPath, "HEAD"); err != nil {
		// 分支已存在（上次失败残留）→ 清理后重试一次；仍失败降级共享工作区。
		if _, cleanErr := gitRunner(root, "worktree", "remove", "--force", wtPath); cleanErr == nil {
			_, _ = gitRunner(root, "branch", "-D", branch)
			if _, retryErr := gitRunner(root, "worktree", "add", "-b", branch, wtPath, "HEAD"); retryErr != nil {
				return nil
			}
		} else {
			return nil
		}
	}
	wt := &nodeWorktree{Path: wtPath, Branch: branch, BaseCommit: baseCommit, MainBranch: mainBranch}
	r.wt.mu.Lock()
	r.wt.worktrees[nodeID] = wt
	r.wt.mu.Unlock()
	return wt
}

// finishNodeWorktree 收尾：变基兜底 → 提交判定 → 合并审批 → merge → 清理。
// 返回错误时节点 failed 且 worktree 保留现场。
func (r *Runtime) finishNodeWorktree(ctx context.Context, nodeID string, wt *nodeWorktree) error {
	root := r.projectScope.Root()
	r.appendNodePhase(ctx, nodeID, "rebasing")
	// 1) 变基兜底：子代理未 rebase 且分支落后主分支 → 框架执行（冲突 → 报错保留）。
	behind, err := branchBehindBase(wt)
	if err != nil {
		return err
	}
	if behind {
		if out, rebaseErr := gitRunner(wt.Path, "rebase", wt.MainBranch); rebaseErr != nil {
			// 冲突诊断：列出未合并文件，指导子代理/主代理在 worktree 现场解决。
			conflicts, _ := conflictFilesIn(wt.Path)
			return fmt.Errorf("worktree %q: rebase onto %s failed (resolve conflicts in %s): %v\n%s\n冲突文件: %v", nodeID, wt.MainBranch, wt.Path, rebaseErr, out, conflicts)
		}
	}
	// 2) 提交判定：相对创建时基线无提交 → 无需合并；但若 worktree 内仍有
	//    未提交/未跟踪改动（子代理没按收尾协议 commit——设计 §风险表要求
	//    "框架检测'worktree 脏且未 commit'报错"），直接清理会静默删除产出，
	//    必须报错保留现场，禁止静默数据丢失。
	commits, err := commitCountSince(wt)
	if err != nil {
		return err
	}
	if commits == 0 {
		dirty, err := worktreeDirty(wt)
		if err != nil {
			return fmt.Errorf("worktree %q: check dirty state: %w", nodeID, err)
		}
		if dirty {
			return fmt.Errorf("worktree %q: subagent left uncommitted changes (finish protocol git add -A && git commit 未执行); files preserved in %s", nodeID, wt.Path)
		}
		return cleanupWorktree(root, wt)
	}
	// 3) 合并审批（用户确认 diff 摘要进主工作区）。
	summary, err := diffStat(wt)
	if err != nil {
		summary = "diff stat unavailable"
	}
	if err := r.approveMerge(ctx, nodeID, wt, summary); err != nil {
		return err // 拒绝：节点 failed，现场保留
	}
	// 4) merge 回主工作区 + 清理。
	r.appendNodePhase(ctx, nodeID, "merging")
	if out, mergeErr := gitRunner(root, "merge", "--no-edit", wt.Branch); mergeErr != nil {
		// 冲突发生在主工作区：列出冲突文件，指导用户解决或回退
		// （git merge --abort 后现场仍保留在 worktree）。
		conflicts, _ := conflictFilesIn(root)
		return fmt.Errorf("worktree %q: merge %s into %s failed (resolve conflicts in the main workspace, or git merge --abort): %v\n%s\n冲突文件: %v", nodeID, wt.Branch, wt.MainBranch, mergeErr, out, conflicts)
	}
	return cleanupWorktree(root, wt)
}

// approveMerge 合并前审批：复用 SetPlanApprovalGate 注入的审批门。
// gate 未注入 → 放行（框架兜底，配置缺省不阻塞）。
func (r *Runtime) approveMerge(ctx context.Context, nodeID string, wt *nodeWorktree, summary string) error {
	gate := r.currentApprovalGate()
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

// branchBehindBase 判断 worktree 分支是否落后主分支（变基兜底判定）。
func branchBehindBase(wt *nodeWorktree) (bool, error) {
	out, err := gitRunner(wt.Path, "rev-list", "--count", "HEAD.."+wt.MainBranch)
	if err != nil {
		return false, err
	}
	count, convErr := strconv.Atoi(strings.TrimSpace(out))
	if convErr != nil {
		return false, convErr
	}
	return count > 0, nil
}

// commitCountSince 返回 worktree 相对创建基线的新增提交数。
func commitCountSince(wt *nodeWorktree) (int, error) {
	out, err := gitRunner(wt.Path, "rev-list", "--count", wt.BaseCommit+"..HEAD")
	if err != nil {
		return 0, err
	}
	count, convErr := strconv.Atoi(strings.TrimSpace(out))
	if convErr != nil {
		return 0, convErr
	}
	return count, nil
}

// worktreeDirty 判断 worktree 工作区是否有未提交/未跟踪改动
// （git status --porcelain 非空；含 untracked，防止只查已跟踪改动的漏网）。
func worktreeDirty(wt *nodeWorktree) (bool, error) {
	out, err := gitRunner(wt.Path, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// conflictFilesIn 列出目录（worktree 或主工作区）中的冲突文件
// （rebase/merge 失败后诊断用；rebase 冲突在 worktree，merge 冲突在主工作区）。
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

// diffStat 生成节点改动摘要（合并审批展示）。
func diffStat(wt *nodeWorktree) (string, error) {
	out, err := gitRunner(wt.Path, "diff", "--stat", wt.BaseCommit+"..HEAD")
	if err != nil {
		return "", err
	}
	return out, nil
}

// cleanupWorktree 删除 worktree 及其分支（合并完成后或无可合并提交时）。
func cleanupWorktree(root string, wt *nodeWorktree) error {
	if _, err := gitRunner(root, "worktree", "remove", "--force", wt.Path); err != nil {
		return err
	}
	_, _ = gitRunner(root, "branch", "-D", wt.Branch)
	return nil
}

// releaseNodeWorktree 在节点结束时从注册表移除（成功路径已清理；失败路径
// 保留现场但解除注册，避免后续节点引用已失效路径）。
func (r *Runtime) releaseNodeWorktree(nodeID string) {
	r.wt.mu.Lock()
	defer r.wt.mu.Unlock()
	delete(r.wt.worktrees, nodeID)
}
