package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/Seele/workplan/sugar/approve"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/winhide"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelebridge/security"
	"github.com/RedHuang-0622/seelex/sessionstore"
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
// NodeWorktree 是单个节点的 worktree 现场（plan_run 生命周期内有效）。
type NodeWorktree struct {
	Path       string // worktree 工作目录（NodeScope.WorkspaceID 指向）
	Branch     string // seelex/<nodeID>
	BaseCommit string // 创建时 HEAD（合并提交判定基线）
	MainBranch string // 主工作区当前分支（rebase/merge 目标）
}

// NodeWorktreeInfo 是节点 worktree 现场的只读摘要（恢复数据面）：
// 节点失败/合并被拒时现场保留且注册表不释放——路径就是人工恢复入口。
type NodeWorktreeInfo = dto.NodeWorktreeInfo

// ErrUncommittedChanges 标记「收尾协议未执行」这一类收尾失败：子代理在自己
// 的 worktree 里留下未提交改动（未跟踪或未暂存的产出/临时文件），且没有任何
// 提交。现场必须保留（绝不静默删除产出），但这类失败**不代表节点的产出无效**
// ——子代理的交付物是它的最终汇报，改动是否合并是框架的后续动作。调用方
// （node 域）据此把它降级为显式警告，避免把一个已完成节点的结论连同同批
// 兄弟节点的产出一起丢掉（2026-09-11 事故：lit-en 收尾失败 → fail-fast
// 连坐 lit-cn → 整个 fork 失败，两个子代理产出全丢）。
var ErrUncommittedChanges = errors.New("worktree finish protocol not executed")

// IsUncommittedChanges 判定 err 是否属于「未提交改动」类收尾失败。
func IsUncommittedChanges(err error) bool {
	return errors.Is(err, ErrUncommittedChanges)
}

type WorktreeManagerDeps struct {
	Root  func() string                                    // 项目根（原 r.projectScope.Root）
	Phase func(ctx context.Context, nodeID, status string) // 阶段事件（原 r.appendNodePhase）
	Gate  func() approve.ApprovalGate                      // 合并审批门（原 r.currentApprovalGate）
}

type WorktreeManager struct {
	mu        sync.Mutex
	worktrees map[string]*NodeWorktree // nodeID → worktree（仅 RoleSubAgent 节点）
	git       func(root string, args ...string) (string, error)
	deps      WorktreeManagerDeps
}

func NewWorktreeManager(deps WorktreeManagerDeps) *WorktreeManager {
	return &WorktreeManager{
		worktrees: make(map[string]*NodeWorktree),
		git:       GitRunner,
		deps:      deps,
	}
}

// Close 幂等关闭：组件无后台 goroutine（git 子进程由调用方串行），空实现满足
// Shutdown 关闭契约，便于后续演进为 actor。
func (w *WorktreeManager) Close() {}

// worktreeFor 返回节点的 worktree（无 → nil）。
func (w *WorktreeManager) worktreeFor(nodeID string) *NodeWorktree {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.worktrees[nodeID]
}

// RegisteredCount 返回当前注册的 worktree 数（测试/诊断读取面）。
func (w *WorktreeManager) RegisteredCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.worktrees)
}

// Begin 为节点创建 worktree（降级返回 nil）：
//   - entry 节点（RoleAgent）共享主工作区；
//   - 非 git 仓库 → 降级共享工作区；
//   - 创建成功 → 注册并返回；创建失败 → 降级（不阻断执行）。
func (w *WorktreeManager) Begin(scope model.NodeScope, nodeID string) *NodeWorktree {
	if scope.Role != model.RoleSubAgent {
		return nil
	}
	// 幂等：同一 nodeID 已有在册现场就直接复用，绝不重建。路径/分支只按 nodeID
	// 命名（`<repo>-seelex-<nodeID>` / `seelex/<nodeID>`），所以跨会话、跨批次同名
	// 的第二次 Begin 会指向**同一个目录**——重建前那句 `worktree remove --force`
	// 会把一个正在被使用的现场删掉。
	if existing := w.worktreeFor(nodeID); existing != nil {
		return existing
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
		// 清理只针对**本管理器不认得的**残留目录：在册现场（可能正被另一个会话的
		// 同名节点使用）不是残留，删它等于删别人的现场。那种情况一律降级为共享
		// 工作区，而不是毁掉一个活着的现场。
		if w.pathRegistered(wtPath) {
			return nil
		}
		if _, cleanErr := w.git(root, "worktree", "remove", "--force", wtPath); cleanErr == nil {
			_, _ = w.git(root, "branch", "-D", branch)
			if _, retryErr := w.git(root, "worktree", "add", "-b", branch, wtPath, "HEAD"); retryErr != nil {
				return nil
			}
		} else {
			return nil
		}
	}
	wt := &NodeWorktree{Path: wtPath, Branch: branch, BaseCommit: baseCommit, MainBranch: mainBranch}
	w.mu.Lock()
	w.worktrees[nodeID] = wt
	w.mu.Unlock()
	return wt
}

// Finish 收尾：变基兜底 → 提交判定 → 合并审批 → merge → 清理。
// 返回错误时节点 failed 且 worktree 保留现场。
func (w *WorktreeManager) Finish(ctx context.Context, nodeID string, wt *NodeWorktree) error {
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
			return fmt.Errorf("worktree %q: subagent left uncommitted changes (finish protocol git add -A && git commit 未执行); files preserved in %s: %w", nodeID, wt.Path, ErrUncommittedChanges)
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
func (w *WorktreeManager) Release(nodeID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.worktrees, nodeID)
}

// Restore 从持久化记录重建 worktree 注册表（重启/恢复锚点）：
// 崩溃遗留节点的现场信息（path/branch/baseCommit）重新登记，
// NodeWorktreeInfoFor 恢复可用。
//
// **已不存在的目录不登记**：手工删掉目录（或它从未真正建成）后，把路径重新登记成
// 「现场」会造出幽灵条目——`Info` 会报一个不存在的路径，`team_retire` 步 2 会对着
// 它跑 `git status` 而失败。恢复的判据是「锚点 + 目录真的在」。
func (w *WorktreeManager) Restore(records []sessionstore.NodeSessionRecord) {
	if w == nil || len(records) == 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, record := range records {
		wt := record.Worktree
		if record.NodeID == "" || wt.Path == "" || wt.Branch == "" {
			continue
		}
		if info, err := os.Stat(wt.Path); err != nil || !info.IsDir() {
			continue
		}
		w.worktrees[record.NodeID] = &NodeWorktree{
			Path:       wt.Path,
			Branch:     wt.Branch,
			BaseCommit: wt.BaseCommit,
			MainBranch: wt.MainBranch,
		}
	}
}

// Info 返回节点 worktree 现场信息（无现场 → false）。
func (w *WorktreeManager) Info(nodeID string) (NodeWorktreeInfo, bool) {
	wt := w.worktreeFor(nodeID)
	if wt == nil {
		return NodeWorktreeInfo{}, false
	}
	return NodeWorktreeInfo{Path: wt.Path, Branch: wt.Branch, MainBranch: wt.MainBranch}, true
}

// pathRegistered 报告某个 worktree 路径是否是本管理器**在册**的现场
// （可能正被某个节点使用，因此不是可以随手删掉的残留）。
func (w *WorktreeManager) pathRegistered(path string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, wt := range w.worktrees {
		if wt.Path == path {
			return true
		}
	}
	return false
}

// registeredPaths 返回在册现场路径的集合快照。
func (w *WorktreeManager) registeredPaths() map[string]bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	paths := make(map[string]bool, len(w.worktrees))
	for _, wt := range w.worktrees {
		paths[wt.Path] = true
	}
	return paths
}

// PruneResult 是一次残留回收的读数：Removed = 已回收的孤儿 worktree 路径，
// Kept = 因有未提交改动而**故意保留**的现场路径。
type PruneResult struct {
	Removed []string
	Kept    []string
}

// Prune 回收**孤儿 worktree**：磁盘上仍是本仓库的 worktree（路径符合本管理器的
// 命名），但注册表里没有它。
//
// 为什么需要兜底清理器：worktree 只在成功收尾时 `git worktree remove`；失败/中断的
// 现场按设计「一律保留」（`Release` 只解除注册、不删磁盘），而全仓没有第二处清理——
// 每个残留 = 一份完整项目检出 + 一个 `seelex/<id>` 分支，磁盘随历史失败数无界增长。
//
// 判据（两条**都要**成立才删）：
//   - **不在册**：注册表（会话作用域的现场锚点）里没有它。恢复锚点必须先经 `Restore`
//     登记，否则恢复出来的现场会被这里当残留删掉——调用方务必先恢复、后清理。
//   - **干净**：没有未提交改动。现场的未提交产出是人的资产，框架不替人做
//     「丢还是留」的决定（与 `ErrUncommittedChanges` 同一口径）。
//
// 未列在本仓库 worktree 清单里的路径一律不碰；`.git/worktrees` 里已经 prunable 的
// 元数据（手工删过目录）顺手用 `git worktree prune` 清掉。
func (w *WorktreeManager) Prune() (PruneResult, error) {
	result := PruneResult{}
	if w == nil {
		return result, nil
	}
	root := w.deps.Root()
	if root == "" || !w.isGitRepository(root) {
		return result, nil
	}
	entries, err := w.listWorktrees(root)
	if err != nil {
		return result, err
	}
	registered := w.registeredPaths()
	for _, entry := range entries {
		if entry.path == root || !w.isManagedPath(root, entry.path) || registered[entry.path] {
			continue
		}
		dirty, dirtyErr := w.pathDirty(entry.path)
		if dirtyErr != nil || dirty {
			result.Kept = append(result.Kept, entry.path)
			continue
		}
		if _, removeErr := w.git(root, "worktree", "remove", "--force", entry.path); removeErr != nil {
			result.Kept = append(result.Kept, entry.path)
			continue
		}
		if entry.branch != "" {
			_, _ = w.git(root, "branch", "-D", entry.branch)
		}
		result.Removed = append(result.Removed, entry.path)
	}
	// 手删目录会留下 .git/worktrees 下的 prunable 元数据，且没有任何别的路径会清它。
	_, _ = w.git(root, "worktree", "prune")
	return result, nil
}

// worktreeEntry 是 `git worktree list --porcelain` 的一条读数。
type worktreeEntry struct{ path, branch string }

// listWorktrees 列出本仓库的全部 worktree（main + 各现场）。
func (w *WorktreeManager) listWorktrees(root string) ([]worktreeEntry, error) {
	out, err := w.git(root, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var entries []worktreeEntry
	current := worktreeEntry{}
	flush := func() {
		if current.path != "" {
			entries = append(entries, current)
		}
		current = worktreeEntry{}
	}
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "worktree "):
			current.path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch "):
			current.branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	flush()
	return entries, nil
}

// isManagedPath 判定一个 worktree 路径是否由本管理器命名（`<repoBase>-seelex-<nodeID>`，
// 与 Begin 同一条命名规则）。
func (w *WorktreeManager) isManagedPath(root, path string) bool {
	prefix := filepath.Base(root) + "-seelex-"
	return strings.HasPrefix(filepath.Base(path), prefix)
}

// pathDirty 报告某个 worktree 是否有未提交改动。
func (w *WorktreeManager) pathDirty(path string) (bool, error) {
	out, err := w.git(path, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// approve 合并前审批：复用 SetPlanApprovalGate 注入的审批门；gate 未注入 → 放行。
func (w *WorktreeManager) approve(ctx context.Context, nodeID string, wt *NodeWorktree, summary string) error {
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

func (w *WorktreeManager) isGitRepository(root string) bool {
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		return true
	}
	_, err := w.git(root, "rev-parse", "--is-inside-work-tree")
	return err == nil
}

func (w *WorktreeManager) branchBehindBase(wt *NodeWorktree) (bool, error) {
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

func (w *WorktreeManager) commitCountSince(wt *NodeWorktree) (int, error) {
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

func (w *WorktreeManager) worktreeDirty(wt *NodeWorktree) (bool, error) {
	out, err := w.git(wt.Path, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func (w *WorktreeManager) conflictFilesIn(dir string) ([]string, error) {
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

func (w *WorktreeManager) diffStat(wt *NodeWorktree) (string, error) {
	out, err := w.git(wt.Path, "diff", "--stat", wt.BaseCommit+"..HEAD")
	if err != nil {
		return "", err
	}
	return out, nil
}

func (w *WorktreeManager) cleanup(root string, wt *NodeWorktree) error {
	if _, err := w.git(root, "worktree", "remove", "--force", wt.Path); err != nil {
		return err
	}
	_, _ = w.git(root, "branch", "-D", wt.Branch)
	return nil
}

// gitRunner 执行 git 命令（worktree 测试可用真实 git；命令经 ConfigureHiddenCommand
// 隐藏窗口，Windows 兼容）。60s 超时防挂起。
func GitRunner(root string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	winhide.Apply(cmd)
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
func CleanupWorktree(root string, wt *NodeWorktree) error {
	if _, err := GitRunner(root, "worktree", "remove", "--force", wt.Path); err != nil {
		return err
	}
	_, _ = GitRunner(root, "branch", "-D", wt.Branch)
	return nil
}

// ConflictFilesIn 列出目录（worktree 或主工作区）中的冲突文件
// （rebase/merge 失败后诊断用；rebase 冲突在 worktree，merge 冲突在主工作区）。
// 包级薄包装：worktree_test.go 直接调用；组件内部走 w.conflictFilesIn。
func ConflictFilesIn(dir string) ([]string, error) {
	out, err := GitRunner(dir, "diff", "--name-only", "--diff-filter=U")
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
