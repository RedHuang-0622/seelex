package plugin

// 精选目录**写侧**的守卫：发现 → 读回 → 落进 yaml。
//
// 每条都对着一个"会让人退回去手抄"的退化实现：只登记本根的东西、只追加不改写、幂等、
// 落盘前的自检、坏目录拒绝改写、没有目录就不硬造一份。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// catalogFixtureYAML 造一份**合法**的目录文本：entries 与 installed 一一对应，baseline
// 含 default（结构守卫的硬要求），每个插件都在某个 preset 里有装配路径。
func catalogFixtureYAML(installed ...string) string {
	var builder strings.Builder
	builder.WriteString("schema_version: 1\nkind: curated-catalog\nread_at: 2026-10-05\nentries:\n")
	for _, name := range installed {
		builder.WriteString("  - name: " + name + "\n    description: " + name + " plugin\n    installed: true\n")
		builder.WriteString("    source:\n      kind: builtin\n      url: https://example.com/" + name +
			"\n      license: MIT\n      pinned: v1\n      read_at: 2026-10-05\n")
	}
	builder.WriteString("presets:\n  - name: baseline\n    description: baseline\n    permission_tier: manual\n    plugins: [")
	builder.WriteString(strings.Join(installed, ", "))
	builder.WriteString("]\n    pending: []\n")
	return builder.String()
}

// catalogRoot 造一个"发行风格"的插件根：default + seed 两个真插件 + 一份带注释头的目录。
func catalogRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeCatalogPlugin(t, root, "default")
	writeCatalogPlugin(t, root, "seed")
	writeCatalogFile(t, root, "# 发行包自带（本机自建条目会被追加在这份之后）\n"+catalogFixtureYAML("default", "seed"))
	return root
}

func writeCatalogPlugin(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "---\nschema_version: 1\nname: " + name + "\ndescription: " + name + " plugin\n---\n\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, manifestFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeCatalogFile(t *testing.T, root, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, CuratedFileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// catalogLoaded 造一个"已加载"的读数（名字 + manifest 描述 + 它所在的根，正是 Loader 给出的那三面）。
func catalogLoaded(root string, names ...string) []Plugin {
	loaded := make([]Plugin, 0, len(names))
	for _, name := range names {
		loaded = append(loaded, Plugin{
			Name:        name,
			Description: name + " plugin",
			RootDir:     filepath.Join(root, name),
		})
	}
	return loaded
}

// linesFormSubsequence 判 before 的每条非空行是否**按原序**出现在 after 里（登记只许在原
// 文本上插入，不许重写：重排/改写都会让这条断言红）。
func linesFormSubsequence(before, after string) bool {
	remaining := strings.Split(after, "\n")
	cursor := 0
	for _, line := range strings.Split(before, "\n") {
		line = strings.TrimRight(line, " \t\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		found := false
		for cursor < len(remaining) {
			if strings.TrimRight(remaining[cursor], " \t\r") == line {
				cursor++
				found = true
				break
			}
			cursor++
		}
		if !found {
			return false
		}
	}
	return true
}

func TestRegisterDiscoveredPluginsAppendsLocalEntryAndPreset(t *testing.T) {
	root := catalogRoot(t)
	before, err := os.ReadFile(filepath.Join(root, CuratedFileName))
	if err != nil {
		t.Fatal(err)
	}

	// artist/backend 是本机新建的；default/seed 已登记 ⇒ 不动。
	loaded := catalogLoaded(root, "artist", "default", "seed", "backend")
	registered, err := RegisterDiscoveredPlugins(root, loaded)
	if err != nil {
		t.Fatalf("登记: %v", err)
	}
	if len(registered) != 2 || registered[0] != "artist" || registered[1] != "backend" {
		t.Fatalf("registered = %v，want [artist backend]（顺序 = 调用方给的顺序）", registered)
	}

	after, err := os.ReadFile(filepath.Join(root, CuratedFileName))
	if err != nil {
		t.Fatal(err)
	}
	// 只追加：原有行必须**按原序**整段还在（登记只许往块里插，不许重写整份文件）。
	if !linesFormSubsequence(string(before), string(after)) {
		t.Fatalf("登记改写了已有内容（只许追加）:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
	if !strings.Contains(string(after), "# 发行包自带") {
		t.Fatal("登记把目录的注释头洗掉了（注释也是簿记）")
	}

	catalog, err := ParseCuratedCatalog(after)
	if err != nil {
		t.Fatalf("登记后的目录必须解得动: %v", err)
	}
	for _, name := range []string{"artist", "backend"} {
		entry, ok := catalog.Entry(name)
		if !ok {
			t.Fatalf("目录里没有 %q（发现 → 读回失败）", name)
		}
		if entry.Source.Kind != CuratedSourceLocal {
			t.Errorf("%s: source.kind = %q，want %q", name, entry.Source.Kind, CuratedSourceLocal)
		}
		if !strings.HasPrefix(entry.Source.URL, "local:") {
			t.Errorf("%s: source.url = %q，必须指向它所在的本机根", name, entry.Source.URL)
		}
		if entry.Source.License == "" || entry.Source.Pinned == "" || entry.Source.ReadAt == "" {
			t.Errorf("%s: source 五项必须填满（哪个插件是谁给的必须可读）: %+v", name, entry.Source)
		}
		if entry.Description != name+" plugin" {
			t.Errorf("%s: description = %q，want manifest 里的那句话", name, entry.Description)
		}
	}
	preset, ok := catalog.Preset(CuratedLocalPreset)
	if !ok {
		t.Fatalf("没有 %q preset：新条目会变成「已落盘但不属于任何 preset」的漂移", CuratedLocalPreset)
	}
	if len(preset.Plugins) != 2 || preset.Plugins[0] != "artist" || preset.Plugins[1] != "backend" {
		t.Fatalf("%s preset plugins = %v，want [artist backend]", CuratedLocalPreset, preset.Plugins)
	}

	// 最强的那条：登记完必须过**发行守卫**（entries ↔ 已装集合一一对应、每条都有装配路径）。
	if err := ValidateCuratedCatalog(catalog, []string{"artist", "backend", "default", "seed"}); err != nil {
		t.Fatalf("登记后的目录过不了发行守卫（等于造出一份自己都不认的目录）: %v", err)
	}
}

func TestRegisterDiscoveredPluginsIsIdempotent(t *testing.T) {
	root := catalogRoot(t)
	loaded := catalogLoaded(root, "artist", "default", "seed")
	if _, err := RegisterDiscoveredPlugins(root, loaded); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(root, CuratedFileName))
	if err != nil {
		t.Fatal(err)
	}
	registered, err := RegisterDiscoveredPlugins(root, loaded)
	if err != nil {
		t.Fatalf("第二次登记: %v", err)
	}
	if len(registered) != 0 {
		t.Fatalf("第二次登记又登记了 %v（幂等：已登记的名字不许重复追加）", registered)
	}
	second, err := os.ReadFile(filepath.Join(root, CuratedFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("无事可做时一个字节都不该动:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

// TestRegisterDiscoveredPluginsAddsToExistingLocalPreset 钉住第二次发现的**增量**路径：
// local preset 已存在时整块被重写成"原有成员 ∪ 本次登记"，而不是造出第二个 local preset。
func TestRegisterDiscoveredPluginsAddsToExistingLocalPreset(t *testing.T) {
	root := catalogRoot(t)
	if _, err := RegisterDiscoveredPlugins(root, catalogLoaded(root, "artist", "default", "seed")); err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterDiscoveredPlugins(root, catalogLoaded(root, "artist", "backend", "default", "seed")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, CuratedFileName))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := ParseCuratedCatalog(data)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, preset := range catalog.Presets {
		if preset.Name != CuratedLocalPreset {
			continue
		}
		count++
		if len(preset.Plugins) != 2 || preset.Plugins[0] != "artist" || preset.Plugins[1] != "backend" {
			t.Fatalf("%s preset plugins = %v，want [artist backend]（原有成员 ∪ 本次登记）", CuratedLocalPreset, preset.Plugins)
		}
	}
	if count != 1 {
		t.Fatalf("目录里出现了 %d 个 %q preset，want 1（升级已有那块，不是再追加一块）", count, CuratedLocalPreset)
	}
	if err := ValidateCuratedCatalog(catalog, []string{"artist", "backend", "default", "seed"}); err != nil {
		t.Fatalf("发行守卫: %v", err)
	}
}

// TestRegisterDiscoveredPluginsHandlesEmptyInlineEntries 钉住 `entries: []` 这种内联空表形态
// （块序列不能直接跟在空 flow 后面）。
func TestRegisterDiscoveredPluginsHandlesEmptyInlineEntries(t *testing.T) {
	root := t.TempDir()
	writeCatalogPlugin(t, root, "default")
	writeCatalogFile(t, root,
		"schema_version: 1\nkind: curated-catalog\nread_at: 2026-10-05\nentries: []\n"+
			"presets:\n  - name: baseline\n    description: baseline\n    permission_tier: manual\n"+
			"    plugins: [default]\n    pending: []\n")

	if _, err := RegisterDiscoveredPlugins(root, catalogLoaded(root, "default", "artist")); err != nil {
		t.Fatalf("登记: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, CuratedFileName))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := ParseCuratedCatalog(data)
	if err != nil {
		t.Fatalf("内联空表登记后必须解得动: %v\n%s", err, data)
	}
	if _, ok := catalog.Entry("artist"); !ok {
		t.Fatalf("`entries: []` 形态没被登记:\n%s", data)
	}
	if _, ok := catalog.Preset(CuratedLocalPreset); !ok {
		t.Fatalf("新 preset 没被追加:\n%s", data)
	}
}

// TestRegisterDiscoveredPluginsIgnoresOtherRoots 钉住多根 first-wins 下的归属判定：
// 只登记**落在本根下面**的插件；别的根供上来的插件不属于这份目录（登记进来会造出第二条
// 谎：目录说"它在根里"，而它根本不在）。
func TestRegisterDiscoveredPluginsIgnoresOtherRoots(t *testing.T) {
	root := catalogRoot(t)
	elsewhere := t.TempDir()
	writeCatalogPlugin(t, elsewhere, "outsider")

	registered, err := RegisterDiscoveredPlugins(root, []Plugin{
		{Name: "artist", Description: "A", RootDir: filepath.Join(root, "artist")},
		{Name: "outsider", Description: "O", RootDir: filepath.Join(elsewhere, "outsider")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(registered) != 1 || registered[0] != "artist" {
		t.Fatalf("registered = %v，want 只有本根下的 [artist]", registered)
	}
	data, err := os.ReadFile(filepath.Join(root, CuratedFileName))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := ParseCuratedCatalog(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := catalog.Entry("outsider"); ok {
		t.Fatal("别的根供上来的插件被登记进了这份目录")
	}
}

// TestRegisterDiscoveredPluginsRefusesBrokenCatalog 钉住"落盘前自检/坏目录不改写"：
// 一份读不动的目录必须原样留着（宁可不登记），而不是被半截新文本覆盖。
func TestRegisterDiscoveredPluginsRefusesBrokenCatalog(t *testing.T) {
	root := t.TempDir()
	broken := "schema_version: 1\nkind: curated-catalog\n"
	writeCatalogFile(t, root, broken)
	if _, err := RegisterDiscoveredPlugins(root, catalogLoaded(root, "artist")); err == nil {
		t.Fatal("坏目录必须报错，不得改写")
	}
	after, err := os.ReadFile(filepath.Join(root, CuratedFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != broken {
		t.Fatalf("坏目录被改写了（现场必须原样留着）: %s", after)
	}
}

// TestRegisterDiscoveredPluginsWithoutCatalogIsNoop 钉住"自建根/用户树可以不带精选目录"：
// 没有目录文件时不硬造一份、不报错。
func TestRegisterDiscoveredPluginsWithoutCatalogIsNoop(t *testing.T) {
	root := t.TempDir()
	writeCatalogPlugin(t, root, "artist")
	registered, err := RegisterDiscoveredPlugins(root, catalogLoaded(root, "artist"))
	if err != nil {
		t.Fatalf("没有目录不该报错（合法现场）: %v", err)
	}
	if len(registered) != 0 {
		t.Fatalf("registered = %v，want 空", registered)
	}
	if _, err := os.Stat(filepath.Join(root, CuratedFileName)); err == nil {
		t.Fatal("没有目录时不许替使用者造一份")
	}
}

// TestManagerRegisterDiscoveredPluginsWalksRootChain 钉住写侧要走**读侧同一条**责任链：
// 链首不存在、链上第二个根带目录 ⇒ 写那份（与 plugin_create 的 PrimaryRoot 同源）。
func TestManagerRegisterDiscoveredPluginsWalksRootChain(t *testing.T) {
	base := t.TempDir()
	missing := filepath.Join(base, "exe-plugins")
	real := catalogRoot(t)

	m := NewManager(NewLoader(missing, real), &fakeTools{}, &fakeMCP{}, &fakeSkills{})
	if err := m.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	writeCatalogPlugin(t, real, "artist")
	if _, err := m.Reload(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	registered, err := m.RegisterDiscoveredPlugins()
	if err != nil {
		t.Fatalf("登记: %v", err)
	}
	if len(registered) != 1 || registered[0] != "artist" {
		t.Fatalf("registered = %v，want [artist]", registered)
	}
	data, err := os.ReadFile(filepath.Join(real, CuratedFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "name: artist") {
		t.Fatalf("登记没落到链上那份真在用的目录:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(missing, CuratedFileName)); err == nil {
		t.Fatal("登记写进了不存在的根")
	}
}
