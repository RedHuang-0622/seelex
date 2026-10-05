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
	"github.com/RedHuang-0622/seelex/seelebridge/internal/actor"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelebridge/security"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// ─── 子代理 worktree 生命周期管理器（Runtime 装配件拆分 Step 1）───
//
// 原 Runtime 直接持有 wt *worktreeState（worktree.go）。本组件把 worktree 注册表
// 与生命周期（begin → finish → release）收进独立组件；git 执行经可注入的 git
// 字段（默认 GitRunner，测试可替换为 fake），不依赖 Runtime / ProjectScope /
// PlanEventSink——项目根、阶段事件与审批门经 WorktreeManagerDeps 注入，保持单向
// 依赖。
//
// 串行化（2026-10-05 修正）：本组件曾经假设「git 子进程由调用方串行」，而调用方
// 恰好不串行——fork 的一批子代理是**并发收尾**的，两个节点同时 rebase/merge/cleanup
// 同一个主工作区就会撞 .git/index.lock、交错合并。收尾段（rebase → 提交判定 → 审批
// → merge → cleanup）因此收进 **finishActor 单写者**：同一时刻只有一个收尾在动主
// 工作区。主工作区被在途改动挡住时不再把节点判死，而是有界重试（阶段
// awaiting_merge），超预算返回 ErrMergeBlockedByMain（现场保留、结论照常交付）。
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

// ErrMergeBlockedByMain 标记「合并被主工作区的在途改动挡住」这一类收尾失败：与
// ErrUncommittedChanges 同族——**不代表节点产出无效**，只是"这次没合进去"。典型
// 成因是主工作区里有未提交改动，正好压在本次合并要改的路径上（git 拒绝覆盖本地
// 改动而中止），或 .git 索引被别的 git 进程短暂占用。
//
// 处置口径（与"判死"的区别）：收尾段会先有界重试（阶段 awaiting_merge，等主工作区
// 干净），超预算才把本错误交回调用方。调用方（node 域）据此降级为显式警告：现场
// 保留 + 产出照常交付，并把"先提交/暂存主工作区的在途改动，再重试合并"写进结果
// 文本，交给父代理或用户处理。
var ErrMergeBlockedByMain = errors.New("merge blocked by in-flight changes in the main workspace")

// IsMergeBlockedByMain 判定 err 是否属于「被主工作区挡住」类收尾失败。
func IsMergeBlockedByMain(err error) bool {
	return errors.Is(err, ErrMergeBlockedByMain)
}

// mergeBlockedError 携带 git 的原始证据（被挡住的路径/索引争用正文）与现场路径，
// Unwrap 到 ErrMergeBlockedByMain 供调用方分类。
type mergeBlockedError struct {
	nodeID string
	path   string
	detail string
}

func (e *mergeBlockedError) Error() string {
	return fmt.Sprintf("worktree %q: merge blocked by in-flight changes in the main workspace: %s（现场保留在 %s）", e.nodeID, e.detail, e.path)
}

func (e *mergeBlockedError) Unwrap() error { return ErrMergeBlockedByMain }

// mergeBlockedMarkers 是 git 的「主工作区挡路」判据（大小写不敏感子串）：
//
//   - 「本地改动会被覆盖」族：merge/checkout 拒绝覆盖未提交改动；
//   - 未跟踪文件会被覆盖：同样是"主工作区里有东西挡着"；
//   - 索引/引用锁争用：另一个 git 进程正在动同一个 .git（收尾串行化之后这一条
//     只应来自**主代理自己的 git 命令**，属可重试的瞬时态）。
//
// 判据全部来自"这次合并**没开始**就中止"，因此重试是安全的：主工作区没被改成
// 半成品（真冲突不在此列——它会把 MERGE_HEAD 与冲突索引留在主工作区，属确定性
// 失败，必须由人或主代理收拾）。
var mergeBlockedMarkers = []string{
	"would be overwritten by merge",
	"would be overwritten by checkout",
	"please commit your changes or stash them",
	"your local changes to the following files",
	"untracked working tree files would be overwritten",
	"index.lock",
	"cannot lock ref",
	"another git process seems to be running",
	"unable to create '",
}

// isMergeBlockedEvidence 判定一次失败的 git 调用是否属于「主工作区挡路」。
func isMergeBlockedEvidence(stderr string, callErr error) bool {
	evidence := strings.ToLower(stderr)
	if callErr != nil {
		evidence += " " + strings.ToLower(callErr.Error())
	}
	for _, marker := range mergeBlockedMarkers {
		if strings.Contains(evidence, marker) {
			return true
		}
	}
	return false
}

type WorktreeManagerDeps struct {
	Root  func() string                                    // 项目根（原 r.projectScope.Root）
	Phase func(ctx context.Context, nodeID, status string) // 阶段事件（原 r.appendNodePhase）
	Gate  func() approve.ApprovalGate                      // 合并审批门（原 r.currentApprovalGate）
}

// 收尾串行化与重试常量。
const (
	// finishMailboxCap 是收尾命令待办上限：积压只可能来自"很多节点同时收工"。
	finishMailboxCap = 256
	// finishSubmitTimeout 是投递上限：actor 已关闭或队列异常时调用方不被无限挂住。
	finishSubmitTimeout = 2 * time.Second
	// defaultMergeRetryBudget / defaultMergeRetryInterval 是「主工作区挡路」的默认
	// 重试预算：等待**不占 actor**（其他节点照常收尾），超预算落 ErrMergeBlockedByMain。
	defaultMergeRetryBudget   = 2 * time.Minute
	defaultMergeRetryInterval = 5 * time.Second
	// phaseAwaitingMerge 是"等主工作区干净"的阶段名（→ node 阶段 → 前端可见）。
	phaseAwaitingMerge = "awaiting_merge"
)

type WorktreeManager struct {
	mu        sync.Mutex
	worktrees map[string]*NodeWorktree // nodeID → worktree（仅 RoleSubAgent 节点）
	git       func(root string, args ...string) (string, error)
	deps      WorktreeManagerDeps
	// finishActor 是收尾段的**单写者**：rebase / merge / cleanup 都要动主工作区的
	// 索引与 .git，而 fork 的一批子代理是**并发收工**的——谁都不排队就会撞
	// index.lock、交错合并、cleanup 撞车。唯一消费者 = 同一时刻只有一个收尾在跑。
	// （nil = 未装配 actor，直接执行；测试用它做"无串行化"对照。）
	finishActor *actor.Actor[finishCommand]
	// mergeRetryBudget / mergeRetryInterval 见 defaultMergeRetry*（可注入以便测试）。
	mergeRetryBudget   time.Duration
	mergeRetryInterval time.Duration
}

// finishCommand 是投给 finishActor 的一条收尾命令。
type finishCommand struct {
	ctx          context.Context
	nodeID       string
	wt           *NodeWorktree
	skipApproval bool // 重试不再重复询问审批门（同一个 nodeID 只问一次）
	reply        chan error
}

func NewWorktreeManager(deps WorktreeManagerDeps) *WorktreeManager {
	manager := &WorktreeManager{
		worktrees:          make(map[string]*NodeWorktree),
		git:                GitRunner,
		deps:               deps,
		mergeRetryBudget:   defaultMergeRetryBudget,
		mergeRetryInterval: defaultMergeRetryInterval,
	}
	manager.finishActor = actor.New(manager.handleFinish, actor.WithCap(finishMailboxCap))
	return manager
}

// handleFinish 是 finishActor 的唯一消费者：串行执行收尾段并把结果回给投递方。
func (w *WorktreeManager) handleFinish(command finishCommand) {
	err := w.finishExclusive(command.ctx, command.nodeID, command.wt, command.skipApproval)
	select {
	case command.reply <- err:
	default:
		// 投递方已放弃等待（超时/上下文取消）：不阻塞消费者。
	}
}

// submitFinish 把一次收尾投给单写者并等结果。
func (w *WorktreeManager) submitFinish(ctx context.Context, nodeID string, wt *NodeWorktree, skipApproval bool) error {
	if w.finishActor == nil {
		return w.finishExclusive(ctx, nodeID, wt, skipApproval)
	}
	reply := make(chan error, 1)
	command := finishCommand{ctx: ctx, nodeID: nodeID, wt: wt, skipApproval: skipApproval, reply: reply}
	if !w.finishActor.SendTimeout(command, finishSubmitTimeout) {
		return fmt.Errorf("worktree %q: finish queue is closed or saturated", nodeID)
	}
	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-w.finishActor.Done():
		return fmt.Errorf("worktree %q: worktree manager is closed", nodeID)
	}
}

// Close 幂等关闭：停收尾单写者（处理完当前命令即退出）并等它收工。
func (w *WorktreeManager) Close() {
	if w == nil || w.finishActor == nil {
		return
	}
	w.finishActor.Close()
	w.finishActor.Wait()
}

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
	return w.beginNamed(nodeID)
}

// BeginNamed 为非子代理的**编排节点**创建 worktree：teammate 的每一个 Work Item 都是
// 这样一个节点（一 Work Item 一个 worktree）。它与 Begin 走同一条实现、同一套命名与
// 幂等规则，只是不要求调用方先构造一个 NodeScope——编排面只认"这件事的 id"。
func (w *WorktreeManager) BeginNamed(nodeID string) *NodeWorktree { return w.beginNamed(nodeID) }

// WorktreeForNode 返回在册现场（无 → nil）。合并/释放按 id 取现场时用。
func (w *WorktreeManager) WorktreeForNode(nodeID string) *NodeWorktree { return w.worktreeFor(nodeID) }

// beginNamed 为 nodeID 开一个现场（幂等）。四种情况**分清楚**，任何一条路径都
// 不得对已存在的现场执行 `git worktree remove --force`：
//
//	① 目录已在本 repo 的 worktree 清单里 → 认领（重启后重派同一 nodeID 走这一条）；
//	② 目录在、却不是本管理器认得的工作树 → 不碰它，降级共享工作区；
//	③ 目录不在而 `seelex/<nodeID>` 分支已在 → 用**既有分支**建现场（不加 `-b`）；
//	④ 目录与分支都不在 → 新建（`-b`）。
//
// 修复前的第 ①/③ 两条都不存在：第 ① 条被当成"残留"（`worktree remove --force` 丢掉
// 未提交产出 + `branch -D`），第 ③ 条因 `add -b` 撞上已存在的分支而必然失败——两条合在
// 一起就是"重启后现场被当孤儿删掉 / 重派丢未提交产出"（F4/F5）。
func (w *WorktreeManager) beginNamed(nodeID string) *NodeWorktree {
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
	wtPath := w.scenePath(root, nodeID)
	branch := "seelex/" + nodeID

	// ① 既有现场：认领而不是重建。重启（新管理器、空注册表）之后重派同一个 nodeID
	//    走的就是这一条——旧目录与它里面的未提交产出原样留下。
	if adopted := w.Adopt(nodeID); adopted != nil {
		return adopted
	}
	// ② 目录在、却不在 `git worktree list` 里：那不是本管理器的资产（别人放的目录、
	//    或不是工作树的残留）。删它等于替人决定丢东西——不碰，降级。
	if _, statErr := os.Stat(wtPath); statErr == nil {
		return nil
	}
	// ③ 分支已在而目录不在：用**既有分支**建现场（`worktree add <path> <branch>`，
	//    不用 `-b`）。分支上已经提交的产出因此能找回，而不是被 `branch -D` 丢掉。
	if w.branchExists(root, branch) {
		if _, err := w.git(root, "worktree", "add", wtPath, branch); err != nil {
			return nil
		}
		return w.register(nodeID, &NodeWorktree{
			Path: wtPath, Branch: branch,
			BaseCommit: w.baselineFor(branch, mainBranch), MainBranch: mainBranch,
		})
	}
	// ④ 目录与分支都不在：新建。
	if _, err := w.git(root, "worktree", "add", "-b", branch, wtPath, "HEAD"); err != nil {
		return nil
	}
	return w.register(nodeID, &NodeWorktree{
		Path: wtPath, Branch: branch, BaseCommit: baseCommit, MainBranch: mainBranch,
	})
}

// Adopt 认领一个**既有的**现场（按 nodeID）：把磁盘上已经建好、且确实是本 repo 的
// worktree 的目录重新登记进注册表——不重建、不 remove、不 branch -D。
//
// 为什么必须有它：Begin/beginNamed 的幂等只看内存注册表，而注册表是进程态。重启之后
// 一个"已经被建出来"的现场在新管理器眼里等于不存在（F4 探针实测：`worktree add -b`
// 因分支已存在而失败 → 落到残留清理分支 → 目录与 `seelex/<nodeID>` 分支一起没了）。
// 认领把它重新变成在册现场，下游两件事因此成立：`bindWorkerProjectRoot` 能拿到现场
// 路径（而不是回退主会话根），`Prune` 的"不在册"判据不再误判它是孤儿。
//
// 判据（三条都要成立，任一不成立一律返回 nil，绝不猜测、绝不清理）：
//   - 路径符合本管理器的命名（`<repoBase>-seelex-<nodeID>`）；
//   - 目录真的在；
//   - 它是本 repo 在 `git worktree list` 里的一行。
//
// 已在册 → 直接返回既有登记（幂等）。
func (w *WorktreeManager) Adopt(nodeID string) *NodeWorktree {
	if w == nil || strings.TrimSpace(nodeID) == "" {
		return nil
	}
	if existing := w.worktreeFor(nodeID); existing != nil {
		return existing
	}
	root := w.deps.Root()
	if root == "" || !w.isGitRepository(root) {
		return nil
	}
	wtPath := w.scenePath(root, nodeID)
	if !w.isManagedPath(root, wtPath) {
		return nil
	}
	entry, ok := w.worktreeEntryAt(root, wtPath)
	if !ok {
		return nil
	}
	branch := strings.TrimSpace(entry.branch)
	if branch == "" {
		branch = "seelex/" + nodeID
	}
	mainBranch, _ := w.git(root, "rev-parse", "--abbrev-ref", "HEAD")
	return w.register(nodeID, &NodeWorktree{
		Path: wtPath, Branch: branch,
		BaseCommit: w.baselineFor(branch, mainBranch), MainBranch: mainBranch,
	})
}

// scenePath 返回 nodeID 的现场路径（与 beginNamed 同一条命名）。
func (w *WorktreeManager) scenePath(root, nodeID string) string {
	return filepath.Join(filepath.Dir(root), fmt.Sprintf("%s-seelex-%s", filepath.Base(root), nodeID))
}

// register 把一份现场登记进注册表并返回它。
func (w *WorktreeManager) register(nodeID string, wt *NodeWorktree) *NodeWorktree {
	w.mu.Lock()
	w.worktrees[nodeID] = wt
	w.mu.Unlock()
	return wt
}

// worktreeEntryAt 返回某路径在 `git worktree list` 里的登记（目录不在或不在册 → false）。
//
// 这是"认领"与"扫目录"的分界：判据来自 git 自己的登记，而不是"磁盘上有个同名目录"。
func (w *WorktreeManager) worktreeEntryAt(root, path string) (worktreeEntry, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return worktreeEntry{}, false
	}
	entries, err := w.listWorktrees(root)
	if err != nil {
		return worktreeEntry{}, false
	}
	for _, entry := range entries {
		if worktreePathEqual(entry.path, path) {
			return entry, true
		}
	}
	return worktreeEntry{}, false
}

// branchExists 报告本地分支是否已存在。判据取"rev-parse 拿到了一个非空输出"：
// 分支不存在时 git 报错，形状因版本而异，空输出不算存在。
func (w *WorktreeManager) branchExists(root, branch string) bool {
	out, err := w.git(root, "rev-parse", "--verify", "refs/heads/"+branch)
	return err == nil && strings.TrimSpace(out) != ""
}

// baselineFor 返回分支与主分支的分叉点，作为合并判定基线（BaseCommit）。
// 算不出来（分支/主分支名缺失、git 不可用）→ 退回主分支 HEAD。
func (w *WorktreeManager) baselineFor(branch, mainBranch string) string {
	root := w.deps.Root()
	if root != "" && branch != "" && mainBranch != "" {
		if out, err := w.git(root, "merge-base", branch, mainBranch); err == nil {
			if trimmed := strings.TrimSpace(out); trimmed != "" {
				return trimmed
			}
		}
	}
	out, _ := w.git(root, "rev-parse", "HEAD")
	return strings.TrimSpace(out)
}

// Finish 收尾：由**单写者 actor** 串行执行 变基兜底 → 提交判定 → 合并审批 → merge
// → 清理（同批子代理并发收工也不会撞 .git）。任何错误返回时 worktree 保留现场。
//
// 「主工作区挡路」（未提交改动正好压在本次合并要改的路径上、或索引被别的 git 进程
// 短暂占用）**不是确定性失败**：预算内有界重试（阶段 awaiting_merge，且不再重复询问
// 审批门），超预算才返回 ErrMergeBlockedByMain——现场保留、产出照常交付，由父代理或
// 用户先提交/暂存主工作区的在途改动，随后重试即可合上。
func (w *WorktreeManager) Finish(ctx context.Context, nodeID string, wt *NodeWorktree) error {
	if wt == nil {
		return nil
	}
	deadline := time.Now().Add(w.mergeRetryBudget)
	attempt := 0
	skipApproval := false
	for {
		attempt++
		err := w.submitFinish(ctx, nodeID, wt, skipApproval)
		if err == nil || !IsMergeBlockedByMain(err) {
			return err
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			return fmt.Errorf("%w：%v（本次已尝试 %d 次；先提交或暂存主工作区的在途改动，再重试合并）", ErrMergeBlockedByMain, err, attempt)
		}
		w.deps.Phase(ctx, nodeID, phaseAwaitingMerge)
		select {
		case <-time.After(w.mergeRetryInterval):
		case <-ctx.Done():
			return fmt.Errorf("%w：%v（等待被取消；现场保留）", ErrMergeBlockedByMain, err)
		}
		skipApproval = true // 审批门对同一个 nodeID 只问一次
	}
}

// finishExclusive 是收尾段的实际实现：只由 finishActor 串行调用（同一时刻只有一个
// 收尾在改主工作区），因此这里不再自带互斥。skipApproval = 重试路径，跳过重复审批。
func (w *WorktreeManager) finishExclusive(ctx context.Context, nodeID string, wt *NodeWorktree, skipApproval bool) error {
	root := w.deps.Root()
	w.deps.Phase(ctx, nodeID, "rebasing")
	behind, err := w.branchBehindBase(wt)
	if err != nil {
		return err
	}
	if behind {
		// 落后就在**子代理自己的现场**里变基：主工作区一动不动，不需要人出手。
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
	if !skipApproval {
		summary, err := w.diffStat(wt)
		if err != nil {
			summary = "diff stat unavailable"
		}
		if err := w.approve(ctx, nodeID, wt, summary); err != nil {
			return err
		}
	}
	w.deps.Phase(ctx, nodeID, "merging")
	if out, mergeErr := w.git(root, "merge", "--no-edit", wt.Branch); mergeErr != nil {
		if isMergeBlockedEvidence(out, mergeErr) {
			// 合并**没开始就被主工作区挡下**：可重试态（重试由 Finish 的预算循环驱动；
			// 主工作区没被改成半成品，所以重试是安全的）。
			return &mergeBlockedError{nodeID: nodeID, path: wt.Path, detail: firstEvidenceLine(out, mergeErr)}
		}
		conflicts, _ := w.conflictFilesIn(root)
		return fmt.Errorf("worktree %q: merge %s into %s failed (resolve conflicts in the main workspace, or git merge --abort): %v\n%s\n冲突文件: %v", nodeID, wt.Branch, wt.MainBranch, mergeErr, out, conflicts)
	}
	return w.cleanup(root, wt)
}

// firstEvidenceLine 从 git 证据里取一行摘要（原文可能多行；诊断取首行）。
func firstEvidenceLine(stderr string, callErr error) string {
	for _, line := range strings.Split(stderr, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	if callErr != nil {
		return callErr.Error()
	}
	return "main workspace is not mergeable"
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
// 「现场」会造出幽灵条目——`Info` 会报一个不存在的路径，`team_close` 收口步 2 会对着
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

// sceneRegistered 报告某个 worktree 路径是否是本管理器**在册**的现场
// （可能正被某个节点使用，因此不是可以随手删掉的残留）。
//
// 比较走 `worktreePathEqual` 而不是逐字符相等：`git worktree list` 在 Windows 上输出的
// 分隔符（`/`）与盘符大小写可能与本地用 `filepath.Join` 拼出的路径（`\`）不同。逐字符比
// 会把一个**在册**的现场判成"不在册"——这正是"恢复出来的现场仍被 Prune 当孤儿删掉"的
// 第二个成因（第一个成因是现场根本没被登记，见 Adopt）。
func (w *WorktreeManager) sceneRegistered(path string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, wt := range w.worktrees {
		if worktreePathEqual(wt.Path, path) {
			return true
		}
	}
	return false
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
//     （"在册"按 `sceneRegistered` 规范化后比较：git 输出的分隔符/盘符大小写与本地拼出的
//     路径可能不同，逐字符比会把在册现场误判成孤儿。）
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
	for _, entry := range entries {
		if entry.path == root || !w.isManagedPath(root, entry.path) || w.sceneRegistered(entry.path) {
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

// CleanupWorktree 删除 worktree 及其分支（合并完成后或无可合并提交时）。
// 包级薄包装：worktree_test.go 直接调用；组件内部走 w.cleanup 以支持 fake git 注入。
//
// **释放幂等**（2026-10-03 缺陷 A）：现场可能已被**上游**先收走——worker 回合结束的
// 自动尾插（MergeWorkspace → WorktreeManager.Finish → cleanup）在"有提交已合并 / 无
// 提交且干净"两条成功路径上都会 `git worktree remove`，但**没有**配一次 Release，于是
// 注册表与账本里的绑定还在、磁盘上的目录已经没了。随即 `team_accept` 的释放步骤对
// 同一份绑定**再动手一次**，就会对着一个已不存在的路径跑 `git worktree remove --force`，
// git 报 `exit status 128 / '<path>' is not a working tree`，把工作项卡在 review、
// 下游里程碑闸门打不开。释放因此按"目录 / 分支 / git 登记任一已不存在 = 已释放"处理。
//
// 真正的问题**不掩盖**：目录还在、且仍在 `git worktree list` 里，而 `git worktree
// remove --force` 失败时，照旧把 git 的原文返回（调用方显式报错）。
func CleanupWorktree(root string, wt *NodeWorktree) error {
	if wt == nil {
		return nil
	}
	if _, err := os.Stat(wt.Path); err != nil {
		// 目录已不在：现场已（被上游或手工）回收；只清可能残留的分支名。
		deleteWorktreeBranch(root, wt.Branch)
		return nil
	}
	if _, err := GitRunner(root, "worktree", "remove", "--force", wt.Path); err != nil {
		if !gitWorktreeRegistered(root, wt.Path) {
			// 目录还在，但 git 已不认识这个 worktree（登记被 prune / 手工移过）：
			// 这也是"已释放过"，不是 remove 失败。
			deleteWorktreeBranch(root, wt.Branch)
			return nil
		}
		return err
	}
	deleteWorktreeBranch(root, wt.Branch)
	return nil
}

// deleteWorktreeBranch 删除本地分支，忽略"分支不存在"（释放幂等的另一半：分支可能
// 已被上游一并删掉）。
func deleteWorktreeBranch(root, branch string) {
	if strings.TrimSpace(branch) == "" {
		return
	}
	_, _ = GitRunner(root, "branch", "-D", branch)
}

// gitWorktreeRegistered 报告某个路径是否仍在 `git worktree list` 的清单里
// （登记还在 = 还没释放；已不在 = 已释放，CleanupWorktree 据此幂等）。
func gitWorktreeRegistered(root, path string) bool {
	out, err := GitRunner(root, "worktree", "list")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// 行格式 `<path>  <sha> [<branch>]`：路径与后面的列以**两个空格**分隔。
		if i := strings.Index(line, "  "); i > 0 {
			line = line[:i]
		}
		if worktreePathEqual(line, path) {
			return true
		}
	}
	return false
}

// worktreePathEqual 比较两个路径是否是同一个 worktree：git 在 Windows 上输出的分隔符
// 与盘符大小写可能与本地拼出的路径不同，统一分隔符与大小写后再比。
func worktreePathEqual(a, b string) bool {
	norm := func(p string) string {
		return strings.TrimRight(filepath.ToSlash(filepath.Clean(p)), "/")
	}
	return strings.EqualFold(norm(a), norm(b))
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
