package worktree

// worktree_dirty_judgment_test.go — 钉住「脏判定」只有一份判据（③B）。
//
// 判据今天有三份拷贝：`w.pathDirty(path)`、`w.worktreeDirty(wt)`（函数体与前者逐字
// 相同）、以及编排面 `seelebridge/runtime_teamwork.go` 里的包级 `worktreeDirty(root)`
// ——三份都在跑同一句 `git status --porcelain` 再判非空。
//
// 判据重复的代价不在"多写了几行"，而在**修一处不够**：本仓库 `core.autocrlf=true`，
// 行尾差异会让"内容其实一样"的文件被判成已修改（事故形状见 docs/self_judgement.md：
// 收尾段这条判据报 62 个"已修改"文件，而 `git diff --ignore-cr-at-eol` 比完是空的、
// 文件内容哈希 主工作区 == worktree）。收成一份之后，CRLF 幻影脏的修复点只剩
// worktree_manager.go 里 `status --porcelain` 那一行。
//
// 本用例钉两件事：(1) 组件内部两条入口落在同一次 `status --porcelain` 上、给同一个
// 结论；(2) 包级入口 PathDirty 接的是真实 git，干净/脏两侧都对。

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirtyJudgmentIsSingleAcrossEntries(t *testing.T) {
	mgr, fake, _ := newTestWorktreeManager(t.TempDir())

	// 同一份现场：porcelain 非空 = 脏，两条入口给同一个结论。
	fake.reply["status --porcelain"] = " M a.txt\n"
	byPath, err := mgr.pathDirty("scene")
	if err != nil {
		t.Fatalf("pathDirty: %v", err)
	}
	byWorktree, err := mgr.worktreeDirty(&NodeWorktree{Path: "scene"})
	if err != nil {
		t.Fatalf("worktreeDirty: %v", err)
	}
	if !byPath || !byWorktree {
		t.Fatalf("porcelain 非空必须判为脏：pathDirty=%v worktreeDirty=%v", byPath, byWorktree)
	}

	// 同一份现场：porcelain 为空 = 干净。
	fake.reply["status --porcelain"] = ""
	if byPath, err = mgr.pathDirty("scene"); err != nil {
		t.Fatalf("pathDirty: %v", err)
	}
	if byWorktree, err = mgr.worktreeDirty(&NodeWorktree{Path: "scene"}); err != nil {
		t.Fatalf("worktreeDirty: %v", err)
	}
	if byPath || byWorktree {
		t.Fatalf("porcelain 为空必须判为干净：pathDirty=%v worktreeDirty=%v", byPath, byWorktree)
	}

	// 「一份判据」的读数：两次判定期间除 `status --porcelain` 之外不许有任何别的 git 调用
	// （判据换了实现、或又长出一份自己的读法，这里立刻红）。
	for _, call := range fake.snapshot() {
		if call != "status --porcelain" {
			t.Fatalf("脏判定只应落在一句 `status --porcelain` 上，多出调用：%q（全部：%v）", call, fake.snapshot())
		}
	}

	// 包级入口接真实 git：干净仓库 → 干净；动一个文件 → 脏。
	repo := reproGitRepo(t)
	clean, err := PathDirty(repo)
	if err != nil {
		t.Fatalf("PathDirty(%s): %v", repo, err)
	}
	if clean {
		t.Fatal("刚建好的仓库必须判为干净")
	}
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirty, err := PathDirty(repo)
	if err != nil {
		t.Fatalf("PathDirty(%s): %v", repo, err)
	}
	if !dirty {
		t.Fatal("改了一个文件之后必须判为脏")
	}
}
