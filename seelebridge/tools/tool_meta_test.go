package tools

import (
	"testing"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
)

// DeclaredToolMeta 是打点 K-0 的**声明路径**：工具名 → 簇属，事实源是路由组表。
// 这组用例钉住三件事：
//   - 组名到簇属类别（Kind）的翻译口径（read/write/control/admin）；
//   - 未分封的名字**不猜**（返零值 ⇒ 判定退回名字路由，与 K-0 之前逐字一致）；
//   - 声明的 Groups 恰好是名字路由到的那个组（推导与路由不可能漂移）。
func TestDeclaredToolMetaFollowsRouteTable(t *testing.T) {
	cases := []struct {
		tool     string
		wantKind frameworktools.ToolKind
		want     string // 期望的组名；空 = 期望"未分封"
	}{
		{"read_file", frameworktools.ToolKindRead, GroupRO},
		{"grep_search", frameworktools.ToolKindRead, GroupRO},
		{"write_file", frameworktools.ToolKindWrite, GroupRW},
		{"bash", frameworktools.ToolKindWrite, GroupRW},
		{"compact_context", frameworktools.ToolKindWrite, GroupRWSession},
		{"computer_click", frameworktools.ToolKindWrite, GroupRWDesktop},
		{"task_complete", frameworktools.ToolKindControl, GroupCTL},
		{"fork_subagents", frameworktools.ToolKindControl, GroupCTL},
		{"switch_plugin", frameworktools.ToolKindAdmin, GroupADM},
		{"mcp_custom_tool", "", ""}, // 动态第三方工具：不猜
	}
	for _, tc := range cases {
		meta := DeclaredToolMeta(tc.tool)
		if tc.want == "" {
			if len(meta.Groups) != 0 || meta.Kind != "" {
				t.Errorf("%s 未分封应返零值簇属，得到 %+v", tc.tool, meta)
			}
			continue
		}
		if len(meta.Groups) != 1 || meta.Groups[0] != tc.want {
			t.Errorf("%s 声明簇 = %v, want [%s]", tc.tool, meta.Groups, tc.want)
		}
		if meta.Kind != tc.wantKind {
			t.Errorf("%s 声明类别 = %q, want %q", tc.tool, meta.Kind, tc.wantKind)
		}
		// 声明与名字路由必须同源：Groups[0] 就是 RoutePermissionGroup 的结果。
		group, routed := RoutePermissionGroup(DefaultPermissionGroupList(), tc.tool)
		if !routed || group.Name != meta.Groups[0] {
			t.Errorf("%s 声明簇 %v 与名字路由 %q(routed=%v) 不一致", tc.tool, meta.Groups, group.Name, routed)
		}
	}
}

// TestDeclaredToolMetaKeepsBitsZero 钉住"所需位只有一个事实源"：声明的 Bits 必须
// 留 0，让 checker 取组的 Mode。填一个数字进 Bits 就是第二个事实源——组 Mode 改了
// 而 Bits 没改，判定就与路由组表脱节，且没有任何报错面。
func TestDeclaredToolMetaKeepsBitsZero(t *testing.T) {
	for _, name := range []string{"read_file", "write_file", "bash", "switch_plugin", "task_complete"} {
		if meta := DeclaredToolMeta(name); meta.Bits != 0 {
			t.Errorf("%s 的声明填了 Bits=%d：所需位必须由组 Mode 派生（单一事实源）", name, meta.Bits)
		}
	}
}

// TestSanitizeMetaDropsUnknownGroups 是 K-0 的**安全侧**守卫用例。
//
// 场景：用户用 seelex.yaml 的 permission.groups 换掉了默认组表
// （mergePermissionConfig 是替换语义），于是工具声明的簇名在本次配置里不存在。
// 此时若把声明原样交给 checker，DecideForMeta 查不到组 ⇒ 所需位退化成 0 ⇒
// **放行**（一次静默放权）。sanitizeMeta 必须把不存在的簇摘掉，让判定退回按名字
// 路由（未分封 ⇒ 默认 ask，也就是 K-0 之前的行为）。
func TestSanitizeMetaDropsUnknownGroups(t *testing.T) {
	state := &PermissionGate{}
	state.Set(DefaultPermissionConfig(), nil)

	declared := frameworktools.ToolMeta{Kind: frameworktools.ToolKindWrite, Groups: []string{GroupRW}}
	if got := state.sanitizeMeta(declared); len(got.Groups) != 1 || got.Groups[0] != GroupRW {
		t.Fatalf("簇在配置里存在时不该被摘掉：%+v", got)
	}

	// 换一份"只有 ro 组"的配置（模拟用户的 permission.groups 替换）。
	narrowed := DefaultPermissionConfig()
	narrowed.Groups = []toolspermission.PermissionGroup{{
		Name: GroupRO, Mode: bitRead, Default: toolspermission.ActionAllow,
		Match: []string{"read_file"},
	}}
	state.Set(narrowed, nil)

	got := state.sanitizeMeta(declared)
	if len(got.Groups) != 0 || got.Bits != 0 {
		t.Fatalf("配置里没有 rw 组时必须摘掉该声明，得到 %+v", got)
	}
	if got.Kind != frameworktools.ToolKindWrite {
		t.Fatalf("摘掉簇不应连 Kind 一起丢：%+v", got)
	}
}
