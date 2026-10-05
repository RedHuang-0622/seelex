package core

// plugin_source_projection_test.go — 「这些前端显示出来的 plugin 我没有在我的 plugin
// 下面见过」在应用层会话快照里的接线。
//
// 为什么这一层要单独钉：来源读数一共有四跳（curated.yaml → plugin.Manager →
// adapters.PluginPort → view_state 收集 → 会话快照 JSON 键），中间任何一跳断了，前端
// 拿到的都是 undefined——面板照旧只列名字，没有任何报错，用户只能继续猜"这插件哪来的"。
// 所以这里从**真实**的 PluginPort（真的 loader、真的多根根链、真的 curated.yaml）走一遍，
// 而不是喂一个手写的 PluginInfo。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/internal/adapters"
	"github.com/RedHuang-0622/seelex/plugin"
	"github.com/RedHuang-0622/seelex/skill"
)

func TestCollectRuntimeProjectionCarriesPluginSource(t *testing.T) {
	// 两个根：链首是"发行载荷"（随包走的那份），链尾是"本机自建"（用户自己那块）。
	payloadRoot, localRoot := t.TempDir(), t.TempDir()
	for _, name := range []string{"default", "hardware", "impeccable"} {
		writeSourcePlugin(t, payloadRoot, name)
	}
	for _, name := range []string{"frontend", "hardware"} {
		writeSourcePlugin(t, localRoot, name)
	}
	// 有目录、但精选目录没登记它：来源读不出来（该留空），但"从哪个根载入"仍是事实。
	writeSourcePlugin(t, localRoot, "unlisted")
	writeSourceCatalog(t, payloadRoot, sourceCatalogYAML(localRoot))

	manager := loadSourceManager(t, payloadRoot, localRoot)
	installed := make([]string, 0, 8)
	for _, item := range manager.All() {
		installed = append(installed, item.Name)
	}
	catalog, _, err := plugin.LoadCuratedForRuntime(payloadRoot, installed)
	if err != nil {
		t.Fatalf("读精选目录: %v", err)
	}
	// 与组合根（initPluginSystem）同一动作：来源判定源挂到 manager 上。
	manager.SetCuratedCatalog(catalog)

	svc := newTestService(t, &fakeEngine{}, withTestPlugins(adapters.PluginPort{Manager: manager}))
	projection := svc.collectRuntimeProjection(t.Context())

	byName := make(map[string]PluginInfo, len(projection.Runtime.Plugins))
	for _, item := range projection.Runtime.Plugins {
		byName[item.Name] = item
	}
	if len(byName) != 5 {
		t.Fatalf("快照里应有 5 个插件（两根本机自建 + 载荷三个），得 %d: %v", len(byName), byName)
	}

	// ① 发行载荷：来源面读出 builtin，载入位置是链首那个根。
	if payload := byName["default"]; payload.SourceKind != "builtin" ||
		payload.SourceURL != "https://github.com/RedHuang-0622/seelex" ||
		payload.SourceRoot != filepath.Join(payloadRoot, "default") {
		t.Fatalf("发行载荷的来源面不对：%+v", payload)
	}
	if vendored := byName["impeccable"]; vendored.SourceKind != "vendored" {
		t.Fatalf("随包第三方应是 vendored：%+v", vendored)
	}

	// ② 本机自建（有目录、目录里标明 local）与发行载荷**可区分**。
	if local := byName["frontend"]; local.SourceKind != "local" ||
		local.SourceURL != "local:"+localRoot ||
		local.SourceRoot != filepath.Join(localRoot, "frontend") {
		t.Fatalf("本机自建插件必须读成 local + 那个根：%+v", local)
	}

	// ② 多根 first-wins：同名 hardware 只算链上先出现的那个根（载荷那份），
	//    链尾那份是死代码，不许被报成"这个插件的载入位置"。
	if hardware := byName["hardware"]; hardware.SourceRoot != filepath.Join(payloadRoot, "hardware") {
		t.Fatalf("first-wins 下载入根必须是链首那份，得 %q", hardware.SourceRoot)
	} else if hardware.SourceKind != "builtin" {
		t.Fatalf("first-wins 后来源面必须跟着链首那份走：%+v", hardware)
	}

	// ③ 缺失来源 = 不编：精选目录里没有它，来源两项必须空，而不是默认成 builtin。
	//    载入位置不是"谁来给的"，它是事实，照旧下发。
	if unlisted := byName["unlisted"]; unlisted.SourceKind != "" || unlisted.SourceURL != "" {
		t.Fatalf("未登记的插件不许编来源：%+v", unlisted)
	} else if unlisted.SourceRoot != filepath.Join(localRoot, "unlisted") {
		t.Fatalf("未登记插件的载入位置仍必须可读：%+v", unlisted)
	}

	// JSON 键是前后端唯一的对接口径：前端读的就是这三个键。
	encoded, err := json.Marshal(projection.Runtime)
	if err != nil {
		t.Fatalf("encode runtime: %v", err)
	}
	for _, want := range []string{`"source_kind":"local"`, `"source_kind":"builtin"`, `"source_url":"local:`, `"source_root":`} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("会话运行原件里必须下发 %s：%s", want, encoded)
		}
	}
	var decoded struct {
		Plugins []map[string]any `json:"plugins"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode runtime: %v", err)
	}
	for _, row := range decoded.Plugins {
		if row["name"] != "unlisted" {
			continue
		}
		if _, present := row["source_kind"]; present {
			t.Fatalf("缺失来源必须整键缺席（omitempty），而不是给个默认值：%v", row)
		}
		if _, present := row["source_url"]; present {
			t.Fatalf("缺失来源必须整键缺席（omitempty）：%v", row)
		}
		if row["source_root"] == nil {
			t.Fatalf("载入位置是事实，必须下发：%v", row)
		}
	}
}

// ── 夹具 ───────────────────────────────────────────────────────────────────

// sourceCatalogYAML 生成一份**合法**的精选目录：三个随包条目（builtin/vendored）+
// 一个本机自建条目（kind=local，url 是 `local:<那个根>`，与交付树里的写法一致）。
func sourceCatalogYAML(localRoot string) string {
	var builder strings.Builder
	builder.WriteString("schema_version: 1\nkind: curated-catalog\nread_at: 2026-10-05\nentries:\n")
	entry := func(name, kind, url string) {
		builder.WriteString("  - name: " + name + "\n    description: " + name +
			" plugin\n    installed: true\n    source:\n")
		builder.WriteString("      kind: " + kind + "\n      url: " + url + "\n")
		builder.WriteString("      license: MIT\n      pinned: v1\n      read_at: 2026-10-05\n")
	}
	entry("default", "builtin", "https://github.com/RedHuang-0622/seelex")
	entry("hardware", "builtin", "https://github.com/RedHuang-0622/seelex")
	entry("impeccable", "vendored", "https://github.com/pbakaus/impeccable")
	// 单引号：Windows 根路径里的反斜杠必须是字面量（双引号会当转义符）。
	entry("frontend", "local", "'local:"+localRoot+"'")
	builder.WriteString("presets:\n  - name: baseline\n    description: baseline\n" +
		"    permission_tier: manual\n    plugins: [default]\n    pending: []\n")
	return builder.String()
}

func writeSourcePlugin(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "---\nschema_version: 1\nname: " + name + "\ndescription: " + name + " plugin\n---\n\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "plugin.md"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeSourceCatalog(t *testing.T, root, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, plugin.CuratedFileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// loadSourceManager 走生产同一条路（多根 loader + manager 事务加载）。
func loadSourceManager(t *testing.T, roots ...string) *plugin.Manager {
	t.Helper()
	manager := plugin.NewManager(plugin.NewLoader(roots...), sourceTestTools{}, sourceTestMCP{}, sourceTestSkills{})
	if err := manager.Load(); err != nil {
		t.Fatalf("加载插件: %v", err)
	}
	return manager
}

// sourceTest* 是本用例的加载后端桩：本用例只关心「从哪来」，不碰工具/技能装配面。
type sourceTestTools struct{}

func (sourceTestTools) DefinePlugin(string, string, []string, []string) error { return nil }
func (sourceTestTools) UndefinePlugin(string)                                 {}
func (sourceTestTools) ActivatePlugin(string) error                           { return nil }
func (sourceTestTools) DeactivatePlugin()                                     {}
func (sourceTestTools) ActivePlugin() string                                  { return "" }

type sourceTestMCP struct{}

func (sourceTestMCP) AttachMCPServer(_ context.Context, _, _, _ string, _, _ []string, _ string) error {
	return nil
}
func (sourceTestMCP) DetachMCP(string) error { return nil }

type sourceTestSkills struct{}

func (sourceTestSkills) PublishPluginSkills(string, []skill.Skill) error { return nil }
func (sourceTestSkills) ClearPluginSkills(string)                        {}
func (sourceTestSkills) ActivatePluginSkills(string) error               { return nil }
func (sourceTestSkills) DeactivatePluginSkills() error                   { return nil }
