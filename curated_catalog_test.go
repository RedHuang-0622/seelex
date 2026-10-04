package main

// plugins/curated.yaml 的落地守卫（仿 plugin_doc_sigil_test.go）：精选目录必须与
// **这个仓库此刻真实落盘的插件**一致。
//
// 判别力来自两侧：plugin/curated.go 的严格解码 + 校验（缺字段/引用了不存在的目录/
// pending 里混进已装插件 → 错误），以及 plugin/curated_test.go 里那组对抗性用例
// （证明校验不是"永远报错"也不是"什么都收"）。本文件只管一件事：**发行仓库里那份
// 文件本身**是对的，且它和 README 索引、加载器读出的插件集合三方一致。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/plugin"
)

const curatedReadmePath = "plugins/README.md"

func TestShippedCuratedCatalogMatchesInstalledPlugins(t *testing.T) {
	root := "plugins"
	if _, err := os.Stat(filepath.Join(root, plugin.CuratedFileName)); err != nil {
		t.Fatalf("%s 未落盘: %v（精选目录是发行包的一部分）", filepath.Join(root, plugin.CuratedFileName), err)
	}
	catalog, err := plugin.LoadCuratedFromRoot(root)
	if err != nil {
		t.Fatalf("精选目录自解析/交叉校验失败: %v", err)
	}

	loaded, err := plugin.NewLoader(root).LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 3 {
		t.Fatalf("loaded %d plugins, want 3 (default + freecad + impeccable)", len(loaded))
	}
	for _, p := range loaded {
		entry, ok := catalog.Entry(p.Name)
		if !ok {
			t.Errorf("插件 %q 已落盘但不在精选目录 entries 里", p.Name)
			continue
		}
		if strings.TrimSpace(entry.Description) == "" || entry.Source.URL == "" {
			t.Errorf("插件 %q 的目录条目缺少 description/source（哪个插件是谁给的必须可读）", p.Name)
		}
	}
	if len(catalog.Entries) != len(loaded) {
		t.Errorf("entries = %d 条，已落盘插件 = %d 个：精选目录只列已落盘插件", len(catalog.Entries), len(loaded))
	}

	// 启动基线必须在册且指向 default（activateDefaultPlugin 装配的就是它）。
	baseline, ok := catalog.Preset(plugin.CuratedBaselinePreset)
	if !ok {
		t.Fatalf("缺少 %q preset", plugin.CuratedBaselinePreset)
	}
	if !containsValue(baseline.Plugins, "default") {
		t.Errorf("%q preset plugins = %v, want it to contain default", plugin.CuratedBaselinePreset, baseline.Plugins)
	}
}

// TestPluginsReadmeIndexMatchesCuratedCatalog 把 README 的索引表钉在目录上：
// 表 ↔ entries 必须一一对应（新增插件只改一处就红）。
func TestPluginsReadmeIndexMatchesCuratedCatalog(t *testing.T) {
	readme, err := os.ReadFile(curatedReadmePath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := plugin.LoadCuratedFromRoot("plugins")
	if err != nil {
		t.Fatalf("精选目录: %v", err)
	}

	listed := readmeIndexNames(t, string(readme))
	if len(listed) == 0 {
		t.Fatalf("%s 的插件索引表没被解析到：守卫失效（表结构变了？）", curatedReadmePath)
	}
	for _, entry := range catalog.Entries {
		if !containsValue(listed, entry.Name) {
			t.Errorf("%s 的索引表缺少插件 %q（已落盘且已入精选目录）", curatedReadmePath, entry.Name)
		}
	}
	for _, name := range listed {
		if _, ok := catalog.Entry(name); !ok {
			t.Errorf("%s 的索引表列了 %q，但精选目录 entries 里没有它", curatedReadmePath, name)
		}
	}
	if !strings.Contains(string(readme), plugin.CuratedFileName) {
		t.Errorf("%s 必须解释 %s 是什么（旁车数据文件，不是插件）", curatedReadmePath, plugin.CuratedFileName)
	}
}

// readmeIndexNames 解析 `| Plugin | 生态位 | README |` 表的第一列（反引号里的名字）。
func readmeIndexNames(t *testing.T, readme string) []string {
	t.Helper()
	lines := strings.Split(readme, "\n")
	start := -1
	for index, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "| Plugin |") {
			start = index
			break
		}
	}
	if start < 0 {
		return nil
	}
	names := make([]string, 0, 4)
	for _, line := range lines[start:] {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			break
		}
		cells := strings.Split(strings.Trim(trimmed, "|"), "|")
		first := strings.TrimSpace(cells[0])
		if !strings.HasPrefix(first, "`") || !strings.HasSuffix(first, "`") {
			continue // 表头与分隔行
		}
		names = append(names, strings.Trim(first, "`"))
	}
	return names
}

func containsValue(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
