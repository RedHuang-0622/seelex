package seelebridge

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/Seele/workplan/sugar/approve"
)

// fakeGit 是可编程 git 执行器：按 args 前缀返回脚本化输出，并记录调用序列。
type fakeGit struct {
	mu    sync.Mutex
	calls []string
	reply map[string]string
	err   map[string]error
}

func newFakeGit() *fakeGit {
	return &fakeGit{
		reply: map[string]string{
			"rev-parse --abbrev-ref HEAD": "main",
			"rev-parse HEAD":              "abc123",
			"rev-list --count":            "1",
			"status --porcelain":          "",
			"diff --stat":                 "1 file changed",
		},
		err: map[string]error{},
	}
}

func (f *fakeGit) run(root string, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strings.Join(args, " ")
	f.calls = append(f.calls, key)
	if err, ok := f.err[key]; ok {
		return "", err
	}
	for prefix, out := range f.reply {
		if strings.HasPrefix(key, prefix) {
			return out, nil
		}
	}
	return "", nil
}

func (f *fakeGit) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func newTestWorktreeManager(root string) (*worktreeManager, *fakeGit, *approveGateStub) {
	gate := &approveGateStub{choice: "approve"}
	var phases []string
	mgr := newWorktreeManager(worktreeManagerDeps{
		Root: func() string { return root },
		Phase: func(_ context.Context, nodeID, status string) {
			phases = append(phases, nodeID+":"+status)
		},
		Gate: func() approve.ApprovalGate { return gate },
	})
	fake := newFakeGit()
	mgr.git = fake.run
	return mgr, fake, gate
}

func TestWorktreeManagerBeginDegradesOutsideGit(t *testing.T) {
	mgr, fake, _ := newTestWorktreeManager(filepath.Join(t.TempDir(), "plain"))
	fake.err["rev-parse --is-inside-work-tree"] = &gitTestErr{}
	if wt := mgr.Begin(NodeScope{NodeID: "x", Role: RoleSubAgent, BranchID: "x"}, "x"); wt != nil {
		t.Fatalf("non-git project must degrade to nil, got %+v", wt)
	}
	if _, ok := mgr.Info("x"); ok {
		t.Fatal("no registration expected after degrade")
	}
}

type gitTestErr struct{}

func (*gitTestErr) Error() string { return "not a git repository" }

func TestWorktreeManagerBeginRegistersAndInfo(t *testing.T) {
	root := t.TempDir()
	mgr, _, _ := newTestWorktreeManager(root)
	wt := mgr.Begin(NodeScope{NodeID: "impl", Role: RoleSubAgent, BranchID: "impl"}, "impl")
	if wt == nil {
		t.Fatal("worktree creation must succeed with fake git")
	}
	if wt.Branch != "seelex/impl" || wt.MainBranch != "main" || wt.BaseCommit != "abc123" {
		t.Fatalf("unexpected worktree fields: %+v", wt)
	}
	info, ok := mgr.Info("impl")
	if !ok || info.Path != wt.Path || info.Branch != wt.Branch || info.MainBranch != wt.MainBranch {
		t.Fatalf("Info = %+v, %v", info, ok)
	}
}

func TestWorktreeManagerFinishDirtyUncommittedPreservesScene(t *testing.T) {
	root := t.TempDir()
	mgr, fake, _ := newTestWorktreeManager(root)
	fake.reply["status --porcelain"] = " M produced.txt"
	fake.reply["rev-list --count abc123..HEAD"] = "0"
	fake.reply["rev-list --count"] = "0"
	wt := mgr.Begin(NodeScope{NodeID: "impl", Role: RoleSubAgent, BranchID: "impl"}, "impl")
	if wt == nil {
		t.Fatal("begin must succeed")
	}
	err := mgr.Finish(context.Background(), "impl", wt)
	if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("dirty worktree must error, got %v", err)
	}
	// 失败路径：注册保留（现场可查）。
	if _, ok := mgr.Info("impl"); !ok {
		t.Fatal("failed worktree must stay registered for scene inspection")
	}
}

func TestWorktreeManagerFinishCleanNoChangesCleansUp(t *testing.T) {
	root := t.TempDir()
	mgr, fake, _ := newTestWorktreeManager(root)
	fake.reply["rev-list --count"] = "0"
	wt := mgr.Begin(NodeScope{NodeID: "noop", Role: RoleSubAgent, BranchID: "noop"}, "noop")
	if wt == nil {
		t.Fatal("begin must succeed")
	}
	if err := mgr.Finish(context.Background(), "noop", wt); err != nil {
		t.Fatalf("clean no-commit worktree must clean without error: %v", err)
	}
	calls := fake.snapshot()
	if !containsCall(calls, "worktree remove --force "+wt.Path) {
		t.Fatalf("cleanup must remove worktree, calls=%v", calls)
	}
}

func TestWorktreeManagerFinishMergeApproved(t *testing.T) {
	root := t.TempDir()
	mgr, fake, gate := newTestWorktreeManager(root)
	wt := mgr.Begin(NodeScope{NodeID: "impl", Role: RoleSubAgent, BranchID: "impl"}, "impl")
	if wt == nil {
		t.Fatal("begin must succeed")
	}
	if err := mgr.Finish(context.Background(), "impl", wt); err != nil {
		t.Fatalf("finish with commit must merge and clean: %v", err)
	}
	calls := fake.snapshot()
	if !containsCall(calls, "merge --no-edit seelex/impl") {
		t.Fatalf("merge must run, calls=%v", calls)
	}
	if !containsCall(calls, "worktree remove --force "+wt.Path) {
		t.Fatalf("cleanup must run after merge, calls=%v", calls)
	}
	gate.mu.Lock()
	asked := len(gate.asked)
	gate.mu.Unlock()
	if asked != 1 {
		t.Fatalf("approval asked %d times, want 1", asked)
	}
}

func TestWorktreeManagerFinishMergeRejectedPreservesScene(t *testing.T) {
	root := t.TempDir()
	mgr, _, gate := newTestWorktreeManager(root)
	gate.choice = "reject"
	wt := mgr.Begin(NodeScope{NodeID: "impl", Role: RoleSubAgent, BranchID: "impl"}, "impl")
	if wt == nil {
		t.Fatal("begin must succeed")
	}
	err := mgr.Finish(context.Background(), "impl", wt)
	if err == nil || !strings.Contains(err.Error(), "merge rejected") {
		t.Fatalf("expected rejection error, got %v", err)
	}
	if _, ok := mgr.Info("impl"); !ok {
		t.Fatal("rejected worktree must stay registered for scene inspection")
	}
}

func TestWorktreeManagerReleaseRemovesRegistration(t *testing.T) {
	root := t.TempDir()
	mgr, _, _ := newTestWorktreeManager(root)
	mgr.Begin(NodeScope{NodeID: "impl", Role: RoleSubAgent, BranchID: "impl"}, "impl")
	if _, ok := mgr.Info("impl"); !ok {
		t.Fatal("expected registration before release")
	}
	mgr.Release("impl")
	if _, ok := mgr.Info("impl"); ok {
		t.Fatal("registration must be gone after release")
	}
}

func TestWorktreeManagerConcurrentRace(t *testing.T) {
	root := t.TempDir()
	mgr, _, _ := newTestWorktreeManager(root)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			wt := mgr.Begin(NodeScope{NodeID: id, Role: RoleSubAgent, BranchID: id}, id)
			if wt != nil {
				_, _ = mgr.Info(id)
			}
			mgr.Release(id)
		}("n" + strings.Repeat("x", 1) + string(rune('a'+i%26)))
	}
	wg.Wait()
}

func containsCall(calls []string, want string) bool {
	for _, call := range calls {
		if strings.Contains(call, want) {
			return true
		}
	}
	return false
}
