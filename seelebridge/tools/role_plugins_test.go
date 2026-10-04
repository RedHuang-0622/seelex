package tools

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/seelebridge/plugin"
)

func faceToolsOf(names ...string) []types.Tool {
	tools := make([]types.Tool, 0, len(names))
	for _, name := range names {
		tools = append(tools, types.Tool{Type: "function", Function: types.ToolFunction{Name: name}})
	}
	return tools
}

func faceNamesOf(tools []types.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Function.Name)
	}
	return names
}

func faceHasName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

// TestRolePluginsContextCarrier 钉住 ctx 载体的空集语义：**没装配**与"装配了零个"
// 必须是同一个形态（都不进 ctx），否则空集那条分支会漂成"装配了空集"。
func TestRolePluginsContextCarrier(t *testing.T) {
	base := context.Background()
	if RolePluginsFromContext(base) != nil {
		t.Fatal("没装配时必须是 nil")
	}
	ctx := WithRolePlugins(base, []string{" cad ", "", "cad", "docs"})
	got := RolePluginsFromContext(ctx)
	if len(got) != 2 || got[0] != "cad" || got[1] != "docs" {
		t.Fatalf("去空 + 保序去重后得 %v", got)
	}
	if got := WithRolePlugins(base, []string{" ", ""}); got != base {
		t.Fatal("清完为空 = 不装配，不得往 ctx 里塞一个空集合")
	}
	if got := WithRolePlugins(base, nil); got != base {
		t.Fatal("nil 输入应原样返回 ctx")
	}
	if RolePluginsFromContext(nil) != nil {
		t.Fatal("nil ctx 必须安全返回 nil")
	}
}

// TestPolicyPluginFaceTakesPrecedenceOverHostFilter：按会话装配（PluginFace）优先
// 于宿主全局装配（PluginFilter）；只接了后者时行为逐字不变（旧闭包语义不变）。
func TestPolicyPluginFaceTakesPrecedenceOverHostFilter(t *testing.T) {
	all := faceToolsOf("read_file", "write_file", "get_time")
	hostOnly := NewPolicy(PolicyDeps{PluginFilter: func(tools []types.Tool) []types.Tool {
		return tools[:1]
	}})
	if got := faceNamesOf(hostOnly.Filter(context.Background(), all)); len(got) != 1 || got[0] != "read_file" {
		t.Fatalf("只接宿主收口时行为必须不变，得 %v", got)
	}
	both := NewPolicy(PolicyDeps{
		PluginFilter: func(tools []types.Tool) []types.Tool { return tools[:1] },
		PluginFace:   func(_ context.Context, tools []types.Tool) []types.Tool { return tools[2:] },
	})
	got := faceNamesOf(both.Filter(context.Background(), all))
	if len(got) != 1 || got[0] != "get_time" {
		t.Fatalf("按会话收口必须优先，得 %v", got)
	}
}

// TestPolicyPluginFaceCannotWidenPermissionFace 钉住"权限不随插件走"：插件面是**最后
// 一道**，只能在权限面算完之后继续做减法——插件全都接纳（等于不收窄）也不能把权限面
// 已经挡掉的工具变回来。
func TestPolicyPluginFaceCannotWidenPermissionFace(t *testing.T) {
	all := faceToolsOf("read_file", "write_file", "cad_draw")
	policy := NewPolicy(PolicyDeps{
		ToolFace: func(_ context.Context, name string) bool { return name != "write_file" },
		PluginFace: func(_ context.Context, tools []types.Tool) []types.Tool {
			// 一个"什么都要"的插件定义：没有 include = 不设准入，没有 exclude。
			return plugin.Face([]plugin.Def{{Name: "wide"}}, tools)
		},
	})
	got := faceNamesOf(policy.Filter(context.Background(), all))
	if faceHasName(got, "write_file") {
		t.Fatalf("权限面挡掉的工具不得被插件面放回来（位缺不是能力问题），得 %v", got)
	}
	if !faceHasName(got, "cad_draw") || !faceHasName(got, "read_file") {
		t.Fatalf("权限面允许的工具不应被无收窄的插件面去掉，得 %v", got)
	}
	// 反过来：插件收窄只做减法。
	narrow := NewPolicy(PolicyDeps{
		PluginFace: func(_ context.Context, tools []types.Tool) []types.Tool {
			return plugin.Face([]plugin.Def{{Name: "cad", Include: []string{"cad_*"}}}, tools)
		},
	})
	if got := faceNamesOf(narrow.Filter(context.Background(), all)); len(got) != 1 || got[0] != "cad_draw" {
		t.Fatalf("插件收窄得 %v", got)
	}
}
