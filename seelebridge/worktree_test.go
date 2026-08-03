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
		if _, err := gitRunner(root, cmd...); err != nil {
			t.Fatalf("git %v: %v", cmd, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitRunner(root, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := gitRunner(root, "commit", "-m", "base"); err != nil {
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
	runtime.wt.mu.Lock()
	left := len(runtime.wt.worktrees)
	runtime.wt.mu.Unlock()
	if left != 0 {
		t.Fatalf("worktree registry not cleaned: %d entries", left)
	}
	wtPath := filepath.Join(filepath.Dir(repo), filepath.Base(repo)+"-seelex-do")
	if _, err := os.Stat(wtPath); err == nil {
		t.Fatalf("worktree dir still exists: %s", wtPath)
	}
	// 主工作区无新提交（无改动不 merge）。
	out, err := gitRunner(repo, "log", "--oneline")
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
	scope := NodeScope{NodeID: "impl", Role: RoleSubAgent, BranchID: "impl"}
	wt := runtime.beginNodeWorktree(scope, "impl")
	if wt == nil {
		t.Fatal("worktree creation must succeed in a git repo")
	}
	// 模拟子代理在 worktree 里干活并 commit。
	change := filepath.Join(wt.Path, "feature.txt")
	if err := os.WriteFile(change, []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitRunner(wt.Path, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := gitRunner(wt.Path, "commit", "-m", "seelex/impl: add feature"); err != nil {
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

	scope := NodeScope{NodeID: "impl", Role: RoleSubAgent, BranchID: "impl"}
	wt := runtime.beginNodeWorktree(scope, "impl")
	if wt == nil {
		t.Fatal("worktree creation must succeed")
	}
	if err := os.WriteFile(filepath.Join(wt.Path, "feature.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitRunner(wt.Path, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := gitRunner(wt.Path, "commit", "-m", "seelex/impl: work"); err != nil {
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

// TestWorktreeDegradeOutsideGit 验证非 git 仓库降级共享工作区。
func TestWorktreeDegradeOutsideGit(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	plain := t.TempDir()
	if err := runtime.BindProjectRoot(plain); err != nil {
		t.Fatal(err)
	}
	scope := NodeScope{NodeID: "x", Role: RoleSubAgent, BranchID: "x"}
	if wt := runtime.beginNodeWorktree(scope, "x"); wt != nil {
		t.Fatalf("non-git project must degrade to shared workspace, got worktree %+v", wt)
	}
}

// TestResolveNodePathUsesWorktreeRoot 验证 worktree 节点的工具路径以
// worktree 为根解析（越界拒绝）。
func TestResolveNodePathUsesWorktreeRoot(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	repo := setupGitRepo(t)
	if err := runtime.BindProjectRoot(repo); err != nil {
		t.Fatal(err)
	}
	scope := NodeScope{NodeID: "x", Role: RoleSubAgent, BranchID: "x"}
	wt := runtime.beginNodeWorktree(scope, "x")
	if wt == nil {
		t.Fatal("worktree creation must succeed")
	}
	defer func() { _ = cleanupWorktree(repo, wt) }()

	ctx := WithNodeScope(context.Background(), NodeScope{
		NodeID: "x", Role: RoleSubAgent, BranchID: "x", WorkspaceID: wt.Path,
	})
	resolved, err := runtime.resolveNodePath(ctx, "base.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(wt.Path, "base.txt")
	if resolved != want {
		t.Fatalf("resolved = %s, want %s", resolved, want)
	}
	// 越界路径拒绝（.. 逃逸）。
	if _, err := runtime.resolveNodePath(ctx, "../outside.txt", false); err == nil {
		t.Fatal("escape path must be rejected")
	}
}
