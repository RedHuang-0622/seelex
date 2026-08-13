package seelebridge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/workplan/sugar/approve"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	seenode "github.com/RedHuang-0622/seelex/seelebridge/node"
	"github.com/RedHuang-0622/seelex/seelebridge/worktree"
)

// ── worktree 生命周期（切片 4，docs/2026-08-03-subagent-fork-architecture/plan.md §3）──

// setupGitRepo 构造临时 git 仓库（含一个初始提交），返回仓库根。
func setupGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, cmd := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@seelex.local"},
		{"config", "user.name", "seelex test"},
	} {
		if _, err := worktree.GitRunner(root, cmd...); err != nil {
			t.Fatalf("git %v: %v", cmd, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.GitRunner(root, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.GitRunner(root, "commit", "-m", "base"); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestWorktreeLifecycleCreateAndClean 验证：subagent 节点开 worktree →
// 执行（scripted completer）→ 无提交则清理（不 merge）→ 注册表清空。
func TestWorktreeLifecycleCreateAndClean(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	repo := setupGitRepo(t)
	if err := runtime.BindProjectRoot(repo); err != nil {
		t.Fatal(err)
	}
	scripted := newScriptedNodeCompleter("read-only result, no changes")
	injectScriptedCompleters(t, runtime, map[string]agent.Completer{"agent-1": scripted})

	planJSON := `{"entry":"do","nodes":{"do":{"input":"read base.txt","kind":"agent"}},"edges":{}}`
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", planJSON); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_run", `{}`); err != nil {
		t.Fatalf("plan_run: %v", err)
	}

	// 节点结束后注册表清空、无残留 worktree 目录。
	left := runtime.worktreeMgr.RegisteredCount()
	if left != 0 {
		t.Fatalf("worktree registry not cleaned: %d entries", left)
	}
	wtPath := filepath.Join(filepath.Dir(repo), filepath.Base(repo)+"-seelex-do")
	if _, err := os.Stat(wtPath); err == nil {
		t.Fatalf("worktree dir still exists: %s", wtPath)
	}
	// 主工作区无新提交（无改动不 merge）。
	out, err := worktree.GitRunner(repo, "log", "--oneline")
	if err != nil || strings.Contains(out, "seelex/") {
		t.Fatalf("main branch must stay clean, log=%q err=%v", out, err)
	}
}

// approveGateStub 是测试审批门（approve/reject 可配）。
type approveGateStub struct {
	mu     sync.Mutex
	choice string
	asked  []string
}

func (g *approveGateStub) Ask(_ context.Context, q approve.Question) (any, error) {
	g.mu.Lock()
	g.asked = append(g.asked, q.ID)
	choice := g.choice
	g.mu.Unlock()
	return choice, nil
}

// TestWorktreeMergeApproved 验证有提交的节点：合并审批 approve →
// merge 回主工作区 → worktree 清理。
func TestWorktreeMergeApproved(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	repo := setupGitRepo(t)
	if err := runtime.BindProjectRoot(repo); err != nil {
		t.Fatal(err)
	}
	gate := &approveGateStub{choice: "approve"}
	runtime.SetPlanApprovalGate(gate)

	// 手动驱动生命周期（避开 scripted completer 不写文件的限制）：
	scope := seenode.NodeScope{NodeID: "impl", Role: model.RoleSubAgent, BranchID: "impl"}
	wt := runtime.beginNodeWorktree(scope, "impl")
	if wt == nil {
		t.Fatal("worktree creation must succeed in a git repo")
	}
	// 模拟子代理在 worktree 里干活并 commit。
	change := filepath.Join(wt.Path, "feature.txt")
	if err := os.WriteFile(change, []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.GitRunner(wt.Path, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.GitRunner(wt.Path, "commit", "-m", "seelex/impl: add feature"); err != nil {
		t.Fatal(err)
	}

	if err := runtime.finishNodeWorktree(context.Background(), "impl", wt); err != nil {
		t.Fatalf("finish: %v", err)
	}
	// merge 后主工作区可见改动（Windows git 可能做 CRLF 转换，容忍行尾）；worktree 已清理。
	merged, err := os.ReadFile(filepath.Join(repo, "feature.txt"))
	if err != nil || strings.TrimSpace(string(merged)) != "feature" {
		t.Fatalf("merged file missing or wrong: %q err=%v", merged, err)
	}
	if _, err := os.Stat(wt.Path); err == nil {
		t.Fatalf("worktree not cleaned after merge: %s", wt.Path)
	}
	gate.mu.Lock()
	asked := len(gate.asked)
	gate.mu.Unlock()
	if asked != 1 {
		t.Fatalf("approval asked %d times, want 1", asked)
	}
}

// TestWorktreeMergeRejected 验证审批拒绝：节点报错且 worktree 保留现场。
func TestWorktreeMergeRejected(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	repo := setupGitRepo(t)
	if err := runtime.BindProjectRoot(repo); err != nil {
		t.Fatal(err)
	}
	gate := &approveGateStub{choice: "reject"}
	runtime.SetPlanApprovalGate(gate)

	scope := seenode.NodeScope{NodeID: "impl", Role: model.RoleSubAgent, BranchID: "impl"}
	wt := runtime.beginNodeWorktree(scope, "impl")
	if wt == nil {
		t.Fatal("worktree creation must succeed")
	}
	if err := os.WriteFile(filepath.Join(wt.Path, "feature.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.GitRunner(wt.Path, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.GitRunner(wt.Path, "commit", "-m", "seelex/impl: work"); err != nil {
		t.Fatal(err)
	}

	err := runtime.finishNodeWorktree(context.Background(), "impl", wt)
	if err == nil || !strings.Contains(err.Error(), "merge rejected") {
		t.Fatalf("expected merge rejection error, got %v", err)
	}
	// 现场保留：worktree 目录仍在，主工作区无改动。
	if _, statErr := os.Stat(wt.Path); statErr != nil {
		t.Fatalf("worktree must be preserved on rejection: %v", statErr)
	}
	if _, readErr := os.Stat(filepath.Join(repo, "feature.txt")); readErr == nil {
		t.Fatal("main workspace must not contain rejected changes")
	}
}

// TestWorktreeDirtyUncommittedPreserved 验证设计 §风险表的承诺：子代理在
// worktree 里写了文件但没 commit（无提交 + 工作区脏）→ finish 报错、现场
// 保留——绝不静默删除产出（P0 修复，2026-08-07）。
func TestWorktreeDirtyUncommittedPreserved(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	repo := setupGitRepo(t)
	if err := runtime.BindProjectRoot(repo); err != nil {
		t.Fatal(err)
	}
	scope := seenode.NodeScope{NodeID: "impl", Role: model.RoleSubAgent, BranchID: "impl"}
	wt := runtime.beginNodeWorktree(scope, "impl")
	if wt == nil {
		t.Fatal("worktree creation must succeed")
	}
	// 模拟子代理写了文件但没 commit（未跟踪 + 未暂存）。
	if err := os.WriteFile(filepath.Join(wt.Path, "produced.txt"), []byte("output\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runtime.finishNodeWorktree(context.Background(), "impl", wt)
	if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("dirty worktree must error, got %v", err)
	}
	// 现场保留：worktree 目录与产出文件都在，主工作区无改动。
	if _, statErr := os.Stat(wt.Path); statErr != nil {
		t.Fatalf("dirty worktree must be preserved: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(wt.Path, "produced.txt")); statErr != nil {
		t.Fatalf("produced file must be preserved in worktree: %v", statErr)
	}
	if _, readErr := os.Stat(filepath.Join(repo, "produced.txt")); readErr == nil {
		t.Fatal("main workspace must not contain uncommitted worktree changes")
	}
	// 清理现场（测试收尾）。
	if err := worktree.CleanupWorktree(repo, wt); err != nil {
		t.Fatalf("test cleanup: %v", err)
	}
}

// TestWorktreeCleanWithNoChanges 无提交且工作区干净 → 仍走原清理路径。
func TestWorktreeCleanWithNoChanges(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	repo := setupGitRepo(t)
	if err := runtime.BindProjectRoot(repo); err != nil {
		t.Fatal(err)
	}
	scope := seenode.NodeScope{NodeID: "noop", Role: model.RoleSubAgent, BranchID: "noop"}
	wt := runtime.beginNodeWorktree(scope, "noop")
	if wt == nil {
		t.Fatal("worktree creation must succeed")
	}
	if err := runtime.finishNodeWorktree(context.Background(), "noop", wt); err != nil {
		t.Fatalf("clean no-commit worktree must clean up without error: %v", err)
	}
	if _, err := os.Stat(wt.Path); err == nil {
		t.Fatalf("clean worktree must be removed: %s", wt.Path)
	}
}

// TestWorktreeConflictFilesListsUnmerged 冲突后能列出冲突文件（诊断面）。
func TestWorktreeConflictFilesListsUnmerged(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	repo := setupGitRepo(t)
	if err := runtime.BindProjectRoot(repo); err != nil {
		t.Fatal(err)
	}
	scope := seenode.NodeScope{NodeID: "impl", Role: model.RoleSubAgent, BranchID: "impl"}
	wt := runtime.beginNodeWorktree(scope, "impl")
	if wt == nil {
		t.Fatal("worktree creation must succeed")
	}
	// 制造一个未合并的冲突（手动 merge 触发冲突状态）。
	if err := os.WriteFile(filepath.Join(wt.Path, "conflict.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.GitRunner(wt.Path, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.GitRunner(wt.Path, "commit", "-m", "base conflict file"); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.GitRunner(wt.Path, "merge", "main", "--no-edit"); err != nil {
		// merge 失败（冲突）→ diff-filter=U 应列出文件。
		files, err := worktree.ConflictFilesIn(wt.Path)
		if err != nil {
			t.Fatalf("conflict files: %v", err)
		}
		if len(files) != 1 || files[0] != "conflict.txt" {
			t.Fatalf("conflict files = %v, want [conflict.txt]", files)
		}
	}
}

// TestWorktreeDegradeOutsideGit 验证非 git 仓库降级共享工作区。
func TestWorktreeDegradeOutsideGit(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	plain := t.TempDir()
	if err := runtime.BindProjectRoot(plain); err != nil {
		t.Fatal(err)
	}
	scope := seenode.NodeScope{NodeID: "x", Role: model.RoleSubAgent, BranchID: "x"}
	if wt := runtime.beginNodeWorktree(scope, "x"); wt != nil {
		t.Fatalf("non-git project must degrade to shared workspace, got worktree %+v", wt)
	}
}
