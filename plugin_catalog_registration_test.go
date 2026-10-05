package main

// 精选目录**写侧**的链路用例（2026-10-05 事故的正面回归）。
//
// 现场：main agent 用 `plugin_create` 造出插件（下面还带 skill）放进运行树的 plugins/，
// 但 `plugins/curated.yaml` 里没有它 —— 于是每次启动只能把磁盘上的事实读成"漂移"，使用者
// 看到的是一条他无从下手的警告（"已落盘插件 %q 不在 entries 里"）。要他手抄进精选目录，
// 就是把机器该做的簿记推给人。
//
// 本文件把「**发现 → 读回 → 落进 yaml**」这条闭环钉在真链路上：临时插件根（含一份发行
// 风格的 curated.yaml + 一个 seed 插件）+ 真 `initPluginSystem` + 真 `plugin_create` /
// `plugins_reload` 工具，断言那份 yaml **文件本身**真的多出了登记条目。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/plugin"
	"github.com/RedHuang-0622/seelex/seelebridge"
)

// catalogFixtureYAML 是"发行包自带的那份目录"的缩样：一个已登记插件（seed）+ 一条装配
// 路径（baseline preset），外加一段**注释头**——注册不许把它洗掉（注释也是簿记）。
const catalogFixtureYAML = `# 发行包自带（本机自建条目会被追加在这份之后）
schema_version: 1
kind: curated-catalog
read_at: 2026-10-05

entries:
  - name: default
    description: baseline plugin
    installed: true
    source:
      kind: builtin
      url: https://example.com/default
      license: MIT
      pinned: v1
      read_at: 2026-10-05
  - name: seed
    description: seed plugin
    installed: true
    source:
      kind: builtin
      url: https://example.com/seed
      license: MIT
      pinned: v1
      read_at: 2026-10-05

presets:
  - name: baseline
    description: baseline
    permission_tier: manual
    plugins: [default, seed]
    pending: []
`

// catalogHarness 起一条真链路：临时插件根（seed 插件 + 上面那份 curated.yaml）+ 真
// runtime + 真插件系统。pre 在**启动之前**跑，用来放"别处进来的"插件目录。
func catalogHarness(t *testing.T, pre func(root string)) (string, func(name, args string) (string, error)) {
	t.Helper()
	originalPlugins := *pluginsPaths
	originalStore := *storePath
	temp := t.TempDir()
	root := filepath.Join(temp, "plugins")
	*pluginsPaths = root
	*storePath = filepath.Join(temp, "sessions")
	t.Cleanup(func() {
		*pluginsPaths = originalPlugins
		*storePath = originalStore
	})

	if err := os.MkdirAll(filepath.Join(root, "seed"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "seed", "plugin.md"),
		[]byte("---\nschema_version: 1\nname: seed\ndescription: seed plugin\n---\n# Seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, plugin.CuratedFileName), []byte(catalogFixtureYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if pre != nil {
		pre(root)
	}

	accountsPath := filepath.Join(temp, "accounts.yaml")
	minimalAccounts := `defaults:
  provider: openai
  context_window: 200000
  max_tokens: 8192
  timeout: 120s
  temperature: 0
roles:
  subagent:
    - model: test-model
      base_url: http://127.0.0.1:9/v1
      api_key: test-only-key
mcp_servers: []
`
	if err := os.WriteFile(accountsPath, []byte(minimalAccounts), 0o600); err != nil {
		t.Fatal(err)
	}

	runtime, err := seelebridge.NewRuntime(seelebridge.RuntimeConfig{
		AccountsPath:    accountsPath,
		StorePath:       filepath.Join(temp, "runtime"),
		ToolCallTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	t.Cleanup(runtime.Shutdown)
	plugins, _, err := initPluginSystem(runtime, initSkillSystem())
	if err != nil {
		t.Fatalf("initPluginSystem: %v", err)
	}
	registerPluginSelfTools(runtime, plugins)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return root, func(name, args string) (string, error) {
		return runtime.Agent().DirectDispatch(ctx, name, args)
	}
}

// TestPluginsReloadRegistersCreatedPluginInCatalog 是用户现场那条链路的正面回归：
// plugin_create（自带 skill）→ plugins_reload → 精选目录**文件里**多出一条本机自建登记。
func TestPluginsReloadRegistersCreatedPluginInCatalog(t *testing.T) {
	root, dispatch := catalogHarness(t, nil)

	if _, err := dispatch("plugin_create",
		`{"name":"artist","description":"绘画插件","prompt":"# Artist","skills":[{"name":"paint","description":"paint","prompt":"paint it"}]}`); err != nil {
		t.Fatalf("plugin_create: %v", err)
	}
	if _, err := dispatch("plugins_reload", `{}`); err != nil {
		t.Fatalf("plugins_reload: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, plugin.CuratedFileName))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := plugin.ParseCuratedCatalog(data)
	if err != nil {
		t.Fatalf("登记后的目录必须解得动: %v", err)
	}
	entry, ok := catalog.Entry("artist")
	if !ok {
		t.Fatalf("plugins_reload 之后 curated.yaml 里仍没有 artist —— 磁盘上的事实没被读回目录:\n%s", data)
	}
	if entry.Source.Kind != "local" {
		t.Errorf("本机自建条目的 source.kind = %q，want %q（它不是上游给的）", entry.Source.Kind, "local")
	}
	if entry.Description != "绘画插件" {
		t.Errorf("条目 description = %q，want 磁盘上 manifest 的那句话（读回的就是落盘的那份事实）", entry.Description)
	}
	if !entry.Installed {
		t.Error("本机自建的插件也是已落盘的，installed 必须为 true")
	}
	preset, ok := catalog.Preset("local")
	if !ok {
		t.Fatalf("登记后必须挂上 %q preset（否则目录里留下「已落盘但不属于任何 preset」的漂移）", "local")
	}
	if !catalogHasString(preset.Plugins, "artist") {
		t.Errorf("%q preset 的 plugins = %v，缺少刚登记的 artist", "local", preset.Plugins)
	}

	// 只追加：发行侧那份必须逐字节还在（注释头 + seed 条目）。
	if !strings.Contains(string(data), "# 发行包自带") {
		t.Error("登记把目录的注释头洗掉了：注册只许追加，不许重新序列化整份文件")
	}
	if !strings.Contains(string(data), "name: seed") || !strings.Contains(string(data), "name: baseline") {
		t.Error("登记动了发行侧已有条目/preset：只许追加")
	}
}

// TestStartupRegistersPluginAddedOutOfBand 钉住另一半：不是 plugin_create 造的（手工放置、
// 别的工具写进去、上一次会话留下的）插件，也要在启动期被发现并登记。
func TestStartupRegistersPluginAddedOutOfBand(t *testing.T) {
	root, _ := catalogHarness(t, func(root string) {
		dir := filepath.Join(root, "backend")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "plugin.md"),
			[]byte("---\nschema_version: 1\nname: backend\ndescription: 后端插件\n---\n# Backend\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	})

	data, err := os.ReadFile(filepath.Join(root, plugin.CuratedFileName))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := plugin.ParseCuratedCatalog(data)
	if err != nil {
		t.Fatalf("登记后的目录必须解得动: %v", err)
	}
	if _, ok := catalog.Entry("backend"); !ok {
		t.Fatalf("启动期没有把磁盘上已有的 backend 登记进目录:\n%s", data)
	}
	if !strings.Contains(string(data), "# 发行包自带") {
		t.Error("登记把目录的注释头洗掉了：注册只许追加")
	}
}

func catalogHasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
