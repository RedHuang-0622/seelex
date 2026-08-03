package seelebridge

import (
	"context"
	"errors"
	"testing"

	"github.com/RedHuang-0622/Seele/agent/bridge"
)

// 注册一个普通产品工具，供可见性断言使用。
func registerNamedTool(t *testing.T, r *Runtime, name string) {
	t.Helper()
	r.RegisterTool(name, "tool "+name, map[string]interface{}{"type": "object"},
		func(context.Context, string) (string, error) { return "ok", nil })
}

// TestPluginSwitchFiltersVisibleTools 验证插件切换后 VisibleTools 按当前插件的
// include/exclude 快照过滤（切片 3：插件可见性策略）。
func TestPluginSwitchFiltersVisibleTools(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()

	for _, name := range []string{"cad_draw", "cad_export", "web_search", "web_fetch", "git_status"} {
		registerNamedTool(t, r, name)
	}
	if err := r.DefinePlugin("cad", "CAD", []string{"cad_*"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.DefinePlugin("web", "Web", []string{"web_*"}, nil); err != nil {
		t.Fatal(err)
	}

	// 未激活任何插件：全部工具可见。
	if got := toolNames(r.VisibleTools(context.Background())); len(got) != 5 {
		t.Fatalf("before activation expected 5 tools, got %v", got)
	}

	// 激活 cad：只剩 cad_* 族。
	if err := r.ActivatePlugin("cad"); err != nil {
		t.Fatal(err)
	}
	if got := toolNames(r.VisibleTools(context.Background())); !equalNames(got, "cad_draw", "cad_export") {
		t.Fatalf("cad plugin visible set = %v", got)
	}

	// 切换到 web：可见集变为 web_* 族。
	if err := r.ActivatePlugin("web"); err != nil {
		t.Fatal(err)
	}
	if got := toolNames(r.VisibleTools(context.Background())); !equalNames(got, "web_search", "web_fetch") {
		t.Fatalf("web plugin visible set = %v", got)
	}

	// 停用插件：恢复全部可见。
	r.DeactivatePlugin()
	if got := toolNames(r.VisibleTools(context.Background())); len(got) != 5 {
		t.Fatalf("after deactivate expected 5 tools, got %v", got)
	}
}

// TestPluginExcludeFiltersVisibleTools 验证 exclude 快照：被排除的工具不可见，
// 未列入 include 的工具保持可见。
func TestPluginExcludeFiltersVisibleTools(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()

	for _, name := range []string{"read_file", "write_file", "web_search"} {
		registerNamedTool(t, r, name)
	}
	if err := r.DefinePlugin("readonly", "Read Only", nil, []string{"write_file", "web_*"}); err != nil {
		t.Fatal(err)
	}
	if err := r.ActivatePlugin("readonly"); err != nil {
		t.Fatal(err)
	}
	if got := toolNames(r.VisibleTools(context.Background())); !equalNames(got, "read_file") {
		t.Fatalf("readonly plugin visible set = %v", got)
	}
}

// TestHiddenToolDispatchRejected 验证 Registry Dispatch 侧复核同一可见性策略：
// 隐藏工具被拒绝（ErrToolNotVisible 语义），可见工具正常调度。
func TestHiddenToolDispatchRejected(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()

	registerNamedTool(t, r, "allowed_tool")
	registerNamedTool(t, r, "hidden_tool")
	if err := r.DefinePlugin("limited", "Limited", []string{"allowed_*"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.ActivatePlugin("limited"); err != nil {
		t.Fatal(err)
	}

	// 可见工具可以正常分发。
	out, err := r.agentDispatch(context.Background(), "allowed_tool", "{}")
	if err != nil {
		t.Fatalf("visible tool dispatch failed: %v", err)
	}
	if out != "ok" {
		t.Fatalf("visible tool output = %q", out)
	}

	// 隐藏工具必须被拒绝（agent/bridge.ErrToolNotVisible）。
	_, err = r.agentDispatch(context.Background(), "hidden_tool", "{}")
	if err == nil {
		t.Fatal("hidden tool dispatch should be rejected")
	}
	if !errors.Is(err, bridge.ErrToolNotVisible) {
		t.Fatalf("hidden tool error = %v, want ErrToolNotVisible", err)
	}
}

func toolNames(tools []Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

func equalNames(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]bool, len(got))
	for _, name := range got {
		seen[name] = true
	}
	for _, name := range want {
		if !seen[name] {
			return false
		}
	}
	return true
}
