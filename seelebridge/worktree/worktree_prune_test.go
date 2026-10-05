package worktree

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// TestPruneRemovesOrphanWorktreesAndKeepsDirtyOnes 钉住「残留兜底清理器」的两条判据：
// 不在册 **且** 干净的孤儿被回收；在册的一律不动；有未提交改动的一律保留
// （现场的未提交产出是人的资产，框架不替人做「丢还是留」）。
func TestPruneRemovesOrphanWorktreesAndKeepsDirtyOnes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	orphanClean := filepath.Join(filepath.Dir(root), "repo-seelex-orphan-clean")
	orphanDirty := filepath.Join(filepath.Dir(root), "repo-seelex-orphan-dirty")
	live := filepath.Join(filepath.Dir(root), "repo-seelex-live")

	mgr, _, _ := newTestWorktreeManager(root)
	var mu sync.Mutex
	var removed []string
	mgr.git = func(dir string, args ...string) (string, error) {
		key := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(key, "worktree list"):
			return strings.Join([]string{
				"worktree " + orphanClean, "branch refs/heads/seelex/orphan-clean", "",
				"worktree " + orphanDirty, "branch refs/heads/seelex/orphan-dirty", "",
				"worktree " + live, "branch refs/heads/seelex/live", "",
				"worktree " + root, "branch refs/heads/main", "",
			}, "\n"), nil
		case strings.HasPrefix(key, "status --porcelain"):
			if dir == orphanDirty {
				return " M scratch.txt\n", nil
			}
			return "", nil
		case strings.HasPrefix(key, "worktree remove"):
			mu.Lock()
			removed = append(removed, args[len(args)-1])
			mu.Unlock()
			return "", nil
		}
		return "", nil
	}
	// 在册现场：Prune 必须放过它。
	mgr.worktrees["live"] = &NodeWorktree{Path: live, Branch: "seelex/live"}

	result, err := mgr.Prune()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != orphanClean {
		t.Fatalf("Removed = %v, want 仅干净孤儿 %q", result.Removed, orphanClean)
	}
	if len(result.Kept) != 1 || result.Kept[0] != orphanDirty {
		t.Fatalf("Kept = %v, want 仅有未提交改动的 %q", result.Kept, orphanDirty)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, path := range removed {
		if path == live {
			t.Fatalf("在册现场被回收了：%v", removed)
		}
	}
}

// TestPruneRunsGitWorktreePrune 钉住「顺手清掉 prunable 元数据」：手工删目录会在
// `.git/worktrees` 下留下永远没人清的条目。
func TestPruneRunsGitWorktreePrune(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	mgr, _, _ := newTestWorktreeManager(root)
	var calls []string
	var mu sync.Mutex
	mgr.git = func(dir string, args ...string) (string, error) {
		mu.Lock()
		calls = append(calls, strings.Join(args, " "))
		mu.Unlock()
		return "", nil
	}
	if _, err := mgr.Prune(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, call := range calls {
		if call == "worktree prune" {
			return
		}
	}
	t.Fatalf("Prune 未清理 prunable 元数据：%v", calls)
}

// TestRestoreSkipsWorktreesThatNoLongerExist 钉住「幽灵条目」修复：锚点还在、目录
// 已经不在的 worktree 不得重新登记——否则 Info 会报一个不存在的路径，
// team_close 收口步 2 会对着它跑 git status 而失败。
func TestRestoreSkipsWorktreesThatNoLongerExist(t *testing.T) {
	root := t.TempDir()
	mgr, _, _ := newTestWorktreeManager(root)

	alive := t.TempDir()
	missing := filepath.Join(t.TempDir(), "gone")
	if _, err := os.Stat(missing); err == nil {
		t.Fatalf("前置条件失败：%q 竟然存在", missing)
	}

	mgr.Restore([]sessionstore.NodeSessionRecord{
		{NodeID: "alive", Worktree: sessionstore.NodeWorktreeRecord{Path: alive, Branch: "seelex/alive"}},
		{NodeID: "ghost", Worktree: sessionstore.NodeWorktreeRecord{Path: missing, Branch: "seelex/ghost"}},
	})

	if _, ok := mgr.Info("alive"); !ok {
		t.Fatal("仍存在的现场必须恢复登记")
	}
	if info, ok := mgr.Info("ghost"); ok {
		t.Fatalf("已不存在的目录不得登记成幽灵现场：%+v", info)
	}
	_ = time.Now
}
