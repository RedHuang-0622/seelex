package worktree

import (
	"context"
	"errors"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/backend/workunit"
	"github.com/RedHuang-0622/seelex/internal/model"
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
func newScriptedManager(t *testing.T, git *scriptedGit) (*WorktreeManager, *phaseLog) {
	t.Helper()
	root := t.TempDir()
	phases := &phaseLog{}
	manager := NewWorktreeManager(WorktreeManagerDeps{
		Root:  func() string { return root },
		Phase: phases.record,
	})
	manager.git = git.run
	t.Cleanup(manager.Close)
	return manager, phases
}

// mainDirtyBlocked 是"主工作区未提交改动压在本次合并路径上"的 git 原文形状。
const mainDirtyBlocked = "error: Your local changes to the following files would be overwritten by merge:\n\tmain.go\nPlease commit your changes or stash them before you merge."

// TestFinishSerializesConcurrentMergesIntoMain（2026-10-05 核心红线）：fork 的一批
// 子代理是**并发收工**的，而收尾段（rebase → 判定 → merge → cleanup）全都动
// 同一个主工作区的 .git。判据取"同时进入 merge 的子进程数"——必须恒为 1。
func TestFinishSerializesConcurrentMergesIntoMain(t *testing.T) {
	git := newScriptedGit()
	manager, _ := newScriptedManager(t, git)
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
	manager, _ := newScriptedManager(t, git)
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

// scenePortForTest 把真实的 WorktreeManager.Finish 接成生命周期实现的现场端口：
// 这样"同一份脚本化 git 输入"可以同时经过现场适配器（单次事实）与 Host（重试判据）。
type scenePortForTest struct{ mgr *WorktreeManager }

func (scenePortForTest) BeginScene(context.Context, workunit.Ownership, string, string) (workunit.Scene, error) {
	return workunit.Scene{}, nil
}

func (p scenePortForTest) MergeScene(ctx context.Context, _ workunit.Ownership, nodeID, _ string) error {
	return p.mgr.Finish(ctx, nodeID, p.mgr.WorktreeForNode(nodeID))
}

func (p scenePortForTest) ReleaseScene(nodeID string) { p.mgr.Release(nodeID) }

func (scenePortForTest) AdoptScenes(context.Context, string) error { return nil }

func (p scenePortForTest) SceneRegistered(nodeID string) bool {
	return p.mgr.WorktreeForNode(nodeID) != nil
}

// zeroUnits 是"没有团队归属"的编排端口：本文件的用例只走 subagent 那一侧。
type zeroUnits struct{}

func (zeroUnits) Owned(context.Context, string, workunit.Ownership) bool { return false }

func (zeroUnits) Settled(context.Context, string, string) (bool, error) { return false, nil }

func (zeroUnits) SettleUnit(context.Context, string, workunit.Ownership, error, error) (workunit.Outcome, error) {
	return workunit.Outcome{}, nil
}

func (zeroUnits) ReclaimUnit(context.Context, string, string) error { return nil }

func (zeroUnits) RecoverUnits(context.Context, string) (workunit.Resume, error) {
	return workunit.Resume{}, nil
}

// zeroRecords 是"没有会话记录"的账本；Clear 由 Immediate 策略在落定后调用。
type zeroRecords struct{}

func (zeroRecords) Status(context.Context, workunit.Ownership, string, string) (string, bool, error) {
	return "", false, nil
}

func (zeroRecords) Readout(context.Context, workunit.Ownership, string, string) (workunit.Resume, error) {
	return workunit.Resume{}, nil
}

func (zeroRecords) SaveRecord(context.Context, workunit.Ownership, string, string, string, string, string) {
}

func (zeroRecords) SettleRecord(context.Context, workunit.Ownership, string, string, workunit.Outcome) {
}

func (zeroRecords) Clear(context.Context, workunit.Ownership, string, string) error { return nil }

type sceneUnit struct{ id string }

func (sceneUnit) Kind() workunit.Kind { return workunit.KindSubagent }

func (u sceneUnit) ID() string { return u.id }

func (sceneUnit) SessionPath() string { return "sess-worktree" }

func (sceneUnit) Policy() workunit.FinishPolicy { return workunit.Immediate{} }

func (sceneUnit) Owns() workunit.Ownership { return workunit.Ownership{} }

// TestHostRetriesWorktreeBlockedMergeWithinBudget："主工作区脏、正好压在本次合并
// 路径上"不是确定性失败——现场适配器只交回一次挡路事实，Host 在预算内等到主工作区
// 干净并把改动合上；阶段 awaiting_merge 也由 Host 报出。
func TestHostRetriesWorktreeBlockedMergeWithinBudget(t *testing.T) {
	git := newScriptedGit()
	git.mergeHook = func(_ string, attempt int) (string, error) {
		if attempt <= 2 {
			return mainDirtyBlocked, errors.New("exit status 1")
		}
		return "merged", nil
	}
	manager, _ := newScriptedManager(t, git)
	wt := manager.BeginNamed("impl")
	if wt == nil {
		t.Fatal("begin must succeed with scripted git")
	}
	var phases phaseLog
	host := workunit.NewHost(
		scenePortForTest{mgr: manager}, zeroUnits{}, zeroRecords{},
		workunit.WithMergeRetry(workunit.MergeRetry{
			Budget:   time.Second,
			Interval: time.Millisecond,
			Observe:  phases.record,
		}),
	)
	outcome, err := host.Finish(context.Background(), sceneUnit{id: "impl"}, workunit.Result{}, nil)
	if err != nil {
		t.Fatalf("被主工作区挡住的合并必须在预算内自愈，got %v", err)
	}
	if outcome.Kind != workunit.OutcomeSettled {
		t.Fatalf("outcome = %+v, want settled", outcome)
	}
	if git.mergeCalls != 3 {
		t.Fatalf("merge 尝试次数 = %d, want 3（2 次被挡 + 1 次成功）", git.mergeCalls)
	}
	if !phases.has(workunit.MergePhaseAwaiting) {
		t.Fatalf("阶段必须报出 %s（前端要能看见'在等主工作区'）", workunit.MergePhaseAwaiting)
	}
	if !containsCall(git.snapshot(), "worktree remove --force "+wt.Path) {
		t.Fatalf("合上之后必须清理现场：calls=%v", git.snapshot())
	}
	if _, ok := manager.Info("impl"); ok {
		t.Fatal("落定后 Immediate 策略应把注册表也清掉")
	}
}

// TestFinishReportsBlockedByMainAsOneAttempt：现场适配器只跑一次 merge，挡路以
// ErrMergeBlockedByMain 交回（等待/重试不在这里），现场保留、不清理。
func TestFinishReportsBlockedByMainAsOneAttempt(t *testing.T) {
	git := newScriptedGit()
	git.mergeHook = func(string, int) (string, error) {
		return mainDirtyBlocked, errors.New("exit status 1")
	}
	manager, _ := newScriptedManager(t, git)
	wt := manager.BeginNamed("impl")
	if wt == nil {
		t.Fatal("begin must succeed with scripted git")
	}
	err := manager.Finish(context.Background(), "impl", wt)
	if !IsMergeBlockedByMain(err) {
		t.Fatalf("err = %v, want IsMergeBlockedByMain", err)
	}
	if _, ok := manager.Info("impl"); !ok {
		t.Fatal("被挡住的现场必须保留（可查、可人工重试）")
	}
	if containsCall(git.snapshot(), "worktree remove --force "+wt.Path) {
		t.Fatal("被挡住时不得清理现场")
	}
	if git.mergeCalls != 1 {
		t.Fatalf("现场适配器只许试一次：mergeCalls = %d, want 1", git.mergeCalls)
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
	manager, _ := newScriptedManager(t, git)
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

// TestFinishBlockedByMainReleasesActorForOtherNodes：挡路事实交回后单写者立刻释放，
// 其他节点照常收尾——等待不再发生在现场适配器里，因此不会把整批子代理拖住。
func TestFinishBlockedByMainReleasesActorForOtherNodes(t *testing.T) {
	git := newScriptedGit()
	git.mergeHook = func(key string, _ int) (string, error) {
		if strings.Contains(key, "seelex/n1") { // n1 永远被挡；n2 放行
			return mainDirtyBlocked, errors.New("exit status 1")
		}
		return "merged", nil
	}
	manager, _ := newScriptedManager(t, git)
	stuck := manager.BeginNamed("n1")
	other := manager.BeginNamed("n2")
	if stuck == nil || other == nil {
		t.Fatal("begin must succeed with scripted git")
	}

	blocked := make(chan error, 1)
	go func() { blocked <- manager.Finish(context.Background(), "n1", stuck) }()
	select {
	case err := <-blocked:
		if !IsMergeBlockedByMain(err) {
			t.Fatalf("n1 = %v, want IsMergeBlockedByMain", err)
		}
	case <-time.After(time.Second):
		t.Fatal("挡路事实必须立刻交回（现场适配器不再等待）")
	}

	done := make(chan error, 1)
	go func() { done <- manager.Finish(context.Background(), "n2", other) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("n2 必须不被 n1 的等待拖住：%v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("n2 被挡住了（单写者没有在挡路事实交回后释放）")
	}
}

// TestBeginNamedReusedByTeammateAndSubagent 钉住两条链的**现场命名同源**：teammate 的
// 每一个 Work Item（BeginNamed）与子代理节点（Begin）走同一实现、同一命名，因此收尾
// 串行化与现场语义对两者同时生效。
func TestBeginNamedReusedByTeammateAndSubagent(t *testing.T) {
	git := newScriptedGit()
	manager, _ := newScriptedManager(t, git)
	byScope := manager.Begin(dto.SceneScope{SceneID: "wi-1", Role: model.RoleSubAgent, BranchID: "wi-1"}, "wi-1")
	byName := manager.BeginNamed("wi-2")
	if byScope == nil || byName == nil {
		t.Fatal("both entries must create a scene")
	}
	if byScope.Branch != "seelex/wi-1" || byName.Branch != "seelex/wi-2" {
		t.Fatalf("两条入口的命名必须同源：%q / %q", byScope.Branch, byName.Branch)
	}
}
