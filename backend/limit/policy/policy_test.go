package policy

import (
	"testing"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// TestGroupTableShape 把**路由组表本体**钉住：组名、位、默认动作、顺序。
//
// 为什么这条必须存在：这张表就是安全边界（谁能路由到哪一族）。它从
// seelebridge/tools 归位时逐字节未改（见 m2-record.md 的 M2-C4 证据），
// 之后任何"顺手加一组/改一个位"都必须在用例里显式表态。
//
// 顺序也是判据：路由是 LMRW（最后匹配胜出），deny 规则必须排在最后。
func TestGroupTableShape(t *testing.T) {
	type want struct {
		name    string
		mode    uint8
		action  toolspermission.Action
		firstMa string // 该组的第一条 Match 模式（顺序也是口径）
	}
	cases := []want{
		// 远端两格（2026-10-2x ssh 工作项）：按**面**判定，放在表头是有意的——路由是
		// LMRW（最后匹配胜出），ro/rw 在它们后面覆盖同名工具，因此这两组"声明了远端面
		// 工具清单，但不接管名字路由"。真实判据在路径门（按已定执行面 + 是否走出远端根）。
		{GroupSSH, BitExecute, toolspermission.ActionAllow, "read_file"},
		{GroupSSHUserspace, BitRead | BitWrite | BitExecute, toolspermission.ActionAsk, "read_file"},
		{GroupRO, BitRead, toolspermission.ActionAllow, "read_file"},
		{GroupRW, BitRead | BitWrite, toolspermission.ActionAllow, "write_file"},
		{GroupRWSession, BitRead | BitWrite, toolspermission.ActionAllow, "compact_context"},
		{GroupRWDesktop, BitRead | BitWrite, toolspermission.ActionAsk, "computer_click"},
		// ctl 组的默认动作是 allow：叫停/挂起/派生对**root**是放行的（员工与子代理
		// 在授权表里 ctl 位 = 0，所以它们仍然进不来）——这是迁移前的既有口径。
		{GroupCTL, BitExecute, toolspermission.ActionAllow, "task_complete"},
		{GroupADM, BitAll, toolspermission.ActionAsk, "switch_plugin"},
	}
	groups := DefaultPermissionGroupList()
	if len(groups) != len(cases) {
		t.Fatalf("路由组数 = %d，want %d（新增一组必须同时改这里说明理由）", len(groups), len(cases))
	}
	for index, expect := range cases {
		got := groups[index]
		if got.Name != expect.name {
			t.Fatalf("第 %d 组 = %q，want %q（顺序即语义）", index, got.Name, expect.name)
		}
		if got.Mode != expect.mode {
			t.Errorf("组 %s 的位 = %d，want %d", got.Name, got.Mode, expect.mode)
		}
		if got.Default != expect.action {
			t.Errorf("组 %s 的默认动作 = %q，want %q", got.Name, got.Default, expect.action)
		}
		if len(got.Match) == 0 || got.Match[0] != expect.firstMa {
			t.Errorf("组 %s 的第一条模式 = %v，want 首条 %q", got.Name, got.Match, expect.firstMa)
		}
	}
}

// TestDenyRulesFormTrailingBlock 钉住"危险命令 deny 在最末"这条顺序判据：
// 它在 ApplyTier 与框架的 LMRW 路由下都是硬拦的最后一层，因此 deny 必须构成
// **尾部连续块**——中间夹一条 deny 会让它被后面的 ask/allow 覆盖掉。
func TestDenyRulesComeLast(t *testing.T) {
	rules := DefaultPermissionRules()
	if len(rules) == 0 {
		t.Fatal("规则表为空")
	}
	if rules[len(rules)-1].Action != toolspermission.ActionDeny {
		t.Fatalf("最后一条规则 = %q（%s），want deny——deny 必须最后匹配",
			rules[len(rules)-1].Action, rules[len(rules)-1].ToolName)
	}
	first := len(rules)
	for index := len(rules) - 1; index >= 0; index-- {
		if rules[index].Action != toolspermission.ActionDeny {
			break
		}
		first = index
	}
	for index, rule := range rules[:first] {
		if rule.Action == toolspermission.ActionDeny {
			t.Errorf("第 %d 条 deny（%s）没有落在尾部 deny 段里：顺序即语义", index, rule.ToolName)
		}
	}
}

// TestTierCatalogHasSingleSource 钉住"档位目录的唯一事实在 contract/dto"：
// 本包只转发，不新增一份目录。
func TestTierCatalogHasSingleSource(t *testing.T) {
	got := DefaultPermissionTiers()
	want := dto.PermissionTiers()
	if len(got) != len(want) {
		t.Fatalf("档位目录条数 = %d，want %d（唯一事实在 dto）", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("第 %d 档 = %+v，want %+v", index, got[index], want[index])
		}
	}
}

// TestDeclaredToolMetaNeverGuesses 钉住"派生不出来就不猜"：
// 未分封的名字返回零值簇属（框架退回名字路由），不是"猜一个组"。
func TestDeclaredToolMetaNeverGuesses(t *testing.T) {
	declared := DeclaredToolMeta("read_file")
	if len(declared.Groups) != 1 || declared.Groups[0] != GroupRO {
		t.Fatalf("read_file 的簇属 = %+v，want 命中 %s", declared, GroupRO)
	}
	if declared.Kind != frameworktools.ToolKindRead {
		t.Errorf("read_file 的 Kind = %q，want read", declared.Kind)
	}
	unknown := DeclaredToolMeta("mcp__third_party__tool")
	if len(unknown.Groups) != 0 || unknown.Kind != "" || unknown.Resource != "" {
		t.Fatalf("未分封工具的簇属 = %+v，want 零值（不猜）", unknown)
	}
}
