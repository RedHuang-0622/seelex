package worktree

// worktree_scene_naming_test.go — 钉住「现场命名 / 在册比较」只有一份口径（③D）。
//
// 两处曾各拼一遍同一串字面量（`<repoBase>-seelex-<nodeID>`）：建现场走 `scenePath`，
// 认现场走 `isManagedPath`。拼法换一处、漏一处，现场立刻"没人认识"——不是编译错，
// 是运行时把它当孤儿清掉。比较同理：git 输出的分隔符是 `/`、盘符大小写也可能与本地
// `filepath.Join` 拼出的 `\` 不同，逐字符比会把**在册**的现场判成不在册。
//
// 本用例钉三件事：(1) 命名只有一份拼法，且拼出来的路径必被 `isManagedPath` 认下；
// (2) 同一份现场换写法（`\` / `/`、盘符大小写）时"在册判定"结论不变；(3) 残留清理与
// 认领这两条**会动现场**的路，对同一份换写法的现场同样给同一个结论。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSceneNamingHasSingleSpelling(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	mgr, _, _ := newTestWorktreeManager(root)

	path := mgr.scenePath(root, "impl")
	want := filepath.Join(filepath.Dir(root), sceneDirName(filepath.Base(root), "impl"))
	if path != want {
		t.Fatalf("scenePath = %q，want %q（命名只应有一份拼法）", path, want)
	}
	if !mgr.isManagedPath(root, path) {
		t.Fatalf("scenePath 拼出来的路径必须被 isManagedPath 认下：%q", path)
	}
	// 认法只认本仓库的现场：换个 repoBase 就不该认。
	if mgr.isManagedPath(root, filepath.Join(filepath.Dir(root), "other-repo-seelex-impl")) {
		t.Fatal("别的仓库名下的目录不该被当成本管理器的现场")
	}
}

func TestSceneRegistrationAgreesAcrossPathSpellings(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	mgr, fake, _ := newTestWorktreeManager(root)

	local := filepath.Join(base, "repo-seelex-live") // 本地拼法（`\`）
	adoptedLocal := mgr.scenePath(root, "adopted")
	for _, dir := range []string{local, adoptedLocal} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	spell := func(path string) string { return filepath.ToSlash(path) }                           // git 风格（`/`）
	upper := func(path string) string { s := spell(path); return strings.ToUpper(s[:1]) + s[1:] } // + 盘符大写

	// 注册表里按本地拼法在册；换任何写法都必须在册。
	mgr.register("live", &NodeWorktree{Path: local, Branch: "seelex/live"})
	for _, spelling := range []string{local, spell(local), upper(local)} {
		if !mgr.sceneRegistered(spelling) {
			t.Fatalf("同一份现场换个写法就不在册了：%q", spelling)
		}
	}

	// git 清单按它自己的风格给出（`/` + 盘符大写）：在册的 live 必须被放过；
	// 不在册但可认领的 adopted 应能被 Adopt 认到同一条登记。
	fake.reply["worktree list --porcelain"] = strings.Join([]string{
		"worktree " + upper(local), "branch refs/heads/seelex/live", "",
		"worktree " + upper(adoptedLocal), "branch refs/heads/seelex/adopted", "",
		"worktree " + spell(root), "branch refs/heads/main", "",
	}, "\n")
	fake.reply["status --porcelain"] = ""

	// 恢复/认领先于清理（Prune 的文档口径）：先认领 adopted，再跑残留清理。
	adopted := mgr.Adopt("adopted")
	if adopted == nil {
		t.Fatal("清单里以 git 风格给出的现场必须能被认领（重启后的恢复路径）")
	}
	if !worktreePathEqual(adopted.Path, adoptedLocal) {
		t.Fatalf("认领到的路径应是本管理器拼出来的现场路径：%q", adopted.Path)
	}
	if adopted.Branch != "seelex/adopted" {
		t.Fatalf("认领到的分支应取自 git 登记，得 %q", adopted.Branch)
	}

	// 认领之后两条现场都在册：残留清理必须一个都不碰（换写法也一样）。
	result, err := mgr.Prune()
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(result.Removed) != 0 {
		t.Fatalf("在册现场被当成孤儿回收了：%v", result.Removed)
	}
	if len(result.Kept) != 0 {
		t.Fatalf("在册现场压根不该进入处理流程（更不该进 Kept）：%v", result.Kept)
	}
}
