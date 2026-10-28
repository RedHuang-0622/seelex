package store

// project_known_test.go — 项目摘要渲染件（M5，T5.3–T5.6 + 落点随存储根变）。
//
// 口径：渲染件是 `storeproject.ProjectRecord` 的**人读渲染（读侧视图）**，所以这里既查"渲染得
// 确定"（两次渲染字节相同、剔除 BuiltAt / 绝对路径 / ws-<nano>），也查"落得被动"
// （无显式调用不产文件、空键空内容显式报错），还查"链不重复"（判定仍只有
// storeproject.RefreshProjectKnowledge 三分支，渲染不写事实）。

import (
	"context"
	"errors"
	storeproject "github.com/RedHuang-0622/seelex/store/project"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// projectKnownAbsPath 造一个"带用户名 + 盘符"的绝对路径（本机 tempdir 之外也可复现）。
func projectKnownAbsPath(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// projectKnownRecord 造一条"含所有脏字段"的记录：未排序模块 + 绝对 Path/Docs +
// 抖动 BuiltAt + 带 ws-<nano> 的摘要。返回值第二个是那条绝对路径（供断言用）。
func projectKnownRecord(t *testing.T, builtAt time.Time) (storeproject.ProjectRecord, string) {
	t.Helper()
	abs := projectKnownAbsPath(t)
	record := storeproject.ProjectRecord{
		Version: "versionhash01",
		Modules: []storeproject.ModuleSemantics{
			{
				Name:    "zeta",
				Summary: "zeta 职责（会话 ws-1700000000000000000 的说明）",
				Path:    filepath.Join(abs, "zeta"),
				Docs:    []string{filepath.Join(abs, "docs", "zeta.md"), "docs/zeta.md"},
			},
			{
				Name:    "alpha",
				Summary: "alpha 职责",
				Path:    "application/alpha",
				Docs:    []string{"docs/alpha.md", "docs/alpha.md"},
			},
			{
				Name:    "beta",
				Summary: "beta 职责，路径 " + filepath.Join(abs, "beta") + " 也在",
				Path:    filepath.Join(abs, "beta"),
			},
		},
		SourceHashes: []string{"h1", "h2"},
		BuiltAt:      builtAt,
	}
	return record, abs
}

// TestProjectKnownRenderDeterministic 对应 T5.3：同一 record 两次渲染字节相同；
// 不含 BuiltAt 时间戳 / 绝对路径 / ws-<nano>；模块按 Name 稳定排序。
func TestProjectKnownRenderDeterministic(t *testing.T) {
	record, abs := projectKnownRecord(t, time.Unix(1700000000, 0).UTC())
	first := storeproject.ProjectKnownMarkdown(record)
	second := storeproject.ProjectKnownMarkdown(record)
	if string(first) != string(second) {
		t.Fatalf("同一 record 两次渲染不同：\n%s\n---\n%s", first, second)
	}

	text := string(first)
	// ① 不含抖动时间戳：BuiltAt 的任何形态都不得出现。
	for _, stamp := range []string{
		record.BuiltAt.Format(time.RFC3339),
		record.BuiltAt.Format(time.RFC3339Nano),
		record.BuiltAt.Format("2006-01-02"),
		"2023-11-14",
	} {
		if strings.Contains(text, stamp) {
			t.Fatalf("渲染件含 BuiltAt 时间戳 %q：\n%s", stamp, text)
		}
	}
	// ② 不含绝对路径（Path / Docs 的绝对路径丢弃；prose 里的盘符路径也清洗）。
	if strings.Contains(text, abs) {
		t.Fatalf("渲染件含绝对路径 %q：\n%s", abs, text)
	}
	if !strings.Contains(text, "<abs-path>") {
		t.Fatalf("prose 里的绝对路径没有被清洗成 <abs-path>：\n%s", text)
	}
	// ③ 不含 ws-<nano>（可提交件三铁律）。
	if strings.Contains(text, "ws-1700000000000000000") {
		t.Fatalf("渲染件含 ws-<nano>：\n%s", text)
	}
	// ④ 模块按 Name 稳定排序，且相对路径 / 去重后的文档都在。
	alpha, beta, zeta := strings.Index(text, "## alpha"), strings.Index(text, "## beta"), strings.Index(text, "## zeta")
	if alpha < 0 || beta < 0 || zeta < 0 || !(alpha < beta && beta < zeta) {
		t.Fatalf("模块未按 Name 稳定排序（alpha=%d beta=%d zeta=%d）：\n%s", alpha, beta, zeta, text)
	}
	if !strings.Contains(text, "- 路径：`application/alpha`") {
		t.Fatalf("相对路径没有被写进渲染件：\n%s", text)
	}
	if strings.Count(text, "- 文档：`docs/alpha.md`") != 1 {
		t.Fatalf("重复的相对文档没有被去重：\n%s", text)
	}
	if !strings.Contains(text, "- 文档：`docs/zeta.md`") {
		t.Fatalf("zeta 的仓库相对文档没有被写进渲染件：\n%s", text)
	}

	// ⑤ 只有 BuiltAt 不同 ⇒ 渲染字节完全相同（这是"每次刷新都不产生 diff"的判据）。
	sameExceptBuiltAt := record
	sameExceptBuiltAt.BuiltAt = record.BuiltAt.Add(37 * time.Hour)
	if again := storeproject.ProjectKnownMarkdown(sameExceptBuiltAt); string(again) != text {
		t.Fatalf("BuiltAt 变化影响了渲染字节：\n%s\n---\n%s", text, again)
	}
	// 纯函数：不改入参（入参顺序必须原样保留）。
	if record.Modules[0].Name != "zeta" || record.Modules[2].Name != "beta" {
		t.Fatalf("渲染改动了入参顺序：%+v", record.Modules)
	}
}

// TestProjectKnownPassiveOnly 对应 T5.4（R6）：**无显式调用 → 不产文件**。
// 装配 / 构造 / 刷新记录 / 纯渲染都不得在盘上留下 project-known.md。
func TestProjectKnownPassiveOnly(t *testing.T) {
	router := newTestRouter(t)
	const workspaceID = "ws-1700000000000000000"
	router.SetWorkspace(workspaceID)
	repository, ok := router.jsonRepository()
	if !ok {
		t.Fatal("router 不是 JSON 会话存储布局")
	}
	projectRoot := t.TempDir()
	path := storeproject.KnownPath(projectRoot)

	// 构造 + 刷新（写 project-record.json）之后：渲染件仍不存在。
	root := newKnowledgeProject(t)
	builder := storeproject.NewProjectKnowledgeBuilder(storeproject.ProjectKnowledgeSources{Root: root})
	result, err := storeproject.RefreshProjectKnowledge(context.Background(), router, workspaceID, builder, false)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(repository.projectDir(workspaceID), "project-record.json")); statErr != nil {
		t.Fatalf("刷新应写下项目记录：%v", statErr)
	}
	// 纯渲染同样不产文件（渲染是纯函数，不是写动作）。
	_ = storeproject.ProjectKnownMarkdown(result.Record)
	if _, statErr := os.Stat(path); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("无显式调用就产出了渲染件（%v）：%v", path, statErr)
	}
	if _, readErr := storeproject.ReadKnown(projectRoot); !errors.Is(readErr, fs.ErrNotExist) {
		t.Fatalf("未渲染过的 ReadProjectKnown 错误 = %v，期望 fs.ErrNotExist", readErr)
	}

	// 显式调用才产文件。
	if err := storeproject.WriteKnown(projectRoot, storeproject.ProjectKnownMarkdown(result.Record)); err != nil {
		t.Fatalf("显式渲染落盘失败：%v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("显式调用后渲染件仍不存在：%v", statErr)
	}

	// 空根 / 空内容显式报错，且不产空文件。
	if err := storeproject.WriteKnown("  ", []byte("x")); err == nil {
		t.Fatal("空 projectRoot 必须显式报错")
	}
	if err := storeproject.WriteKnown(projectRoot, nil); err == nil {
		t.Fatal("空内容必须显式报错（空文件不是一份渲染件）")
	}
	if err := storeproject.WriteKnown(projectRoot, []byte("   \n")); err == nil {
		t.Fatal("空白内容必须显式报错")
	}
	if _, err := storeproject.ReadKnown(""); err == nil {
		t.Fatal("空 projectRoot 读取必须显式报错")
	}
	// 上面三次失败写入不得把既有渲染件清空 / 改成空文件。
	data, readErr := storeproject.ReadKnown(projectRoot)
	if readErr != nil {
		t.Fatalf("渲染件被失败写入破坏：%v", readErr)
	}
	if len(data) == 0 {
		t.Fatal("渲染件成了空文件")
	}
}

// TestProjectKnownReusesExistingChain 对应 T5.5：**命中即处理、未命中传递** ——
// 谁能产出 storeproject.ProjectRecord 仍只有 storeproject.RefreshProjectKnowledge 三分支；
// 渲染只是它的读侧视图（复用 storeproject.ProjectKnowledgeBuilder / storeproject.RefreshProjectKnowledge）。
func TestProjectKnownReusesExistingChain(t *testing.T) {
	router := newTestRouter(t)
	const workspaceID = "ws-1700000000000000000"
	router.SetWorkspace(workspaceID)
	projectRoot := t.TempDir()
	knownPath := storeproject.KnownPath(projectRoot)

	root := newKnowledgeProject(t)
	builder := storeproject.NewProjectKnowledgeBuilder(storeproject.ProjectKnowledgeSources{Root: root})

	// [未命中] 首建：来源已变（无既有记录）⇒ 走重建分支。
	built, err := storeproject.RefreshProjectKnowledge(context.Background(), router, workspaceID, builder, false)
	if err != nil {
		t.Fatalf("首建失败：%v", err)
	}
	if built.Reused || built.Fallback {
		t.Fatalf("首建不应命中复用/回退：%+v", built)
	}
	markdown := storeproject.ProjectKnownMarkdown(built.Record)
	if err := storeproject.WriteKnown(projectRoot, markdown); err != nil {
		t.Fatal(err)
	}
	firstBytes, err := storeproject.ReadKnown(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstBytes) != string(markdown) {
		t.Fatalf("读回 ≠ 写入：\n%s\n---\n%s", firstBytes, markdown)
	}
	if !strings.Contains(string(firstBytes), "版本：`"+built.Record.Version+"`") {
		t.Fatalf("渲染件没有绑定 storeproject.ProjectRecord.Version（渲染不是它的视图）：\n%s", firstBytes)
	}

	// [命中] 来源 hash 未变 ⇒ 复用分支：记录不重建、渲染字节逐字节不变。
	reused, err := storeproject.RefreshProjectKnowledge(context.Background(), router, workspaceID, builder, false)
	if err != nil {
		t.Fatalf("复用刷新失败：%v", err)
	}
	if !reused.Reused {
		t.Fatalf("来源未变应命中复用分支：%+v", reused)
	}
	if reused.Record.Version != built.Record.Version {
		t.Fatalf("复用分支换了版本：%q → %q", built.Record.Version, reused.Record.Version)
	}
	// 复用分支重渲染后仍逐字节相同（确定性渲染 + 无时间戳 ⇒ 不产生 diff）。
	reusedBytes := storeproject.ProjectKnownMarkdown(reused.Record)
	if string(reusedBytes) != string(firstBytes) {
		t.Fatalf("复用分支渲染出了不同字节：\n%s\n---\n%s", firstBytes, reusedBytes)
	}

	// [渲染不写事实] 拿一条"外部"记录渲染，既有项目记录不被改写。
	stray, _ := projectKnownRecord(t, time.Unix(1, 0).UTC())
	_ = storeproject.ProjectKnownMarkdown(stray)
	stored, loadErr := router.LoadProjectRecord(workspaceID)
	if loadErr != nil {
		t.Fatalf("渲染动作把项目记录弄丢了：%v", loadErr)
	}
	if stored.Version != built.Record.Version {
		t.Fatalf("渲染动作改写了项目记录：%q → %q", built.Record.Version, stored.Version)
	}
	// 渲染不产文件：删掉渲染件后纯渲染不复活它（只有显式写才落盘）。
	if err := os.Remove(knownPath); err != nil {
		t.Fatal(err)
	}
	_ = storeproject.ProjectKnownMarkdown(stored)
	if _, statErr := os.Stat(knownPath); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("纯渲染把渲染件复活了：%v", statErr)
	}
}

// TestProjectKnownRebuildFailureKeepsLast 对应 T5.6：重建失败保留上一版本；
// 首建失败显式报错、不产空文件。
func TestProjectKnownRebuildFailureKeepsLast(t *testing.T) {
	router := newTestRouter(t)
	const workspaceID = "ws-1700000000000000000"
	router.SetWorkspace(workspaceID)
	projectRoot := t.TempDir()
	knownPath := storeproject.KnownPath(projectRoot)

	root := newKnowledgeProject(t)
	builder := storeproject.NewProjectKnowledgeBuilder(storeproject.ProjectKnowledgeSources{Root: root})
	built, err := storeproject.RefreshProjectKnowledge(context.Background(), router, workspaceID, builder, false)
	if err != nil {
		t.Fatalf("首建失败：%v", err)
	}
	first := storeproject.ProjectKnownMarkdown(built.Record)
	if err := storeproject.WriteKnown(projectRoot, first); err != nil {
		t.Fatal(err)
	}

	// 让来源全部落空（元数据 / 文档目录 / 手写说明一起消失）⇒ 重建不可能。
	for _, victim := range []string{builder.MetadataPath(), builder.ModulesDir(), builder.ManifestPath()} {
		if err := os.RemoveAll(victim); err != nil {
			t.Fatal(err)
		}
	}
	fallback, err := storeproject.RefreshProjectKnowledge(context.Background(), router, workspaceID, builder, true)
	if err != nil {
		t.Fatalf("已有记录时重建失败应回退而不是报错：%v", err)
	}
	if !fallback.Fallback || fallback.Note == "" {
		t.Fatalf("重建失败未走回退分支：%+v", fallback)
	}
	if fallback.Record.Version != built.Record.Version {
		t.Fatalf("回退分支换了版本：%q → %q", built.Record.Version, fallback.Record.Version)
	}
	// 回退 ⇒ 渲染件保持上一版本（handler 在 fallback 时不重写；这里同口径复核）。
	if _, statErr := os.Stat(knownPath); statErr != nil {
		t.Fatalf("回退后渲染件应当仍在盘上：%v", statErr)
	}
	kept, readErr := storeproject.ReadKnown(projectRoot)
	if readErr != nil {
		t.Fatalf("回退把渲染件弄丢了：%v", readErr)
	}
	if string(kept) != string(first) {
		t.Fatalf("回退改动了渲染件：\n%s\n---\n%s", first, kept)
	}

	// 首建失败（无既有记录 + 无来源）⇒ 显式报错，且不产空文件。
	fresh := newTestRouter(t)
	fresh.SetWorkspace(workspaceID)
	freshRoot := t.TempDir()
	freshKnown := storeproject.KnownPath(freshRoot)
	emptyBuilder := storeproject.NewProjectKnowledgeBuilder(storeproject.ProjectKnowledgeSources{Root: t.TempDir()})
	if _, err := storeproject.RefreshProjectKnowledge(context.Background(), fresh, workspaceID, emptyBuilder, false); err == nil {
		t.Fatal("首建失败必须显式报错（不产空记录）")
	} else if !strings.Contains(err.Error(), "no sources") {
		t.Fatalf("首建失败应点明来源缺失：%v", err)
	}
	if _, statErr := os.Stat(freshKnown); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("首建失败产出了渲染件：%v", statErr)
	}
}

// TestProjectKnownPathFollowsProjectRoot 覆盖"落点跟**项目根**走"：换一个项目根，
// 渲染件随之落到那个项目的 `.seelex/board/` 下（不再跟数据根/hash 目录走）。
func TestProjectKnownPathFollowsProjectRoot(t *testing.T) {
	paths := make([]string, 0, 2)
	roots := make([]string, 0, 2)
	for index := 0; index < 2; index++ {
		projectRoot := t.TempDir()
		path := storeproject.KnownPath(projectRoot)
		want := filepath.Join(projectRoot, ".seelex", "board", storeproject.KnownFile)
		if path != want {
			t.Fatalf("落点 = %q，期望 %q", path, want)
		}
		if !strings.HasPrefix(path, projectRoot) {
			t.Fatalf("落点不在项目根之下：%q vs %q", path, projectRoot)
		}
		if err := storeproject.WriteKnown(projectRoot, []byte("# 项目摘要（Project Known）\n\n## core\n\ncore 职责\n")); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("渲染件没有落在项目目录下：%v", err)
		}
		roots = append(roots, projectRoot)
		paths = append(paths, path)
	}
	if roots[0] == roots[1] {
		t.Fatal("两次构造用了同一个项目根，用例无效")
	}
	if paths[0] == paths[1] {
		t.Fatalf("换项目根后落点没变：%q", paths[0])
	}
	// 两个项目互不可见：第一个项目根读不到第二个项目的渲染件。
	if _, err := storeproject.ReadKnown(t.TempDir()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("另一个项目根读到了别的项目的渲染件：%v", err)
	}
}
