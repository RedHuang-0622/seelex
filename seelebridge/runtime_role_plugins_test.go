package seelebridge

// runtime_role_plugins_test.go — 钉住「teammate 按指定 plugin 装配」的四条底线：
//
//   1. **空集与今天逐字一致**：不声明 plugins 时，工具面 == 宿主全局插件面（同一个
//      早期返回），技能目录不注入（system 字节不变）；
//   2. **按会话隔离**：两个 teammate 的装配集合不同 ⇒ 工具面不同，互不串味，且主
//      代理的全局激活态不受影响（root 路径另走一条）；
//   3. **失灵但不放宽**：集合引用了本进程未定义的插件（声明后被 root 撤销）⇒ 空工具
//      面（显式失灵），绝不静默退回宿主面（那等于丢掉收窄 = 放宽）；
//   4. **回执/审计带读数**：目录字节、token 估算、空集写成 inherit-host。

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelebridge/tools"
	"github.com/RedHuang-0622/seelex/sessionstore"
	"github.com/RedHuang-0622/seelex/skill"
)

func pluginFaceToolsOf(names ...string) []types.Tool {
	built := make([]types.Tool, 0, len(names))
	for _, name := range names {
		built = append(built, types.Tool{Type: "function", Function: types.ToolFunction{Name: name}})
	}
	return built
}

func pluginFaceNamesOf(built []types.Tool) []string {
	names := make([]string, 0, len(built))
	for _, tool := range built {
		names = append(names, tool.Function.Name)
	}
	return names
}

func pluginFaceSameNames(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

// newPluginAssemblyRuntime 是装配探针基座：两个插件定义 + 四个工具名。
// cad 是**全局激活**的那个（宿主面），docs 只有按会话装配才能拿到。
func newPluginAssemblyRuntime(t *testing.T) *Runtime {
	t.Helper()
	runtime := newTestRuntime(t)
	for _, name := range []string{"cad_draw", "doc_read", "write_file", "get_time"} {
		name := name
		runtime.RegisterTool(name, name, map[string]interface{}{"type": "object"},
			func(context.Context, string) (string, error) { return "ok", nil })
	}
	if err := runtime.DefinePlugin("cad", "CAD", []string{"cad_*"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DefinePlugin("docs", "Docs", []string{"doc_*"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := runtime.ActivatePlugin("cad"); err != nil {
		t.Fatal(err)
	}
	return runtime
}

// TestPluginAssemblyEmptySetMatchesHostFace 是**回归断言**（契约 4）：空集时新收口必须
// 与旧收口（只接 PluginFilter = r.plugins.Filter）给出同一个工具面——不是"等价实现"，
// 是同一条路。
func TestPluginAssemblyEmptySetMatchesHostFace(t *testing.T) {
	runtime := newPluginAssemblyRuntime(t)
	defer runtime.Shutdown()
	all := pluginFaceToolsOf("cad_draw", "doc_read", "write_file", "get_time")

	hostPolicy := tools.NewPolicy(tools.PolicyDeps{PluginFilter: runtime.plugins.Filter})
	want := pluginFaceNamesOf(hostPolicy.Filter(context.Background(), all))
	got := pluginFaceNamesOf(runtime.visibilityPolicy.Filter(context.Background(), all))
	if !pluginFaceSameNames(got, want) {
		t.Fatalf("空集必须逐字沿用宿主的插件收口：got %v want %v", got, want)
	}
	if !pluginFaceSameNames(want, []string{"cad_draw"}) {
		t.Fatalf("基座前提不成立（全局激活 cad，include cad_*）：%v", want)
	}
	// 空集的技能目录不注入；system 字节因此逐字不变。
	if catalog := runtime.roleSkillCatalog(nil); catalog != "" {
		t.Fatalf("空集不得注入技能目录，得 %q", catalog)
	}
	if catalog := runtime.roleSkillCatalog([]string{}); catalog != "" {
		t.Fatalf("空切片同义，得 %q", catalog)
	}
	prompt := runtime.roleTurnSystemPrompt("exec")
	if strings.Contains(prompt, "## Available Skills") {
		t.Fatalf("没有装配就没有目录段：%q", prompt)
	}
}

// TestPluginAssemblyIsolatedPerTurnContext：同一进程里两个 teammate 装不同插件不得串
// 味（含并发回合：集合按构造进 ctx，任何句柄上都没有缓存的"当前装配"）。
func TestPluginAssemblyIsolatedPerTurnContext(t *testing.T) {
	runtime := newPluginAssemblyRuntime(t)
	defer runtime.Shutdown()
	all := pluginFaceToolsOf("cad_draw", "doc_read", "write_file", "get_time")

	ctxCad := tools.WithRolePlugins(context.Background(), []string{"cad"})
	ctxDocs := tools.WithRolePlugins(context.Background(), []string{"docs"})
	gotCad := pluginFaceNamesOf(runtime.visibilityPolicy.Filter(ctxCad, all))
	gotDocs := pluginFaceNamesOf(runtime.visibilityPolicy.Filter(ctxDocs, all))
	if !pluginFaceSameNames(gotCad, []string{"cad_draw"}) {
		t.Fatalf("装配 cad 的 teammate 得 %v", gotCad)
	}
	if !pluginFaceSameNames(gotDocs, []string{"doc_read"}) {
		t.Fatalf("装配 docs 的 teammate 得 %v（不得受全局激活 cad 影响）", gotDocs)
	}
	// 重复求值稳定（无缓存漂移），且主代理的全局面不变。
	again := pluginFaceNamesOf(runtime.visibilityPolicy.Filter(ctxDocs, all))
	if !pluginFaceSameNames(again, gotDocs) {
		t.Fatalf("同一 ctx 重复求值必须稳定：%v → %v", gotDocs, again)
	}
	if runtime.ActivePlugin() != "cad" {
		t.Fatalf("teammate 的装配不得改全局激活态，得 %q", runtime.ActivePlugin())
	}
	if root := pluginFaceNamesOf(runtime.visibilityPolicy.Filter(context.Background(), all)); !pluginFaceSameNames(root, []string{"cad_draw"}) {
		t.Fatalf("主代理面必须仍是宿主全局装配：%v", root)
	}
}

// TestPluginAssemblyUndefinedNameFailsClosed：声明过的插件之后被撤销 ⇒ 显式失灵
// （空工具面），不静默退回宿主面（那是放宽）。
func TestPluginAssemblyUndefinedNameFailsClosed(t *testing.T) {
	runtime := newPluginAssemblyRuntime(t)
	defer runtime.Shutdown()
	all := pluginFaceToolsOf("cad_draw", "doc_read", "write_file", "get_time")
	ctx := tools.WithRolePlugins(context.Background(), []string{"ghost"})
	if got := runtime.visibilityPolicy.Filter(ctx, all); len(got) != 0 {
		t.Fatalf("未定义插件名必须失灵（空面），得 %v", pluginFaceNamesOf(got))
	}
}

// TestRoleSkillCatalogAndAssemblyReadings：目录段按装配集合取（name+description），
// 读数（字节 / token 估算 / 空集写成 inherit-host）随回执给出。
func TestRoleSkillCatalogAndAssemblyReadings(t *testing.T) {
	runtime := newPluginAssemblyRuntime(t)
	defer runtime.Shutdown()
	registry := skill.NewRegistry()
	registry.SetPluginSkills("docs", []skill.Skill{{Name: "readme", Description: "how to read the docs"}})
	runtime.SetSkillRegistry(registry)

	catalog := runtime.roleSkillCatalog([]string{"docs"})
	if !strings.Contains(catalog, "## Available Skills") || !strings.Contains(catalog, "readme: how to read the docs") {
		t.Fatalf("目录段缺少技能行：%q", catalog)
	}
	if !strings.Contains(catalog, "cannot switch plugins") {
		t.Fatalf("员工面的目录尾句必须纠正「active plugin」的宿主口径：%q", catalog)
	}
	if strings.Contains(catalog, "cad_draw") {
		t.Fatal("目录段只该有技能名与描述")
	}
	if other := runtime.roleSkillCatalog([]string{"cad"}); other != "" {
		t.Fatalf("未发布技能的插件不得凭空造出目录段：%q", other)
	}

	views := runtime.assemblyViews([]sessionstore.TeamworkMember{
		{Role: "writer", Plugins: []string{" docs "}},
		{Role: "exec"},
	})
	if len(views) != 2 {
		t.Fatalf("逐成员读数得 %d 条", len(views))
	}
	declared := views[0]
	if declared.Mode != assemblyModeReplace || len(declared.Plugins) != 1 || declared.Plugins[0] != "docs" {
		t.Fatalf("声明面读数得 %#v", declared)
	}
	if declared.SkillCount != 1 || declared.SkillCatalogRunes != len([]rune(catalog)) {
		t.Fatalf("目录字节读数得 %#v（目录段 %d runes）", declared, len([]rune(catalog)))
	}
	if declared.SkillCatalogTokensEst != (len([]rune(catalog))+3)/4 || declared.Yellow || declared.YellowReason != "" {
		t.Fatalf("token 估算与黄牌读数得 %#v", declared)
	}
	if declared.TotalTools == 0 || declared.PluginFaceTools != 1 {
		t.Fatalf("插件面读数得 %#v（docs include doc_*，应只剩 doc_read）", declared)
	}
	inherited := views[1]
	// 空集必须在回执里**说出来**（不靠字段缺失暗示），且不注入目录。
	if inherited.Mode != assemblyModeInheritHost || len(inherited.Plugins) != 0 || inherited.SkillCatalogRunes != 0 {
		t.Fatalf("空集读数得 %#v", inherited)
	}
}

// TestRolePluginAssemblyPrefersExplicitOverRoleDefault：集合口径是"显式声明替换 →
// 角色自带 → 空集"（契约 6 的第一层）。
func TestRolePluginAssemblyPrefersExplicitOverRoleDefault(t *testing.T) {
	runtime := newPluginAssemblyRuntime(t)
	defer runtime.Shutdown()
	runtime.SetRolePluginsProvider(func(roleName string) []string {
		if roleName == "exec" {
			return []string{" docs "}
		}
		return nil
	})
	if got := runtime.rolePluginAssembly(roleRoundSpec{RoleName: "exec"}); !pluginFaceSameNames(got, []string{"docs"}) {
		t.Fatalf("没声明时落到角色自带，得 %v", got)
	}
	if got := runtime.rolePluginAssembly(roleRoundSpec{RoleName: "exec", Plugins: []string{"cad"}}); !pluginFaceSameNames(got, []string{"cad"}) {
		t.Fatalf("显式声明必须替换集合（不是叠加），得 %v", got)
	}
	if got := runtime.rolePluginAssembly(roleRoundSpec{RoleName: "writer"}); got != nil {
		t.Fatalf("角色自带也为空 = 空集（不覆盖），得 %v", got)
	}
}

// TestValidateMemberPluginsRejectsUnknownAndOverCap：编排入口的两道显式拒绝（未知
// 名 / 超上限），以及"空集写回 nil"。
func TestValidateMemberPluginsRejectsUnknownAndOverCap(t *testing.T) {
	defined := map[string]bool{"cad": true, "docs": true}
	definedFn := func(name string) bool { return defined[name] }

	members := []sessionstore.TeamworkMember{{Role: "exec", Plugins: []string{" cad ", "", "docs"}}}
	if err := validateMemberPlugins(members, 3, definedFn); err != nil {
		t.Fatalf("合法装配不得被拒：%v", err)
	}
	if !pluginFaceSameNames(members[0].Plugins, []string{"cad", "docs"}) {
		t.Fatalf("规整结果必须写回计划（运行时用的就是这一份）：%v", members[0].Plugins)
	}

	// 重复 = 显式拒绝（与未知名/超限同口径）：修前这里静默去重 ⇒"唯一生效的口径"
	// 是静默合并，而计划/存储层写的是显式拒绝（对抗复核 D）。
	duplicated := []sessionstore.TeamworkMember{{Role: "exec", Plugins: []string{" cad ", "cad"}}}
	err := validateMemberPlugins(duplicated, 3, definedFn)
	if err == nil || !strings.Contains(err.Error(), "重复") {
		t.Fatalf("重复声明必须显式拒绝（不静默去重），得 %v", err)
	}

	unknown := []sessionstore.TeamworkMember{{Role: "exec", Plugins: []string{"ghost"}}}
	err = validateMemberPlugins(unknown, 3, definedFn)
	if err == nil || !strings.Contains(err.Error(), "未定义") {
		t.Fatalf("未知名必须显式拒绝，得 %v", err)
	}

	over := []sessionstore.TeamworkMember{{Role: "exec", Plugins: []string{"a", "b", "c", "d"}}}
	err = validateMemberPlugins(over, 3, definedFn)
	if err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("超上限必须显式拒绝（不静默截断），得 %v", err)
	}

	inherit := []sessionstore.TeamworkMember{{Role: "exec"}}
	if err := validateMemberPlugins(inherit, 3, definedFn); err != nil || inherit[0].Plugins != nil {
		t.Fatalf("空集 = 不覆盖（写回 nil），得 (%v, %#v)", err, inherit[0].Plugins)
	}
}

// pluginRoundProbeEngine 是钉"装配进 ctx + 目录进 system prompt"的最小假引擎。
type pluginRoundProbeEngine struct {
	id         string
	prompts    []string
	ctxPlugins []string
}

func (e *pluginRoundProbeEngine) SessionID() string { return e.id }
func (e *pluginRoundProbeEngine) SetSystemPrompt(prompt string) {
	e.prompts = append(e.prompts, prompt)
}
func (e *pluginRoundProbeEngine) SetMaxLoops(int)          {}
func (e *pluginRoundProbeEngine) ClearHistory()            {}
func (e *pluginRoundProbeEngine) History() []types.Message { return nil }
func (e *pluginRoundProbeEngine) ChatStream(ctx context.Context, _ string, _ func(string)) (string, error) {
	e.ctxPlugins = tools.RolePluginsFromContext(ctx)
	return "done", nil
}

// TestRunRoleRoundAssemblesPlugins：装配真的落到角色回合上——集合进本轮 ctx（工具面
// 每轮现算），目录进系统提示；空集那一轮 system 字节逐字不变。
func TestRunRoleRoundAssemblesPlugins(t *testing.T) {
	runtime := newPluginAssemblyRuntime(t)
	defer runtime.Shutdown()
	registry := skill.NewRegistry()
	registry.SetPluginSkills("docs", []skill.Skill{{Name: "readme", Description: "docs"}})
	runtime.SetSkillRegistry(registry)

	assembled := &pluginRoundProbeEngine{id: "role-assembled"}
	plain := &pluginRoundProbeEngine{id: "role-plain"}
	runtime.SetRoleEngineFactory(func(sessionID string) (roleEngine, error) {
		if sessionID == assembled.id {
			return assembled, nil
		}
		return plain, nil
	})

	if _, err := runtime.runRoleRound(context.Background(), roleRoundSpec{
		MainSessionID: "main-1", RoleName: "exec", RoleSessionID: assembled.id,
		SystemPrompt: "ROLE PROMPT", Plugins: []string{"docs"}, Input: "go",
	}); err != nil {
		t.Fatalf("装配回合失败：%v", err)
	}
	if !pluginFaceSameNames(assembled.ctxPlugins, []string{"docs"}) {
		t.Fatalf("装配集合必须进本轮 ctx，得 %v", assembled.ctxPlugins)
	}
	if len(assembled.prompts) == 0 || !strings.Contains(assembled.prompts[len(assembled.prompts)-1], "readme") {
		t.Fatalf("技能目录必须进系统提示：%#v", assembled.prompts)
	}

	if _, err := runtime.runRoleRound(context.Background(), roleRoundSpec{
		MainSessionID: "main-1", RoleName: "exec", RoleSessionID: plain.id,
		SystemPrompt: "ROLE PROMPT", Input: "go",
	}); err != nil {
		t.Fatalf("空集回合失败：%v", err)
	}
	if plain.ctxPlugins != nil {
		t.Fatalf("空集不得往 ctx 里放集合，得 %v", plain.ctxPlugins)
	}
	if len(plain.prompts) == 0 {
		t.Fatal("回合必须设置系统提示")
	}
	// 提示在建会话时与每轮起手各设一次（同值重设幂等）；空集时**两次都**是原始
	// 字节——这才是"空集与今天逐字一致"的意思。
	for _, prompt := range plain.prompts {
		if prompt != "ROLE PROMPT" {
			t.Fatalf("空集的 system 字节必须逐字不变，得 %#v", plain.prompts)
		}
	}
}
