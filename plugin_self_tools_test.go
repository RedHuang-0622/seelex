package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/seelebridge"
)

// TestPluginSelfTools 验证内置自迭代工具族（plugin_create / plugins_reload /
// plugins_list / skill_create / skills_list / mcp_create / mcp_list）注册且
// 可调用：临时 plugins 根 + 临时账号配置，无真实网络调用。
func TestPluginSelfTools(t *testing.T) {
	originalPlugins := *pluginsPaths
	originalStore := *storePath
	temp := t.TempDir()
	*pluginsPaths = filepath.Join(temp, "plugins")
	*storePath = filepath.Join(temp, "sessions")
	defer func() {
		*pluginsPaths = originalPlugins
		*storePath = originalStore
	}()

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
  agent:
    - model: test-model
      base_url: http://127.0.0.1:9/v1
      api_key: test-only-key
  goalplan:
    - model: test-model
      base_url: http://127.0.0.1:9/v1
      api_key: test-only-key
mcp_servers: []
`
	if err := os.WriteFile(accountsPath, []byte(minimalAccounts), 0o600); err != nil {
		t.Fatal(err)
	}

	// 插件根不再是"空着也能启动"的地方：零插件在启动期显式报错（main.go 的
	// requirePlugins）。这里先给临时根放一个最小插件，代表"发行包自带 plugins/"，
	// 再走自迭代闭环（plugin_create → plugins_reload 看到新增）。
	seedDir := filepath.Join(*pluginsPaths, "seed")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "plugin.md"),
		[]byte("---\nschema_version: 1\nname: seed\ndescription: seed\n---\n# Seed\n"), 0o644); err != nil {
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
	defer runtime.Shutdown()
	skills := initSkillSystem()
	plugins, _, err := initPluginSystem(runtime, skills)
	if err != nil {
		t.Fatalf("initPluginSystem: %v", err)
	}
	registerPluginSelfTools(runtime, plugins)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dispatch := func(name, args string) (string, error) {
		return runtime.Agent().DirectDispatch(ctx, name, args)
	}

	// plugin_create → 写盘（不激活）
	created, err := dispatch("plugin_create", `{"name":"smoke_plugin","description":"smoke","prompt":"# Smoke","skills":[{"name":"smoke_skill","description":"s","prompt":"do s"}]}`)
	if err != nil {
		t.Fatalf("plugin_create: %v", err)
	}
	if !strings.Contains(created, `"status":"created"`) {
		t.Fatalf("plugin_create result: %s", created)
	}
	if _, err := os.Stat(filepath.Join(*pluginsPaths, "smoke_plugin", "plugin.md")); err != nil {
		t.Fatalf("plugin.md not written: %v", err)
	}

	// plugins_reload → 新增
	report, err := dispatch("plugins_reload", `{}`)
	if err != nil {
		t.Fatalf("plugins_reload: %v", err)
	}
	var parsed struct {
		Added []string `json:"added"`
	}
	if err := json.Unmarshal([]byte(report), &parsed); err != nil {
		t.Fatalf("plugins_reload result: %v (%s)", err, report)
	}
	if len(parsed.Added) != 1 || parsed.Added[0] != "smoke_plugin" {
		t.Fatalf("plugins_reload added = %+v", parsed.Added)
	}

	// plugins_list → 可见
	list, err := dispatch("plugins_list", `{}`)
	if err != nil {
		t.Fatalf("plugins_list: %v", err)
	}
	if !strings.Contains(list, "smoke_plugin") || !strings.Contains(list, "smoke_skill") {
		t.Fatalf("plugins_list missing smoke_plugin/smoke_skill: %s", list)
	}

	// skill_create（追加第二个 skill）→ plugins_reload → Updated
	if _, err := dispatch("skill_create", `{"plugin":"smoke_plugin","name":"smoke_skill2","description":"s2","prompt":"do s2"}`); err != nil {
		t.Fatalf("skill_create: %v", err)
	}
	report, err = dispatch("plugins_reload", `{}`)
	if err != nil {
		t.Fatalf("plugins_reload after skill_create: %v", err)
	}
	var updated struct {
		Updated []string `json:"updated"`
	}
	if err := json.Unmarshal([]byte(report), &updated); err != nil {
		t.Fatalf("reload result: %v (%s)", err, report)
	}
	if len(updated.Updated) != 1 || updated.Updated[0] != "smoke_plugin" {
		t.Fatalf("plugins_reload updated = %+v", updated.Updated)
	}

	// skills_list → 两个 skill 可见
	skillList, err := dispatch("skills_list", `{}`)
	if err != nil {
		t.Fatalf("skills_list: %v", err)
	}
	if !strings.Contains(skillList, "smoke_skill2") {
		t.Fatalf("skills_list missing smoke_skill2: %s", skillList)
	}

	// mcp_create + mcp_list（内存态登记，不连接）
	if _, err := dispatch("mcp_create", `{"name":"smoke_mcp","transport":"stdio","command":"echo","args":["hi"]}`); err != nil {
		t.Fatalf("mcp_create: %v", err)
	}
	mcpList, err := dispatch("mcp_list", `{}`)
	if err != nil {
		t.Fatalf("mcp_list: %v", err)
	}
	if !strings.Contains(mcpList, "smoke_mcp") {
		t.Fatalf("mcp_list missing smoke_mcp: %s", mcpList)
	}
}
