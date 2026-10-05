package worktree

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/workplan/sugar/approve"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// scriptedGit 是可编程 git 执行器，比 worktree_manager_test.go 的 fakeGit 多两样：
//   - **并发读数**（同时进入 merge 的子进程数）：收尾串行化的判据；
//   - **按次脚本**（mergeHook）：第几次 merge 返回什么，用来演"被主工作区挡住 →
//     等干净 → 合上"与"真冲突"两条路径。
type scriptedGit struct {
	mu         sync.Mutex
	calls      []string
	inMerge    int
	maxInMerge int
	mergeCalls int
	// mergeHook 按 key（含分支名）+ 第几次 merge 决定本次 merge 的结果。
	mergeHook func(key string, attempt int) (string, error)
}

func newScriptedGit() *scriptedGit { return &scriptedGit{} }

func (g *scriptedGit) run(_ string, args ...string) (string, error) {
	key := strings.Join(args, " ")
	g.mu.Lock()
	g.calls = append(g.calls, key)
	g.mu.Unlock()
	switch {
	case strings.HasPrefix(key, "rev-parse --abbrev-ref"):
		return "main", nil
	case strings.HasPrefix(key, "rev-parse --verify"):
		return "refs/heads/seelex/x", nil
	case strings.HasPrefix(key, "rev-parse HEAD"):
		return "abc123", nil
	case strings.HasPrefix(key, "rev-list --count"):
		return "1", nil
	case strings.HasPrefix(key, "status --porcelain"):
		return "", nil
	case strings.HasPrefix(key, "diff --stat"):
		return "1 file changed", nil
	case strings.HasPrefix(key, "diff --name-only"):
		return "", nil
	case strings.HasPrefix(key, "merge-base"):
		return "abc123", nil
	case strings.HasPrefix(key, "rebase"):
		return "", nil
	case strings.HasPrefix(key, "merge --no-edit"):
		g.mu.Lock()
		g.inMerge++
		if g.inMerge > g.maxInMerge {
			g.maxInMerge = g.inMerge
		}
		g.mergeCalls++
		attempt := g.mergeCalls
		hook := g.mergeHook
		g.mu.Unlock()
		out, err := "merged", error(nil)
		if hook != nil {
			out, err = hook(key, attempt)
		} else {
			// merge "占用主工作区"的窗口：没有串行化时两次收尾会在这里重叠。
			time.Sleep(40 * time.Millisecond)
		}
		g.mu.Lock()
		g.inMerge--
		g.mu.Unlock()
		return out, err
	case strings.HasPrefix(key, "worktree remove"), strings.HasPrefix(key, "branch -D"):
		return "", nil
	}
	return "", nil
}

func (g *scriptedGit) snapshot() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.calls...)
}

// phaseLog 收集收尾段报出的阶段名（前端"工作区现场/节点阶段"的读数）。
type phaseLog struct {
	mu     sync.Mutex
	phases []string
}

func (l *phaseLog) record(_ context.Context, nodeID, status string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.phases = append(l.phases, nodeID+":"+status)
}

func (l *phaseLog) has(status string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, phase := range l.phases {
		if strings.HasSuffix(phase, ":"+status) {
			return true
		}
	}
	return false
}

// newScriptedManager 组装一个注入脚本化 git 的管理器（预算压到几十毫秒，测试可
// 在秒级观察重试）。
func newScriptedManager(t *testing.T, git *scriptedGit) (*WorktreeManager, *phaseLog, *approveGateStub) {
	t.Helper()
	root := t.TempDir()
	phases := &phaseLog{}
	gate := &approveGateStub{choice: "approve"}
	manager := NewWorktreeManager(WorktreeManagerDeps{
		Root:  func() string { return root },
		Phase: phases.record,
		Gate:  func() approve.ApprovalGate { return gate },
	})
	manager.git = git.run
	manager.mergeRetryBudget = 60 * time.Millisecond
	manager.mergeRetryInterval = 2 * time.Millisecond
	t.Cleanup(manager.Close)
	return manager, phases, gate
}

// mainDirtyBlocked 是"主工作区未提交改动压在本次合并路径上"的 git 原文形状。
const mainDirtyBlocked = "error: Your local changes to the following files would be overwritten by merge:\n\tmain.go\nPlease commit your changes or stash them before you merge."

// TestFinishSerializesConcurrentMergesIntoMain（2026-10-05 核心红线）：fork 的一批
// 子代理是**并发收工**的，而收尾段（rebase → 判定 → 审批 → merge → cleanup）全都动
// 同一个主工作区的 .git。判据取"同时进入 merge 的子进程数"——必须恒为 1。
func TestFinishSerializesConcurrentMergesIntoMain(t *testing.T) {
	git := newScriptedGit()
	manager, _, _ := newScriptedManager(t, git)
	first := manager.BeginNamed("n1")
	second := manager.BeginNamed("n2")
	if first == nil || second == nil {
		t.Fatal("begin must succeed with scripted git")
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	errorsByNode := make(map[string]error, 2)
	var mu sync.Mutex
	for _, target := range []struct {
		nodeID string
		wt     *NodeWorktree
	}{{"n1", first}, {"n2", second}} {
		wg.Add(1)
		go func(nodeID string, wt *NodeWorktree) {
			defer wg.Done()
			<-start // 两条收尾同时起跑
			err := manager.Finish(context.Background(), nodeID, wt)
			mu.Lock()
			errorsByNode[nodeID] = err
			mu.Unlock()
		}(target.nodeID, target.wt)
	}
	close(start)
	wg.Wait()

	for nodeID, err := range errorsByNode {
		if err != nil {
			t.Fatalf("Finish(%s): %v", nodeID, err)
		}
	}
	if git.maxInMerge != 1 {
		t.Fatalf("同时进入 merge 的子进程数 = %d, want 1（收尾段的单写者被绕过了）", git.maxInMerge)
	}
	calls := git.snapshot()
	if !containsCall(calls, "merge --no-edit seelex/n1") || !containsCall(calls, "merge --no-edit seelex/n2") {
		t.Fatalf("两个现场都必须被合并：calls=%v", calls)
	}
}

// TestFinishWithoutActorOverlapsMerges 是上一条的**反证对照**：把单写者摘掉
// （finishActor = nil → 直接执行），同一脚本立刻看到两个 merge 重叠。这正是
// 2026-10-05 之前"一批子代理同时收工"的现场——红线用例必须能区分这两者。
func TestFinishWithoutActorOverlapsMerges(t *testing.T) {
	git := newScriptedGit()
	manager, _, _ := newScriptedManager(t, git)
	manager.finishActor = nil // 对照：无串行化
	first := manager.BeginNamed("n1")
	second := manager.BeginNamed("n2")
	if first == nil || second == nil {
		t.Fatal("begin must succeed with scripted git")
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, target := range []struct {
		nodeID string
		wt     *NodeWorktree
	}{{"n1", first}, {"n2", second}} {
		wg.Add(1)
		go func(nodeID string, wt *NodeWorktree) {
			defer wg.Done()
			<-start
			_ = manager.Finish(context.Background(), nodeID, wt)
		}(target.nodeID, target.wt)
	}
	close(start)
	wg.Wait()

	if git.maxInMerge != 2 {
		t.Fatalf("对照组的 maxInMerge = %d, want 2（无 actor 时两次 merge 必然重叠）", git.maxInMerge)
	}
}

// TestFinishRetriesWhileMainWorkspaceDirtyThenMerges："主工作区脏、正好压在本次合并
// 路径上"不是确定性失败——预算内的重试必须自己等到主工作区干净并把改动合上，而且
// 重试**不再重复询问审批门**（同一个 nodeID 只问一次）。
func TestFinishRetriesWhileMainWorkspaceDirtyThenMerges(t *testing.T) {
	git := newScriptedGit()
	git.mergeHook = func(_ string, attempt int) (string, error) {
		if attempt <= 2 {
			return mainDirtyBlocked, errors.New("exit status 1")
		}
		return "merged", nil
	}
	manager, phases, gate := newScriptedManager(t, git)
	wt := manager.BeginNamed("impl")
	if wt == nil {
		t.Fatal("begin must succeed with scripted git")
	}
	if err := manager.Finish(context.Background(), "impl", wt); err != nil {
		t.Fatalf("被主工作区挡住的合并必须在预算内自愈，got %v", err)
	}
	if git.mergeCalls != 3 {
		t.Fatalf("merge 尝试次数 = %d, want 3（2 次被挡 + 1 次成功）", git.mergeCalls)
	}
	if !phases.has(phaseAwaitingMerge) {
		t.Fatalf("阶段必须报出 %s（前端要能看见'在等主工作区'）", phaseAwaitingMerge)
	}
	if !containsCall(git.snapshot(), "worktree remove --force "+wt.Path) {
		t.Fatalf("合上之后必须清理现场：calls=%v", git.snapshot())
	}
	gate.mu.Lock()
	asked := len(gate.asked)
	gate.mu.Unlock()
	if asked != 1 {
		t.Fatalf("审批门被问 %d 次，want 1（重试不重复打扰）", asked)
	}
}

// TestFinishReportsMergeBlockedByMainWithoutFailingScene：预算耗尽仍被挡住 → 返回
// ErrMergeBlockedByMain（分类错误），**不是**确定性失败：现场保留、不清理，输出里
// 带上处置办法，节点侧据此降级为警告而不是把产出判死。
func TestFinishReportsMergeBlockedByMainWithoutFailingScene(t *testing.T) {
	git := newScriptedGit()
	git.mergeHook = func(string, int) (string, error) {
		return mainDirtyBlocked, errors.New("exit status 1")
	}
	manager, phases, _ := newScriptedManager(t, git)
	wt := manager.BeginNamed("impl")
	if wt == nil {
		t.Fatal("begin must succeed with scripted git")
	}
	err := manager.Finish(context.Background(), "impl", wt)
	if !IsMergeBlockedByMain(err) {
		t.Fatalf("err = %v, want IsMergeBlockedByMain", err)
	}
	if !strings.Contains(err.Error(), "先提交或暂存主工作区的在途改动") {
		t.Fatalf("错误必须带处置办法（谁看都能动手）：%v", err)
	}
	if _, ok := manager.Info("impl"); !ok {
		t.Fatal("被挡住的现场必须保留（可查、可人工重试）")
	}
	if containsCall(git.snapshot(), "worktree remove --force "+wt.Path) {
		t.Fatal("被挡住时不得清理现场")
	}
	if !phases.has(phaseAwaitingMerge) {
		t.Fatalf("阶段必须报出 %s", phaseAwaitingMerge)
	}
	if git.mergeCalls < 2 {
		t.Fatalf("预算内必须真的重试过：mergeCalls = %d", git.mergeCalls)
	}
}

// TestFinishMergeConflictIsDefinitive：真冲突（主工作区真的被改成了冲突态）**不是**
// 可重试态——必须只试一次就报错、保留现场，并指路 `git merge --abort`。把它误判成
// "主工作区挡路"会让重试对着 MERGE_HEAD 反复撞墙。
func TestFinishMergeConflictIsDefinitive(t *testing.T) {
	git := newScriptedGit()
	git.mergeHook = func(string, int) (string, error) {
		return "CONFLICT (content): Merge conflict in main.go\nAutomatic merge failed; fix conflicts and then commit the result.", errors.New("exit status 1")
	}
	manager, _, _ := newScriptedManager(t, git)
	wt := manager.BeginNamed("impl")
	if wt == nil {
		t.Fatal("begin must succeed with scripted git")
	}
	err := manager.Finish(context.Background(), "impl", wt)
	if err == nil {
		t.Fatal("真冲突必须报错")
	}
	if IsMergeBlockedByMain(err) {
		t.Fatalf("真冲突不得被判成可重试的'主工作区挡路'：%v", err)
	}
	if git.mergeCalls != 1 {
		t.Fatalf("真冲突不得重试：mergeCalls = %d, want 1", git.mergeCalls)
	}
	if !strings.Contains(err.Error(), "merge --abort") {
		t.Fatalf("冲突错误必须给主代理指路：%v", err)
	}
	if _, ok := manager.Info("impl"); !ok {
		t.Fatal("冲突现场必须保留")
	}
}

// TestFinishBlockedByMainReleasesActorForOtherNodes：一个节点在等主工作区干净时，
// 其他节点**照常收尾**——等待不占单写者（否则一个脏主工作区会把整批子代理拖住）。
func TestFinishBlockedByMainReleasesActorForOtherNodes(t *testing.T) {
	git := newScriptedGit()
	firstBlocked := make(chan struct{}, 1)
	git.mergeHook = func(key string, _ int) (string, error) {
		if strings.Contains(key, "seelex/n1") { // n1 永远被挡；n2 放行
			select {
			case firstBlocked <- struct{}{}:
			default:
			}
			return mainDirtyBlocked, errors.New("exit status 1")
		}
		return "merged", nil
	}
	manager, _, _ := newScriptedManager(t, git)
	manager.mergeRetryBudget = 30 * time.Second // n1 一直等，直到自己的 ctx 被取消
	manager.mergeRetryInterval = 5 * time.Millisecond
	stuck := manager.BeginNamed("n1")
	other := manager.BeginNamed("n2")
	if stuck == nil || other == nil {
		t.Fatal("begin must succeed with scripted git")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	blocked := make(chan error, 1)
	go func() { blocked <- manager.Finish(ctx, "n1", stuck) }()
	select {
	case <-firstBlocked:
	case <-time.After(2 * time.Second):
		t.Fatal("n1 没有被挡住（脚本未生效）")
	}

	done := make(chan error, 1)
	go func() { done <- manager.Finish(context.Background(), "n2", other) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("n2 必须不被 n1 的等待拖住：%v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("n2 被 n1 的等待挡住了（等待占用了单写者）")
	}

	cancel() // 等不来干净的主工作区时，取消必须立刻收口为分类错误
	select {
	case err := <-blocked:
		if !IsMergeBlockedByMain(err) {
			t.Fatalf("n1 = %v, want IsMergeBlockedByMain", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("n1 在被挡住的等待里没有对取消收口")
	}
}

// TestBeginNamedReusedByTeammateAndSubagent 钉住两条链的**现场命名同源**：teammate 的
// 每一个 Work Item（BeginNamed）与子代理节点（Begin）走同一实现、同一命名，因此收尾
// 串行化与现场语义对两者同时生效。
func TestBeginNamedReusedByTeammateAndSubagent(t *testing.T) {
	git := newScriptedGit()
	manager, _, _ := newScriptedManager(t, git)
	byScope := manager.Begin(model.NodeScope{NodeID: "wi-1", Role: model.RoleSubAgent, BranchID: "wi-1"}, "wi-1")
	byName := manager.BeginNamed("wi-2")
	if byScope == nil || byName == nil {
		t.Fatal("both entries must create a scene")
	}
	if byScope.Branch != "seelex/wi-1" || byName.Branch != "seelex/wi-2" {
		t.Fatalf("两条入口的命名必须同源：%q / %q", byScope.Branch, byName.Branch)
	}
}
