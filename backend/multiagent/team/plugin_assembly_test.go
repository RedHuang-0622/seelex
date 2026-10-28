package team

// plugin_assembly_test.go 是运行时插件装配判据的**域级**用例（P21-18：随实现从
// `backend/engine/assemble/runtime_role_plugins.go` 的判据搬进本域；装配层里那两条纯函数用例
// 仍走同身份别名，属宿主级覆盖）。

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/backend/engine/plugin/visibility"
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
)

// TestResolvePluginAssemblyPrefersExplicitDeclaration：三层口径——显式声明**替换**集合，
// 没声明才落到角色自带，两者都空 = 空集（不覆盖）。
func TestResolvePluginAssemblyPrefersExplicitDeclaration(t *testing.T) {
	if got := ResolvePluginAssembly([]string{" cad ", "", "docs"}, []string{"self"}); len(got) != 2 ||
		got[0] != "cad" || got[1] != "docs" {
		t.Fatalf("显式声明必须替换角色自带（并做读侧规整）：%#v", got)
	}
	if got := ResolvePluginAssembly(nil, []string{"self"}); len(got) != 1 || got[0] != "self" {
		t.Fatalf("没声明才落到角色自带：%#v", got)
	}
	if got := ResolvePluginAssembly(nil, nil); got != nil {
		t.Fatalf("都空 = 空集（nil）：%#v", got)
	}
	// 全空白的"显式声明"等于没声明（读侧规整后为空）⇒ 落到角色自带。
	if got := ResolvePluginAssembly([]string{"  ", "\t"}, []string{"self"}); len(got) != 1 || got[0] != "self" {
		t.Fatalf("全空白的显式声明应视为没声明：%#v", got)
	}
	// 读侧去重只留一份（保序）。
	if got := ResolvePluginAssembly([]string{"a", " a ", "a"}, nil); len(got) != 1 || got[0] != "a" {
		t.Fatalf("读侧去重只留一份：%#v", got)
	}
}

// TestNormalizePluginNamesKeepsOrderAndDropsEmpties 钉住读侧规范化的形状：
// 保序去重、去空白、空清完 → nil（"没装配"与"装配了零个"是同一形态）。
func TestNormalizePluginNamesKeepsOrderAndDropsEmpties(t *testing.T) {
	got := NormalizePluginNames([]string{" b ", "a", "", "b", "  "})
	if len(got) != 2 || got[0] != "b" || got[1] != "a" {
		t.Fatalf("保序去重 + trim：%#v", got)
	}
	if NormalizePluginNames(nil) != nil || NormalizePluginNames([]string{"", "\t"}) != nil {
		t.Fatal("空输入与全空白都必须归一为 nil")
	}
}

// TestValidateMemberPluginsRejectsExplicitly 钉住**四条显式拒绝/规整**：
// 语法规整写回成员、未知名拒绝、超限拒绝、重复拒绝；空/缺失 = 不覆盖（nil）。
func TestValidateMemberPluginsRejectsExplicitly(t *testing.T) {
	defined := map[string]bool{"cad": true, "docs": true}
	definedFn := func(name string) bool { return defined[name] }

	members := []storeteamwork.Member{{Role: "exec", Plugins: []string{" cad ", "", "docs"}}}
	if err := ValidateMemberPlugins(members, 3, definedFn); err != nil {
		t.Fatalf("合法声明应通过：%v", err)
	}
	if len(members[0].Plugins) != 2 || members[0].Plugins[0] != "cad" {
		t.Fatalf("规整结果必须写回成员条目（计划里存的才是运行时真会用那份）：%#v", members[0].Plugins)
	}

	cases := map[string][]string{
		"重复声明显式拒绝": {"cad", " cad "},
		"未知名显式拒绝":  {"cad", "nope"},
		"超上限显式拒绝":  {"cad", "docs", "cad2", "docs2"},
	}
	for name, plugins := range cases {
		if err := ValidateMemberPlugins([]storeteamwork.Member{{Role: "exec", Plugins: plugins}}, 3, definedFn); err == nil {
			t.Fatalf("%s：必须报错", name)
		}
	}
	// 未知名那一支的文案要点明"显式拒绝，不静默忽略"（回执据此说人话）。
	err := ValidateMemberPlugins([]storeteamwork.Member{{Role: "exec", Plugins: []string{"nope"}}}, 3, definedFn)
	if err == nil || !strings.Contains(err.Error(), "未定义") {
		t.Fatalf("未知名必须点名：%v", err)
	}

	inherit := []storeteamwork.Member{{Role: "exec"}}
	if err := ValidateMemberPlugins(inherit, 3, definedFn); err != nil || inherit[0].Plugins != nil {
		t.Fatalf("空/缺失 = 不覆盖（nil）：%v %#v", err, inherit[0].Plugins)
	}
	// 没有目录判据（defined = nil）时只做语法与数量校验。
	if err := ValidateMemberPlugins([]storeteamwork.Member{{Role: "exec", Plugins: []string{"who"}}}, 3, nil); err != nil {
		t.Fatalf("没有目录面时不该做名字校验：%v", err)
	}
}

// TestPluginFaceJudgementThreeShapes 钉住装配面裁决的**三种形态**（覆盖全部输入，
// 且与运行事实一一对应）：定义有值 ⇒ 按它收窄；定义为空 ⇒ 一律可见；失灵 ⇒ 一个都不给。
func TestPluginFaceJudgementThreeShapes(t *testing.T) {
	tools := []types.Tool{
		{Function: types.ToolFunction{Name: "read_file"}},
		{Function: types.ToolFunction{Name: "bash"}},
	}
	defs := []visibility.Def{{Include: []string{"read_file"}}}

	narrowed := PluginFaceJudgement{Defs: defs}
	if !narrowed.Visible("read_file") || narrowed.Visible("bash") {
		t.Fatal("有定义时按定义收窄（读数面）")
	}
	if face := narrowed.Face(tools); len(face) != 1 || face[0].Function.Name != "read_file" {
		t.Fatalf("有定义时按定义收窄（运行面）：%+v", face)
	}
	if narrowed.FaultNote([]string{"cad"}) != "" {
		t.Fatal("没失灵就没有失灵说明")
	}

	inherit := PluginFaceJudgement{}
	if !inherit.Visible("bash") || len(inherit.Face(tools)) != 2 {
		t.Fatal("空定义 = 一律可见（不改变工具面）")
	}

	faulted := PluginFaceJudgement{Missing: []string{"cad"}, Faulted: true}
	if faulted.Visible("read_file") {
		t.Fatal("失灵 ⇒ 读数面一个都不给（不静默放宽为宿主面）")
	}
	if face := faulted.Face(tools); len(face) != 0 {
		t.Fatalf("失灵 ⇒ 运行面为空：%+v", face)
	}
	note := faulted.FaultNote([]string{"cad", "docs"})
	if !strings.Contains(note, "cad, docs") || !strings.Contains(note, "失灵") {
		t.Fatalf("失灵说明必须点明声明集合与原因：%q", note)
	}
}
