package seelebridge

// assembly_chain_real_partition_test.go — 链路用例（**当前真划分**版）。
//
// 与 assembly_chain_test.go 的分工：那份用 `cad`/`docs`/`ops` 三个**合成**插件钉链路形状
// （装配生效 / 不串味 / 空集回归 / 显式拒绝 / 闸门读数）；本文件把同一条链路接到**发行包
// 里真实存在的插件划分**上跑——`plugins/` 根下的 `default` / `hardware` / `impeccable`
// 三份 manifest 由生产加载器（`plugin.NewLoader` + `plugin.Manager.Load`）读进来，
// 而不是测试自己 `DefinePlugin` 造出来的。
//
// 链路（任一跳断掉都表现为"派发时带了、执行时丢了"）：
//
//	members[].plugins → roleRoundSpec.Plugins → rolePluginAssembly（显式声明 > 角色自带 > 空集）
//	  → 本轮 ctx（seeltools.WithRolePlugins）→ Policy.Filter 的最后一道 PluginFace
//	  → 工具可见面；同一集合另渲染技能目录段追加进 system prompt；读数进 team_plan 回执。
//
// 本文件钉四件事（每件都写了"排掉了什么错"）：
//
//	1. 装配 `hardware`：真 include 名单**真的收窄**工具面（名单外的探针工具被硬拆）+ 7 份
//	   cad-* 技能进目录段。排掉"装配只进 ctx、没落到工具面"与"目录段接的是宿主技能"。
//	2. 装配 `impeccable`：include/exclude 皆空 ⇒ 工具面**与不装配时逐元素相同**，能力差
//	   体现在技能目录（1 份 impeccable）。排掉"以为装配等于收窄"这种误读，也排掉
//	   "所有插件的工具面差异都被忽略"这种退化实现。
//	3. **不装配 ⇒ 默认 default**：空集时工具面 = 宿主当前装配 = 启动基线 `default` 的面
//	   （与显式声明 `["default"]` 逐元素相等），system 字节逐字不变；同时把**刻意的不对称**
//	   写清楚：工具面同 default，技能目录不注入（default 那 11 份技能不进员工 prompt）。
//	4. 回执与划分三方一致：真根的 `curated.yaml` entries、运行时已定义集合、回执的逐成员
//	   读数（含 inherit-host 成员）对同一份划分给出同一组数字；真划分上的错别字仍显式拒绝
//	   且不落盘。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/plugin"
	seeletelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/skill"
)

// ── 当前划分（三方同一份事实）───────────────────────────────────────────────

// realPartitionRoot 是发行包自带的插件根：本包测试的 CWD 是 `seelebridge/`，所以这一跳
// 就是 `plugins/`（生产那条责任链的最后一跳，见 plugins/README.md）。
const realPartitionRoot = "../plugins"

// realPartitionNames 是当前划分（三份插件）。取值不是写死的期望，而是要被**交叉核对**的
// 对象：`plugins/curated.yaml` 的 entries、运行时已定义集合都必须与它逐元素一致。
var realPartitionNames = []string{"default", "hardware", "impeccable"}

// realPartitionToolNames 是一组与真划分相干的探针工具：hardware 的 include 字面量
// （plugins/hardware/plugin.md: switch_plugin / switch_mode / get_time / read_file /
// grep_search / glob / write* / edit* / bash）在名单**内外**都有样本，收窄才可判别
// ——`bash` 是精确匹配，所以 `bash_bg` / `bash_read` 必须被硬拆掉。
var realPartitionToolNames = []string{
	"switch_plugin", "switch_mode", "get_time", "read_file", "grep_search", "glob",
	"write_file", "edit_file", "bash",
	"bash_bg", "bash_read", "web_search", "computer_screenshot", "team_plan", "job_manage",
}

// realPartitionHardwareFace 是上面那组探针里 hardware 的 include 命中的那些（按注册顺序）。
var realPartitionHardwareFace = []string{
	"switch_plugin", "switch_mode", "get_time", "read_file", "grep_search", "glob",
	"write_file", "edit_file", "bash",
}

// realPartitionDefaultSkills 是 default 的 11 份技能（plugins_list 的活体读数）。
var realPartitionDefaultSkills = []string{
	"cli-design", "code", "code-aesthetics", "goal", "plan", "plan-design",
	"plan-efficiency", "plan-norm", "review", "teamwork", "test",
}

// realPartitionHardwareSkills 是 hardware 的 7 份技能。
var realPartitionHardwareSkills = []string{
	"cad-batch", "cad-boolean", "cad-core", "cad-fillet", "cad-inspect", "cad-repair", "cad-template",
}

// ── 用例 1：装 hardware ⇒ 真 include 收窄工具面 + cad-* 进目录 ────────────────

func TestAssemblyChainRealPartitionHardwareNarrowsToolFaceAndAddsSkills(t *testing.T) {
	fixture := newRealPartitionFixture(t)
	defer fixture.runtime.Shutdown()
	hostFace := realPartitionHostFace(t, fixture.runtime)

	probe := fixture.newProbe("role-real-cad")
	snapshot := fixture.runRound(t, probe, "cad-eng", []string{"hardware"}, "ROLE PROMPT")

	// ① 集合进了本轮 ctx（跳链第一跳：派发参数没被吞）。
	if !assemblyChainSameStrings(snapshot.ctxPlugins, []string{"hardware"}) {
		t.Fatalf("装配集合没进本轮 ctx：%v", snapshot.ctxPlugins)
	}
	// ② 工具面 = 真 include 名单 ∩ 全量（名单外的探针工具必须被硬拆）。
	if !assemblyChainSameStrings(snapshot.visible, realPartitionHardwareFace) {
		t.Fatalf("装 hardware 的 teammate 可见工具 = %v\nwant（hardware/plugin.md 的 include 字面量）%v\n"+
			"host=%v", snapshot.visible, realPartitionHardwareFace, hostFace)
	}
	// ③ 判别力：收窄必须真的发生（否则 ② 只是"两面都是全量"式的空断言）。
	if len(snapshot.visible) >= len(hostFace) {
		t.Fatalf("hardware 的 include 没生效：装配面 %v 不窄于宿主面 %v", snapshot.visible, hostFace)
	}
	for _, name := range []string{"bash_bg", "bash_read", "web_search", "computer_screenshot", "team_plan", "job_manage"} {
		if assemblyChainContains(snapshot.visible, name) {
			t.Fatalf("include 名单外的 %q 不该可见：%v", name, snapshot.visible)
		}
	}

	// ④ 技能目录：hardware 的 7 份一条不少、default 的那些一条不来。
	prompt := snapshot.lastPrompt()
	catalog := fixture.runtime.roleSkillCatalog([]string{"hardware"})
	if catalog == "" || !strings.Contains(prompt, "## Available Skills") {
		t.Fatalf("装配的目录段必须进 system prompt：%q", prompt)
	}
	for _, name := range realPartitionHardwareSkills {
		if !strings.Contains(catalog, name) {
			t.Fatalf("hardware 的技能 %q 没进目录段：%q", name, catalog)
		}
	}
	for _, sentinel := range []string{"code-aesthetics", "cli-design", "teamwork"} {
		if strings.Contains(catalog, sentinel) {
			t.Fatalf("default 的技能 %q 混进了 hardware 的目录段（目录按装配集合渲染，不读全局激活态）：%q",
				sentinel, catalog)
		}
	}
	// 目录段尾部那一行纠正（员工面不能切插件/激活技能）必须随装配一起到场。
	if !strings.Contains(prompt, "cannot switch plugins") {
		t.Fatalf("目录段缺少员工面的口径纠正：%q", prompt)
	}
	// ⑤ 装配不许动全局激活态（root 路径另有其人）。
	if got := fixture.runtime.ActivePlugin(); got != "default" {
		t.Fatalf("员工装配不得改全局激活态：%q", got)
	}
}

// ── 用例 2：装 impeccable ⇒ 工具面不变、能力面变 ─────────────────────────────

func TestAssemblyChainRealPartitionImpeccableKeepsToolFaceAddsOneSkill(t *testing.T) {
	fixture := newRealPartitionFixture(t)
	defer fixture.runtime.Shutdown()
	hostFace := realPartitionHostFace(t, fixture.runtime)

	probe := fixture.newProbe("role-real-ui")
	snapshot := fixture.runRound(t, probe, "ui-eng", []string{"impeccable"}, "ROLE PROMPT")

	if !assemblyChainSameStrings(snapshot.ctxPlugins, []string{"impeccable"}) {
		t.Fatalf("装配集合没进本轮 ctx：%v", snapshot.ctxPlugins)
	}
	// include/exclude 皆空（plugins/impeccable/plugin.md）⇒ 工具面与宿主面**逐元素相同**。
	if !assemblyChainSameStrings(snapshot.visible, hostFace) {
		t.Fatalf("impeccable 不裁剪工具面，应与宿主面逐元素相同：\n got %v\nwant %v", snapshot.visible, hostFace)
	}
	if assemblyChainSameStrings(snapshot.visible, realPartitionHardwareFace) {
		t.Fatal("两面相等说明夹具退化（host 面本身就等于 hardware 面），这条用例没有判别力")
	}
	// 能力面确实变了：目录段恰好是 impeccable 那一份（default 的 11 份不进来）。
	catalog := fixture.runtime.roleSkillCatalog([]string{"impeccable"})
	if catalog == "" || !strings.Contains(catalog, "impeccable") {
		t.Fatalf("装 impeccable 必须拿到它自己的技能目录：%q", catalog)
	}
	if strings.Contains(snapshot.lastPrompt(), "code-aesthetics") {
		t.Fatalf("default 的技能不该出现在 impeccable 的目录段：%q", snapshot.lastPrompt())
	}
	// 同一事实的另一半：工具面相同、目录段不同 ⇒ 两者不是同一个装配。
	if snapshot.lastPrompt() == appendSkillCatalog("ROLE PROMPT", "") {
		t.Fatal("装配 impeccable 的 prompt 与不装配逐字相同 ⇒ 目录段没接上")
	}
}

// ── 用例 3：不装配 ⇒ 默认 default（并把不对称写清楚）───────────────────────

func TestAssemblyChainRealPartitionEmptyMeansDefaultBaseline(t *testing.T) {
	fixture := newRealPartitionFixture(t)
	defer fixture.runtime.Shutdown()

	// 前提：进程的启动基线是 default（main.go 的 activateDefaultPlugin 走的就是这一跳）。
	if got := fixture.runtime.ActivePlugin(); got != "default" {
		t.Fatalf("启动基线应激活 default，得 %q", got)
	}
	hostFace := realPartitionHostFace(t, fixture.runtime)

	// ① 不装配：工具面 = 宿主当前装配 = default 的面，system 字节逐字不变。
	plain := fixture.newProbe("role-real-plain")
	prompt := fixture.runtime.roleTurnSystemPrompt("exec")
	plainSnapshot := fixture.runRound(t, plain, "exec", nil, prompt)
	if len(plainSnapshot.ctxPlugins) != 0 {
		t.Fatalf("不装配就不该有装配集合进 ctx：%v", plainSnapshot.ctxPlugins)
	}
	if !assemblyChainSameStrings(plainSnapshot.visible, hostFace) {
		t.Fatalf("不装配的工具面应继承宿主当前装配（default）：\n got %v\nwant %v", plainSnapshot.visible, hostFace)
	}
	for index, got := range plainSnapshot.prompts {
		if got != prompt {
			t.Fatalf("不装配时 system 字节必须逐字不变（第 %d 次设置）：\n got %q\nwant %q", index, got, prompt)
		}
	}
	if catalog := fixture.runtime.roleSkillCatalog(nil); catalog != "" {
		t.Fatalf("不装配不注入技能目录，得 %q", catalog)
	}

	// ② 可判定的"默认 default"：显式声明 ["default"] 与不装配的工具面**逐元素相等**。
	explicit := fixture.newProbe("role-real-default")
	explicitSnapshot := fixture.runRound(t, explicit, "exec", []string{"default"}, prompt)
	if !assemblyChainSameStrings(explicitSnapshot.visible, plainSnapshot.visible) {
		t.Fatalf("显式装 default 与不装配的工具面应逐元素相同：\n explicit %v\n   空集 %v",
			explicitSnapshot.visible, plainSnapshot.visible)
	}
	if !assemblyChainSameStrings(explicitSnapshot.visible, hostFace) {
		t.Fatalf("default 的 include/exclude 皆空 ⇒ 面 = 全量：%v vs 宿主 %v", explicitSnapshot.visible, hostFace)
	}

	// ③ 不对称（刻意的，裁决 3）：工具面同 default，**技能目录不注入**——显式装 default 的
	//    目录段里有 default 的 11 份技能，不装配的那一份是空。这条差别必须显式钉住，
	//    否则"不装配=默认 default"会被读成"技能也继承 default"。
	defaultCatalog := fixture.runtime.roleSkillCatalog([]string{"default"})
	for _, name := range realPartitionDefaultSkills {
		if !strings.Contains(defaultCatalog, name) {
			t.Fatalf("显式装 default 的目录段应含技能 %q：%q", name, defaultCatalog)
		}
	}
	if strings.Contains(plainSnapshot.lastPrompt(), "## Available Skills") {
		t.Fatalf("不装配不该有目录段：%q", plainSnapshot.lastPrompt())
	}

	// ④ 阴性对照：真收窄的那个面与空集面**不同** ⇒ 上面几条相等不是"装配整条失效"。
	narrowed := fixture.newProbe("role-real-narrowed")
	narrowedSnapshot := fixture.runRound(t, narrowed, "cad-eng", []string{"hardware"}, prompt)
	if assemblyChainSameStrings(narrowedSnapshot.visible, plainSnapshot.visible) {
		t.Fatalf("hardware 的面与空集面相等 ⇒ 装配没生效：%v", narrowedSnapshot.visible)
	}
}

// ── 用例 4：回执读数与划分三方一致 ──────────────────────────────────────────

func TestAssemblyChainRealPartitionReceiptMatchesShippedPartition(t *testing.T) {
	fixture := newRealPartitionFixture(t)
	defer fixture.runtime.Shutdown()

	// ① 划分三方一致：curated.yaml 的 entries ↔ 运行时已定义集合 ↔ 本文件的期望。
	catalog, err := plugin.LoadCuratedFromRoot(filepath.Join("..", "plugins"))
	if err != nil {
		t.Fatalf("读发行根的精选目录：%v", err)
	}
	entries := make([]string, 0, len(catalog.Entries))
	for _, entry := range catalog.Entries {
		entries = append(entries, entry.Name)
	}
	if !assemblyChainSameStrings(entries, realPartitionNames) {
		t.Fatalf("curated.yaml 的 entries = %v，want 当前划分 %v", entries, realPartitionNames)
	}
	if defined := fixture.runtime.plugins.Names(); !assemblyChainSameStrings(defined, realPartitionNames) {
		t.Fatalf("运行时已定义插件 = %v，want 当前划分 %v", defined, realPartitionNames)
	}
	if err := plugin.ValidateCuratedCatalog(catalog, fixture.runtime.plugins.Names()); err != nil {
		t.Fatalf("目录与已落盘集合必须自洽：%v", err)
	}

	// ② 回执读数：真划分下逐成员的装配记录（含不装配那一位的继承读数）。
	store := &memPlanStore{}
	const sessionID = "s-real-partition"
	if err := fixture.runtime.SetTeamworkBackend(teamworkTestBackend(store, sessionID)); err != nil {
		t.Fatal(err)
	}
	receipt, err := fixture.runtime.teamPlanHandler(realPartitionCtx(sessionID), `{
		"team_id": "t-real-partition",
		"milestones": [{"id":"m-1"}],
		"members": [
			{"role":"cad-eng","plugins":["hardware"]},
			{"role":"ui-eng","plugins":["impeccable"]},
			{"role":"plain"}
		]
	}`)
	if err != nil {
		t.Fatalf("按当前划分装配应被受理：%v", err)
	}
	var got struct {
		PluginLimit int `json:"plugin_limit_per_teammate"`
		Assemblies  []struct {
			Role                  string   `json:"role"`
			Mode                  string   `json:"mode"`
			Plugins               []string `json:"plugins"`
			SkillCount            int      `json:"skill_count"`
			SkillCatalogRunes     int      `json:"skill_catalog_runes"`
			SkillCatalogTokensEst int      `json:"skill_catalog_tokens_est"`
			PluginFaceTools       int      `json:"plugin_face_tools"`
			TotalTools            int      `json:"total_tools"`
			Yellow                bool     `json:"yellow"`
		} `json:"assemblies"`
	}
	if err := json.Unmarshal([]byte(receipt), &got); err != nil {
		t.Fatalf("回执不是可解析的 JSON（%v）：%s", err, receipt)
	}
	if len(got.Assemblies) != 3 {
		t.Fatalf("回执应逐成员一条读数，得 %d 条：%s", len(got.Assemblies), receipt)
	}
	total := len(fixture.runtime.AllTools())
	hostReal := realRegistryFace(t, fixture.runtime)

	cad := got.Assemblies[0]
	if cad.Role != "cad-eng" || cad.Mode != assemblyModeReplace || !assemblyChainSameStrings(cad.Plugins, []string{"hardware"}) {
		t.Fatalf("hardware 成员读数：%#v", cad)
	}
	if cad.SkillCount != len(realPartitionHardwareSkills) {
		t.Fatalf("hardware 技能数 = %d，want %d（%v）", cad.SkillCount, len(realPartitionHardwareSkills), realPartitionHardwareSkills)
	}
	// 收窄是相对**真机全量面**说的：名单内的探针必过，且面严格小于全量。
	if cad.TotalTools != total || cad.PluginFaceTools >= total {
		t.Fatalf("hardware 的 include 必须真的收窄真机工具面：%#v（total=%d）", cad, total)
	}
	if cad.PluginFaceTools < len(realPartitionHardwareFace) {
		t.Fatalf("hardware 面 %d 少于名单内探针数 %d：%#v", cad.PluginFaceTools, len(realPartitionHardwareFace), cad)
	}
	if cad.SkillCatalogRunes != len([]rune(fixture.runtime.roleSkillCatalog([]string{"hardware"}))) {
		t.Fatalf("目录字节读数与真会进 prompt 的那一份不一致：%#v", cad)
	}

	ui := got.Assemblies[1]
	if ui.Mode != assemblyModeReplace || ui.SkillCount != 1 || ui.PluginFaceTools != total {
		t.Fatalf("impeccable 成员读数：%#v（1 份技能、include/exclude 皆空 ⇒ 工具面不收窄）", ui)
	}

	// ③ 不装配那一位：语义写出来（inherit-host），且工具面是**真读数**（默认 default 面），
	//    不是 0/0 的占位（回执里的 0 会被读成"零能力"）。
	plain := got.Assemblies[2]
	if plain.Mode != assemblyModeInheritHost || len(plain.Plugins) != 0 || plain.SkillCount != 0 {
		t.Fatalf("不装配成员的读数：%#v", plain)
	}
	if plain.TotalTools != total || plain.PluginFaceTools != total {
		t.Fatalf("不装配成员的插件面 = 默认 default 的面（include/exclude 皆空 ⇒ 全量）真读数：%#v（total=%d）", plain, total)
	}
	if plain.PluginFaceTools != ui.PluginFaceTools {
		t.Fatalf("装 impeccable 与不装配的工具面读数应相同（能力轴不改工具面）：ui=%d plain=%d",
			ui.PluginFaceTools, plain.PluginFaceTools)
	}
	if plain.SkillCatalogRunes != 0 || plain.Yellow || ui.Yellow || cad.Yellow {
		t.Fatalf("真划分的目录段都在 6k 半径内，不该有黄牌也不该有目录字节：%#v", got.Assemblies)
	}
	t.Logf("真机读数：全量 %d 个工具、宿主（default）面 %d 个、hardware 面 %d 个、impeccable 面 %d 个",
		total, len(hostReal), cad.PluginFaceTools, ui.PluginFaceTools)

	// ④ 真划分上的错别字：显式拒绝，且**不落盘**（拒绝不是文案）。
	rejected := assemblyChainPlanStore(t, fixture.runtime, "s-real-typo",
		`{"team_id":"t-typo","milestones":[{"id":"m-1"}],"members":[{"role":"ui-eng","plugins":["impeccables"]}]}`)
	if rejected.err == nil {
		t.Fatal("当前划分上的错别字必须被显式拒绝")
	}
	assemblyChainWantError(t, "真划分上的错别字", rejected.err, "未定义", "impeccables", "显式拒绝", "不静默忽略")
	if !rejected.empty {
		t.Fatal("被拒的计划不得落盘")
	}
}

// ── 基座与探针（真划分）────────────────────────────────────────────────────

type realPartitionFixture struct {
	runtime *Runtime
	root    string
}

// newRealPartitionFixture 把**发行根的真划分**装进一个测试 Runtime：加载器（与生产同一条
// 多根 first-wins 路）→ `Manager.Load`（定义插件 + 发布技能）→ 启动基线激活 `default`
// （与 main.go 的 activateDefaultPlugin 同一条路）。工具面是探针注册的（真 include 的
// 内外都有样本），所以"收窄"在这里是可判别的。
func newRealPartitionFixture(t testing.TB) *realPartitionFixture {
	t.Helper()
	root := realPartitionRoot
	if _, err := os.Stat(filepath.Join(root, "curated.yaml")); err != nil {
		t.Fatalf("当前插件根 %q 不像发行根（没有 curated.yaml）：%v", root, err)
	}
	runtime := newTestRuntime(t)
	for _, name := range realPartitionToolNames {
		name := name
		runtime.RegisterTool(name, name, map[string]interface{}{"type": "object"},
			func(context.Context, string) (string, error) { return "ok", nil })
	}
	skills := skill.NewRegistry()
	manager := plugin.NewManager(plugin.NewLoader(root), runtime, runtime, skills)
	if err := manager.Load(); err != nil {
		t.Fatalf("加载真插件根失败：%v", err)
	}
	runtime.SetSkillRegistry(skills)
	if err := manager.Activate(context.Background(), "default"); err != nil {
		t.Fatalf("启动基线激活 default 失败：%v", err)
	}
	return &realPartitionFixture{runtime: runtime, root: root}
}

func (f *realPartitionFixture) newProbe(id string) *assemblyChainProbe {
	// 复用装配链路的探针（记录 prompt / ctx 集合 / **生产那条收口**算出来的可见面）。
	return &assemblyChainProbe{id: id, runtime: f.runtime, all: realPartitionTools()}
}

// runRound 跑一个真划分下的角色回合（装配集合经 roleRoundSpec 走生产透传链）。
func (f *realPartitionFixture) runRound(t *testing.T, probe *assemblyChainProbe, role string, plugins []string, prompt string) assemblyChainSnapshot {
	t.Helper()
	f.runtime.SetRoleEngineFactory(assemblyChainEngineFactory(map[string]roleEngine{probe.id: probe}))
	if _, err := f.runtime.runRoleRound(context.Background(), roleRoundSpec{
		MainSessionID: "main-real-partition", RoleName: role, RoleSessionID: probe.id,
		SystemPrompt: prompt, Plugins: plugins, Input: "go",
	}); err != nil {
		t.Fatalf("回合失败（role=%s plugins=%v）：%v", role, plugins, err)
	}
	return probe.snapshot()
}

// realPartitionHostFace 是"不装配时 teammate 拿到的面"——宿主全局激活插件（启动基线
// default）经**生产那条收口**算出来的结果（含其余几道面的作用，不是手算的期望）。
func realPartitionHostFace(t testing.TB, runtime *Runtime) []string {
	t.Helper()
	return assemblyChainNames(runtime.visibilityPolicy.Filter(context.Background(), realPartitionTools()))
}

// realRegistryFace 用生产那条收口算"真机全量工具里哪些过得了宿主面"（回执读数的对照：
// 回执里的 plugin_face_tools 只算插件面，这里算的是含其余几道面的最终面）。
func realRegistryFace(t testing.TB, runtime *Runtime) []string {
	t.Helper()
	return assemblyChainNames(runtime.visibilityPolicy.Filter(context.Background(), realRegistryTools(runtime)))
}

// realRegistryTools 把运行期全量工具投影成可见性策略消费的 types.Tool（名字即全部输入）。
func realRegistryTools(runtime *Runtime) []types.Tool {
	all := runtime.AllTools()
	tools := make([]types.Tool, 0, len(all))
	for _, tool := range all {
		tools = append(tools, types.Tool{Type: "function", Function: types.ToolFunction{Name: tool.Name}})
	}
	return tools
}

func realPartitionTools() []types.Tool {
	tools := make([]types.Tool, 0, len(realPartitionToolNames))
	for _, name := range realPartitionToolNames {
		tools = append(tools, types.Tool{Type: "function", Function: types.ToolFunction{Name: name}})
	}
	return tools
}

// realPartitionCtx 给 team_plan 一个带会话键的 ctx（teamPlanHandler 按它取计划作用域）。
func realPartitionCtx(sessionID string) context.Context {
	return seeletelemetry.WithSessionID(context.Background(), sessionID)
}

func assemblyChainContains(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}
