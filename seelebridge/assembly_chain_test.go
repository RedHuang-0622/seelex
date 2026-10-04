package seelebridge

// assembly_chain_test.go — 装配链路（teammate 按 plugin 装配）的端到端用例与回归证。
//
// 链路是一条透传链，任一跳断掉都是"派发时带了、执行时丢了"：
//
//	team_plan members[].plugins → plan(sessionstore) → WorkerRequest.Plugins →
//	roleRoundSpec.Plugins → rolePluginAssembly（显式声明 > 角色自带 > 空集）→
//	本轮 ctx（seeltools.WithRolePlugins）→ Policy.Filter 的**最后一道** PluginFace →
//	工具可见面；同一集合另渲染成技能目录段追加进 system prompt，并给出装配读数进回执。
//
// 本文件钉五组（每组都写了"排掉了什么错"）：
//
//	1. 装配生效：装了 P 的 teammate 只见 P 的收窄结果 + P 的技能；装 Q 的不含 P 的东西；
//	   多插件是 include 并集。排掉"装配只进 ctx、没落到工具面/目录"与"两个 teammate 撞面"。
//	2. 不串味：两个 teammate 的回合**在会合点对齐着同时在飞**（不用 sleep），各自结果稳定，
//	   主代理的全局激活态与 root 面不受影响。排掉"把'当前装配'缓存在句柄/全局上"。
//	3. 回归：plugins 缺省/为空 ⇒ 与今天**逐字一致**（工具面走同一条早退路 + system 字节不变）。
//	4. 拒绝面：未知名 / 超上限 / 精选目录 pending（路线图）名——三种都断言错误文案，且断言
//	   被拒的计划**没有落盘**（不是"报了个错但仍然写进去"）。
//	5. 闸门读数：装配回执带目录字节/token 读数与黄牌判据（>6k token 或 >窗口 2%；只报不拒）。

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/plugin"
	seeletelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/seelebridge/tools"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/sessionstore"
	"github.com/RedHuang-0622/seelex/skill"
)

// ── 基座 ────────────────────────────────────────────────────────────────────

// 装配链路的工具探针名：cad 只许 cad_*，docs 只许 doc_*，ops 只许 get_*，
// 另有 write_file 谁都没装配到（用来证明"没装就没有"，也用来证明空集不等于"零工具面"）。
var assemblyChainToolNames = []string{"cad_draw", "doc_read", "doc_edit", "write_file", "get_time"}

// 三个假工具面上的插件（装配只收窄，不放宽）；宿主全局激活 cad。
const (
	assemblyChainHostPlugin = "cad"
	assemblyChainDocsPlugin = "docs"
	assemblyChainOpsPlugin  = "ops"
)

type assemblyChainCase struct {
	name    string
	plugins []string
	want    []string
}

// TestAssemblyChainTeammateSeesOwnPluginsToolsAndSkills 是第 1 组：装配**生效**。
//
// 排掉的错：① 装配集合只落进 ctx、没落到工具可见面（PluginFace 收口没接上）；
// ② 技能目录段没接上装配集合（装了 P 却看不到 P 的技能）；③ 两个 teammate 拿到同一份面
// （按会话隔离失效）；④ 装配与宿主全局激活态混在一起（装 docs 的却看见了 cad 的工具）。
func TestAssemblyChainTeammateSeesOwnPluginsToolsAndSkills(t *testing.T) {
	runtime := newAssemblyChainRuntime(t)
	defer runtime.Shutdown()

	registry := skill.NewRegistry()
	registry.SetPluginSkills(assemblyChainHostPlugin, []skill.Skill{{Name: "cad-tips", Description: "draw precisely"}})
	registry.SetPluginSkills(assemblyChainDocsPlugin, []skill.Skill{{Name: "docs-guide", Description: "how to read the docs"}})
	// 全局激活态故意指向 cad：员工面的目录**不许**读它（skill.Registry.All 会被它掩蔽）。
	registry.ActivatePlugin(assemblyChainHostPlugin)
	runtime.SetSkillRegistry(registry)

	cad := newAssemblyChainProbe(runtime, "role-cad")
	docs := newAssemblyChainProbe(runtime, "role-docs")
	both := newAssemblyChainProbe(runtime, "role-both")
	runtime.SetRoleEngineFactory(assemblyChainEngineFactory(map[string]roleEngine{
		cad.id: cad, docs.id: docs, both.id: both,
	}))

	round := func(probe *assemblyChainProbe, role string, plugins []string) {
		t.Helper()
		if _, err := runtime.runRoleRound(context.Background(), roleRoundSpec{
			MainSessionID: "main-assembly", RoleName: role, RoleSessionID: probe.id,
			SystemPrompt: "ROLE PROMPT", Plugins: plugins, Input: "go",
		}); err != nil {
			t.Fatalf("%s 回合失败：%v", probe.id, err)
		}
	}
	round(cad, "exec", []string{assemblyChainHostPlugin})
	round(docs, "writer", []string{assemblyChainDocsPlugin})
	round(both, "reviewer", []string{assemblyChainHostPlugin, assemblyChainDocsPlugin})

	// ① 工具面：装配集合 ∩ 全量工具，且只做减法。
	cadSnap, docsSnap, bothSnap := cad.snapshot(), docs.snapshot(), both.snapshot()
	wantCad, wantDocs := []string{"cad_draw"}, []string{"doc_read", "doc_edit"}
	if !assemblyChainSameStrings(cadSnap.ctxPlugins, []string{assemblyChainHostPlugin}) {
		t.Fatalf("装配集合没进本轮 ctx：%v", cadSnap.ctxPlugins)
	}
	if !assemblyChainSameStrings(cadSnap.visible, wantCad) {
		t.Fatalf("装 cad 的 teammate 可见工具 = %v，want %v（装配必须落到工具面）", cadSnap.visible, wantCad)
	}
	if !assemblyChainSameStrings(docsSnap.visible, wantDocs) {
		t.Fatalf("装 docs 的 teammate 可见工具 = %v，want %v", docsSnap.visible, wantDocs)
	}
	for _, name := range docsSnap.visible {
		if strings.HasPrefix(name, "cad_") {
			t.Fatalf("装 docs 的 teammate 看见了 cad 的工具 %q（串味）", name)
		}
	}
	// 多插件 = include 取并集（不是"只取第一个"）。
	if !assemblyChainSameStrings(bothSnap.visible, []string{"cad_draw", "doc_read", "doc_edit"}) {
		t.Fatalf("装 cad+docs 的 teammate 可见工具 = %v，want include 并集", bothSnap.visible)
	}
	// 判别力：退化形态（装配被忽略 = 只剩宿主全局面 cad_*）与实测面**不同**——上面那几条
	// 相等断言因此不是"两边都拿到同一份宿主面"式的空断言。
	hostFace := assemblyChainNames(runtime.visibilityPolicy.Filter(context.Background(), assemblyChainTools()))
	if assemblyChainSameStrings(hostFace, docsSnap.visible) || assemblyChainSameStrings(hostFace, bothSnap.visible) {
		t.Fatalf("装配被忽略才会等于宿主面：host=%v docs=%v both=%v", hostFace, docsSnap.visible, bothSnap.visible)
	}
	if docsSnap.lastPrompt() == cadSnap.lastPrompt() {
		t.Fatal("两个 teammate 的 system prompt 逐字相同 ⇒ 目录段没有按会话渲染")
	}

	// ② 技能目录：只给本会话装配的那几份，且不读全局激活态。
	cadPrompt, docsPrompt := cadSnap.lastPrompt(), docsSnap.lastPrompt()
	if !strings.Contains(cadPrompt, "cad-tips") || strings.Contains(cadPrompt, "docs-guide") {
		t.Fatalf("装 cad 的目录段不对：%q", cadPrompt)
	}
	if !strings.Contains(docsPrompt, "docs-guide") || strings.Contains(docsPrompt, "cad-tips") {
		t.Fatalf("装 docs 的目录段不对（全局激活 cad 不许掩蔽它）：%q", docsPrompt)
	}
	if !strings.Contains(docsPrompt, "## Available Skills") || !strings.Contains(docsPrompt, "cannot switch plugins") {
		t.Fatalf("目录段必须带员工面的口径纠正：%q", docsPrompt)
	}
	bothPrompt := bothSnap.lastPrompt()
	if !strings.Contains(bothPrompt, "cad-tips") || !strings.Contains(bothPrompt, "docs-guide") {
		t.Fatalf("装 cad+docs 的目录段必须含两份技能：%q", bothPrompt)
	}
	// ③ 装配不许改全局激活态（root 路径另有其人）。
	if got := runtime.ActivePlugin(); got != assemblyChainHostPlugin {
		t.Fatalf("员工装配不得改全局激活态：%q", got)
	}
}

// TestAssemblyChainConcurrentTeammatesDoNotCrossTalk 是第 2 组：**不串味**。
//
// 排掉的错：把"当前装配"缓存在句柄 / Policy / 任何全局位置上——两个回合同时在飞时，
// 后写的那份会覆盖先跑的那份（并发丢失更新的另一面）。交错由**会合点**排定：两个回合
// 都进到 ChatStream 之后才放行（没有 sleep，没有超时当同步手段）。
func TestAssemblyChainConcurrentTeammatesDoNotCrossTalk(t *testing.T) {
	runtime := newAssemblyChainRuntime(t)
	defer runtime.Shutdown()
	registry := skill.NewRegistry()
	registry.SetPluginSkills(assemblyChainHostPlugin, []skill.Skill{{Name: "cad-tips", Description: "draw precisely"}})
	registry.SetPluginSkills(assemblyChainDocsPlugin, []skill.Skill{{Name: "docs-guide", Description: "how to read the docs"}})
	runtime.SetSkillRegistry(registry)

	tools := assemblyChainTools()
	const rounds = 3
	for round := 0; round < rounds; round++ {
		barrier := &assemblyChainRendezvous{arrived: make(chan string, 2), release: make(chan struct{})}
		cad := newAssemblyChainProbe(runtime, "role-cad-"+strconv.Itoa(round))
		docs := newAssemblyChainProbe(runtime, "role-docs-"+strconv.Itoa(round))
		cad.marshal, docs.marshal = barrier, barrier
		runtime.SetRoleEngineFactory(assemblyChainEngineFactory(map[string]roleEngine{cad.id: cad, docs.id: docs}))

		failures := make(chan error, 2)
		var group sync.WaitGroup
		start := func(probe *assemblyChainProbe, role, pluginName string) {
			group.Add(1)
			go func() {
				defer group.Done()
				if _, err := runtime.runRoleRound(context.Background(), roleRoundSpec{
					MainSessionID: "main-assembly", RoleName: role, RoleSessionID: probe.id,
					SystemPrompt: "ROLE PROMPT", Plugins: []string{pluginName}, Input: "go",
				}); err != nil {
					failures <- fmt.Errorf("%s: %w", probe.id, err)
				}
			}()
		}
		start(cad, "exec", assemblyChainHostPlugin)
		start(docs, "writer", assemblyChainDocsPlugin)

		// 会合点：两个回合必须**同时**在 ChatStream 里（这才叫并发），再放行。
		for arrived := 0; arrived < 2; arrived++ {
			select {
			case <-barrier.arrived:
			case <-time.After(30 * time.Second):
				t.Fatalf("第 %d 轮：两个并发回合没能同时在飞（会合点超时）", round)
			}
		}
		close(barrier.release)
		group.Wait()
		close(failures)
		for err := range failures {
			t.Fatalf("并发回合失败：%v", err)
		}

		if got := cad.snapshot().visible; !assemblyChainSameStrings(got, []string{"cad_draw"}) {
			t.Fatalf("第 %d 轮：装 cad 的并发回合可见 %v（被另一个回合的装配污染）", round, got)
		}
		if got := docs.snapshot().visible; !assemblyChainSameStrings(got, []string{"doc_read", "doc_edit"}) {
			t.Fatalf("第 %d 轮：装 docs 的并发回合可见 %v（被另一个回合的装配污染）", round, got)
		}
		if !strings.Contains(cad.snapshot().lastPrompt(), "cad-tips") || strings.Contains(docs.snapshot().lastPrompt(), "cad-tips") {
			t.Fatalf("第 %d 轮：并发回合的目录段串味了", round)
		}
		if got := runtime.ActivePlugin(); got != assemblyChainHostPlugin {
			t.Fatalf("第 %d 轮：teammate 回合改了全局激活态 %q", round, got)
		}
		if got := assemblyChainNames(runtime.visibilityPolicy.Filter(context.Background(), tools)); !assemblyChainSameStrings(got, []string{"cad_draw"}) {
			t.Fatalf("第 %d 轮：主代理的 root 面被 teammate 的装配污染：%v", round, got)
		}
	}
}

// TestAssemblyChainEmptySetIsByteIdenticalToToday 是第 3 组：**回归**（plugins 缺省/为空）。
//
// "逐字一致"用四条具体断言钉：
//   - 工具面：空集时的收口结果与**旧收口**（只接 PluginFilter = 宿主全局装配）逐元素相等，
//     且旧收口非空（否则这条相等是"两个空集合相等"式的空断言，没有判别力）；
//   - system 字节：引擎上被设置的每一份 prompt 都与 roleTurnSystemPrompt(role) **逐字节相等**；
//   - 目录：空集不注入（目录段为空串），即使注册表里**确实有**宿主插件的技能、且全局激活态指向它；
//   - 阴性对照：同一个注册表下，非空集合确实渲染出目录段 ⇒"不注入"不是"目录功能没接上"。
//
// 排掉的错：把"没声明 plugins"误实现成"装配了空集"（工具面变空 / 目录被清空 / 注入全宿主技能）。
func TestAssemblyChainEmptySetIsByteIdenticalToToday(t *testing.T) {
	runtime := newAssemblyChainRuntime(t)
	defer runtime.Shutdown()
	registry := skill.NewRegistry()
	registry.SetPluginSkills(assemblyChainHostPlugin, []skill.Skill{{Name: "cad-tips", Description: "draw precisely"}})
	registry.ActivatePlugin(assemblyChainHostPlugin)
	runtime.SetSkillRegistry(registry)

	probe := newAssemblyChainProbe(runtime, "role-plain")
	runtime.SetRoleEngineFactory(assemblyChainEngineFactory(map[string]roleEngine{probe.id: probe}))

	// 生产路径给的就是 roleTurnSystemPrompt(role)（见 workerRoleRoundSpec）：用它当输入，
	// "字节一致"才有意义——空集时它必须**原样**被设置上去。
	role := "exec"
	prompt := runtime.roleTurnSystemPrompt(role)
	if _, err := runtime.runRoleRound(context.Background(), roleRoundSpec{
		MainSessionID: "main-assembly", RoleName: role, RoleSessionID: probe.id,
		SystemPrompt: prompt, Input: "go", // 不声明 Plugins = 缺省
	}); err != nil {
		t.Fatalf("空集回合失败：%v", err)
	}
	snapshot := probe.snapshot()

	// ① 工具面：同一条早退路（宿主全局装配），不是"等价实现"。
	all := assemblyChainTools()
	legacy := tools.NewPolicy(tools.PolicyDeps{PluginFilter: runtime.plugins.Filter})
	legacyFace := assemblyChainNames(legacy.Filter(context.Background(), all))
	liveFace := assemblyChainNames(runtime.visibilityPolicy.Filter(snapshot.ctx, all))
	if !assemblyChainSameStrings(legacyFace, []string{"cad_draw"}) {
		t.Fatalf("基座前提不成立（宿主激活 cad，include cad_*）：%v", legacyFace)
	}
	if !assemblyChainSameStrings(liveFace, legacyFace) {
		t.Fatalf("空集必须逐字沿用宿主的插件收口：活路径 %v，旧收口 %v", liveFace, legacyFace)
	}

	// ② system 字节：建会话时一次 + 每轮起手一次，两次都是原始字节。
	want := prompt
	if len(snapshot.prompts) == 0 {
		t.Fatal("回合并未设置 system prompt")
	}
	for index, got := range snapshot.prompts {
		if got != want {
			t.Fatalf("空集的 system 字节必须逐字不变（第 %d 次设置）：\n got %q\nwant %q", index, got, want)
		}
	}
	if strings.Contains(want, "## Available Skills") {
		t.Fatalf("没有装配就不该有目录段：%q", want)
	}

	// ③ 目录：空集不注入 + 恒等式（追加空目录段 = 原样返回）。
	if catalog := runtime.roleSkillCatalog(nil); catalog != "" {
		t.Fatalf("空集不得注入技能目录，得 %q", catalog)
	}
	if catalog := runtime.roleSkillCatalog([]string{}); catalog != "" {
		t.Fatalf("空切片同义，得 %q", catalog)
	}
	if got := appendSkillCatalog(want, ""); got != want {
		t.Fatalf("空目录段必须原样返回：%q → %q", want, got)
	}

	// ④ 阴性对照：技能确实在注册表里、非空集合确实会注入 ⇒ 上面的"不注入"不是空断言。
	if catalog := runtime.roleSkillCatalog([]string{assemblyChainHostPlugin}); catalog == "" || !strings.Contains(catalog, "cad-tips") {
		t.Fatalf("同一个注册表下非空集合应渲染出目录段：%q", catalog)
	}
}

// TestAssemblyChainPendingPluginCannotBeAssembled 是第 4 组的第三种拒绝：**路线图上的名字**。
//
// 精选目录允许把"这台机器/这个发行包里还没有"的插件列在 presets[].pending（路线图，
// 带证据档：谁给的 + 核到什么程度 + 转正还缺什么）。这些名字**必须进不了装配**，
// 而且拒绝文案要点名 pending 与它的来源——否则用户只看到一句"插件目录不存在"，
// 既不知道那是路线图候选，也不知道该去核谁。
//
// 本用例钉两件事：① 目录侧的装配解析（AssemblePreset / AssemblePlugin）对 pending
// 显式拒绝；② **同一条链的下一跳**（team_plan 的成员装配）对同一个名字也显式拒绝，
// 而两处对"已落盘的那个"给出同一个答案（目录说装得起来 ⇒ team_plan 收；目录说
// 装不起来 ⇒ team_plan 拒）。
//
// 排掉的错：把路线图当可用集合——装配时静默跳过 → 落成一个看起来正常、实际什么都没有的
// 空插件面；以及两处闸门对同一个名字给出不同答案（一处放行一处拒绝）。
func TestAssemblyChainPendingPluginCannotBeAssembled(t *testing.T) {
	const roadmap = "next-cad"
	const upstream = "acme/next-cad"
	installed := []string{"default", assemblyChainDocsPlugin}

	catalog := plugin.CuratedCatalog{
		SchemaVersion: plugin.CurrentSchemaVersion,
		Kind:          plugin.CuratedKind,
		ReadAt:        "2026-10-05",
		Entries: []plugin.CuratedEntry{
			assemblyChainCuratedEntry("default"),
			assemblyChainCuratedEntry(assemblyChainDocsPlugin),
		},
		Presets: []plugin.CuratedPreset{
			{Name: plugin.CuratedBaselinePreset, Description: "baseline", PermissionTier: "manual",
				Plugins: []string{"default"}, Pending: []plugin.CuratedPending{}},
			{Name: "cad", Description: "cad", PermissionTier: "manual",
				Plugins: []string{assemblyChainDocsPlugin},
				Pending: []plugin.CuratedPending{{
					Name: roadmap, Upstream: upstream, Verified: plugin.CuratedVerifiedListingOnly,
					Promote: plugin.CuratedPromoteQuestions,
					Source: plugin.CuratedSource{
						Kind: plugin.CuratedSourceCommunityListing, URL: "https://example.com/listing",
						License: "MIT", Pinned: "listing", ReadAt: "2026-10-05",
					},
				}}},
		},
	}
	// 目录本身是合法的（pending 带证据档即可）：否则下面那组拒绝可以靠"目录永远报错"通过。
	if err := plugin.ValidateCuratedCatalog(catalog, installed); err != nil {
		t.Fatalf("带证据档的 pending 是合法路线图：%v", err)
	}

	// ① 单插件装配：pending 候选必须被拒，且文案点名 pending 与来源。
	if _, err := catalog.AssemblePlugin(roadmap, installed); err == nil {
		t.Fatalf("装配到 pending %q 必须被拒绝", roadmap)
	} else {
		assemblyChainWantError(t, "pending 候选（AssemblePlugin）", err, "pending", roadmap, upstream)
	}
	// ② preset 装配：preset.plugins 引用一个尚未落盘（且在别的 preset 里是 pending）的名字，
	//    也必须被拒（不是静默跳过那一条）。
	referencing := catalog
	referencing.Presets = append([]plugin.CuratedPreset(nil), catalog.Presets...)
	referencing.Presets[0].Plugins = []string{"default", roadmap}
	if _, err := referencing.AssemblePreset(plugin.CuratedBaselinePreset, installed); err == nil {
		t.Fatalf("preset 引用尚未落盘的 %q 必须被拒绝", roadmap)
	} else {
		assemblyChainWantError(t, "preset 引用 pending", err, "pending", roadmap, upstream)
	}
	// 阴性对照 + 链的下一条：目录说装得起来的那个，plan 里就是那一个。
	plan, err := catalog.AssemblePreset("cad", installed)
	if err != nil {
		t.Fatalf("已落盘 preset 必须装得起来：%v", err)
	}
	if !assemblyChainSameStrings(plan.Plugins, []string{assemblyChainDocsPlugin}) {
		t.Fatalf("assembly plan = %#v，want 只含已落盘插件", plan)
	}

	// ③ 链的下一跳：同一个名字进 team_plan 也必须显式拒绝；目录放行的那个必须收。
	runtime := newAssemblyChainRuntime(t)
	defer runtime.Shutdown()
	if err := runtime.DefinePlugin("default", "Baseline", nil, nil); err != nil {
		t.Fatal(err)
	}
	rejected := assemblyChainPlanStore(t, runtime, "s-roadmap",
		`{"team_id":"t-roadmap","milestones":[{"id":"m-1"}],"members":[{"role":"exec","plugins":["`+roadmap+`"]}]}`)
	if rejected.err == nil {
		t.Fatal("装配到路线图插件必须被拒")
	}
	assemblyChainWantError(t, "路线图插件（team_plan）", rejected.err, "未定义", roadmap, "显式拒绝")
	if !rejected.empty {
		t.Fatal("被拒的计划**不得落盘**（拒绝了却仍然写进去 = 拒绝只是文案）")
	}
	accepted := assemblyChainPlanStore(t, runtime, "s-roadmap-ok",
		`{"team_id":"t-roadmap-ok","milestones":[{"id":"m-1"}],"members":[{"role":"exec","plugins":["`+assemblyChainDocsPlugin+`"]}]}`)
	if accepted.err != nil || accepted.empty {
		t.Fatalf("目录放行的插件必须能被 team_plan 装配：err=%v empty=%v", accepted.err, accepted.empty)
	}
}

// TestAssemblyChainRejectionsSpellOutTheReason 是第 4 组：三条显式拒绝各自说清理由。
//
// 排掉的错：① 未知名被静默忽略（leader 以为装上了、员工那头一个能力也没有，要到跑完才发现）；
// ② 超上限被静默截断（"我声明了 5 个"悄悄变成"装了 3 个"）；③ 拒绝发生在写盘之后
// （报错但计划已经落地）。三种都断言**错误文案**，不是只看 err != nil。
func TestAssemblyChainRejectionsSpellOutTheReason(t *testing.T) {
	runtime := newAssemblyChainRuntime(t)
	defer runtime.Shutdown()
	if err := runtime.DefinePlugin("default", "Baseline", nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := runtime.maxPluginsPerTeammate(); got != seelexctx.DefaultPluginsPerTeammate {
		t.Fatalf("未配置时每会话上限应走出厂值 %d，得 %d", seelexctx.DefaultPluginsPerTeammate, got)
	}

	const sessionID = "s-reject"
	store := &memPlanStore{}
	if err := runtime.SetTeamworkBackend(teamworkTestBackend(store, sessionID)); err != nil {
		t.Fatal(err)
	}
	ctx := seeletelemetry.WithSessionID(context.Background(), sessionID)
	key := sessionstore.Key{ProjectID: "p-team", SessionID: sessionID}

	// 先落一份合法计划当基准：后面每一条被拒之后，落盘的必须**还是这一份**。
	accepted := `{"team_id":"t-accepted","milestones":[{"id":"m-1"}],"members":[{"role":"exec","plugins":["docs"]}]}`
	if _, err := runtime.teamPlanHandler(ctx, accepted); err != nil {
		t.Fatalf("合法装配不得被拒：%v", err)
	}
	baseline, err := store.ReadPlan(context.Background(), key)
	if err != nil {
		t.Fatalf("基准计划未落盘：%v", err)
	}
	if len(baseline.Members) != 1 || !assemblyChainSameStrings(baseline.Members[0].Plugins, []string{assemblyChainDocsPlugin}) {
		t.Fatalf("基准计划的装配不对：%#v", baseline.Members)
	}

	cases := []struct {
		name    string
		plugins string
		want    []string
	}{
		{"未知名", `["docs","ghost"]`, []string{"未定义", "显式拒绝", "不静默忽略", "ghost"}},
		{"超上限", `["cad","docs","ops","default"]`, []string{"上限 3", "声明了 4 个", "不静默截断"}},
		{"重复声明", `["docs"," docs "]`, []string{"重复", "docs", "不静默"}},
		{"只留空白项 = 空集", `["  "]`, nil},
	}
	for _, tc := range cases {
		plan := `{"team_id":"t-` + tc.name + `","milestones":[{"id":"m-1"}],"members":[{"role":"exec","plugins":` + tc.plugins + `}]}`
		_, err := runtime.teamPlanHandler(ctx, plan)
		if tc.want == nil {
			// 空白项清完为空 = 不覆盖（不是"装配了一个叫空字符串的插件"）。
			if err != nil {
				t.Fatalf("%s 不该被拒：%v", tc.name, err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("%s 必须被显式拒绝", tc.name)
		}
		assemblyChainWantError(t, tc.name, err, tc.want...)
		after, readErr := store.ReadPlan(context.Background(), key)
		if readErr != nil {
			t.Fatalf("%s：拒绝之后计划不见了：%v", tc.name, readErr)
		}
		if after.TeamID != baseline.TeamID || !assemblyChainSameStrings(after.Members[0].Plugins, []string{assemblyChainDocsPlugin}) {
			t.Fatalf("%s：被拒的计划不得落盘（现落盘的是 %q / %#v）", tc.name, after.TeamID, after.Members)
		}
	}
}

// TestAssemblyChainReceiptCarriesCatalogReadingsAndYellow 是第 5 组：**闸门读数**。
//
// 排掉的错：① 装配回执不带读数（leader 无法知道这一装把多少字节挂上了每轮常驻的
// system prompt——成本不可见）；② 空集成员不写明语义（靠字段缺失暗示"没装配"，会被读成
// "装配了零个插件"）；③ 黄牌判据写反（该报不报 / 该放行却拒）。口径：>6k token 估算或
// >上下文窗口 2% ⇒ 回执给黄牌读数，**不拒绝装配**。
func TestAssemblyChainReceiptCarriesCatalogReadingsAndYellow(t *testing.T) {
	runtime := newAssemblyChainRuntime(t)
	defer runtime.Shutdown()
	registry := skill.NewRegistry()
	registry.SetPluginSkills(assemblyChainDocsPlugin, []skill.Skill{{Name: "docs-guide", Description: "how to read the docs"}})
	registry.SetPluginSkills(assemblyChainOpsPlugin, []skill.Skill{{Name: "ops-runbook", Description: "short"}})
	runtime.SetSkillRegistry(registry)

	const sessionID = "s-readings"
	store := &memPlanStore{}
	if err := runtime.SetTeamworkBackend(teamworkTestBackend(store, sessionID)); err != nil {
		t.Fatal(err)
	}
	ctx := seeletelemetry.WithSessionID(context.Background(), sessionID)

	receipt, err := runtime.teamPlanHandler(ctx, `{
		"team_id": "t-readings",
		"milestones": [{"id":"m-1"}],
		"members": [
			{"role":"exec","plugins":["docs"]},
			{"role":"pm"},
			{"role":"writer","plugins":[" ops "]}
		]
	}`)
	if err != nil {
		t.Fatalf("team_plan：%v", err)
	}
	var got struct {
		PluginLimit int `json:"plugin_limit_per_teammate"`
		Assemblies  []struct {
			Role                  string   `json:"role"`
			Mode                  string   `json:"mode"`
			Plugins               []string `json:"plugins"`
			PluginCount           int      `json:"plugin_count"`
			SkillCount            int      `json:"skill_count"`
			SkillCatalogRunes     int      `json:"skill_catalog_runes"`
			SkillCatalogTokensEst int      `json:"skill_catalog_tokens_est"`
			PluginFaceTools       int      `json:"plugin_face_tools"`
			TotalTools            int      `json:"total_tools"`
			Yellow                bool     `json:"yellow"`
			YellowReason          string   `json:"yellow_reason"`
		} `json:"assemblies"`
	}
	if err := json.Unmarshal([]byte(receipt), &got); err != nil {
		t.Fatalf("回执不是可解析的 JSON（%v）：%s", err, receipt)
	}
	if got.PluginLimit != seelexctx.DefaultPluginsPerTeammate {
		t.Fatalf("回执必须带上限读数：得 %d", got.PluginLimit)
	}
	if len(got.Assemblies) != 3 {
		t.Fatalf("回执的 assemblies = %d 条，want 逐成员一条：%s", len(got.Assemblies), receipt)
	}

	// ① 声明了集合的成员：集合 + 目录字节/token + 工具面读数（6k 半径内 → 不黄）。
	catalog := runtime.roleSkillCatalog([]string{assemblyChainDocsPlugin})
	declared := got.Assemblies[0]
	if declared.Role != "exec" || declared.Mode != "replace" || !assemblyChainSameStrings(declared.Plugins, []string{"docs"}) {
		t.Fatalf("声明面读数：%#v", declared)
	}
	if declared.PluginCount != 1 || declared.SkillCount != 1 {
		t.Fatalf("集合/技能计数：%#v", declared)
	}
	if declared.SkillCatalogRunes != len([]rune(catalog)) {
		t.Fatalf("目录字节读数 %d，want %d（必须是**真会进 system prompt 的那一份**）", declared.SkillCatalogRunes, len([]rune(catalog)))
	}
	if declared.SkillCatalogTokensEst != (declared.SkillCatalogRunes+3)/4 {
		t.Fatalf("token 估算口径不对：%#v", declared)
	}
	if declared.Yellow || declared.YellowReason != "" {
		t.Fatalf("小目录不该黄牌：%#v", declared)
	}
	if declared.PluginFaceTools != 2 || declared.TotalTools != len(runtime.AllTools()) {
		t.Fatalf("工具面读数：%#v（docs include doc_*，全量 %d 个）", declared, len(runtime.AllTools()))
	}

	// ② 空集成员：语义**写出来**（inherit-host），不靠字段缺失暗示。
	inherited := got.Assemblies[1]
	if inherited.Mode != assemblyModeInheritHost || len(inherited.Plugins) != 0 || inherited.SkillCatalogRunes != 0 {
		t.Fatalf("空集读数：%#v", inherited)
	}

	// ③ 规整写回：[" ops "] → ["ops"]（计划里存的才是运行时真会用的那一份）。
	if !assemblyChainSameStrings(got.Assemblies[2].Plugins, []string{assemblyChainOpsPlugin}) || got.Assemblies[2].PluginCount != 1 {
		t.Fatalf("规整结果必须写回并出现在回执里：%#v", got.Assemblies[2])
	}
	plan, err := store.ReadPlan(context.Background(), sessionstore.Key{ProjectID: "p-team", SessionID: sessionID})
	if err != nil {
		t.Fatalf("计划未落盘：%v", err)
	}
	if !assemblyChainSameStrings(plan.Members[2].Plugins, []string{assemblyChainOpsPlugin}) {
		t.Fatalf("计划里存的装配必须是规整后的那一份：%#v", plan.Members[2].Plugins)
	}

	// ④ 黄牌判据：>6k token 报黄（只报不拒），窗口 2% 是更紧的那道门。
	if ok, _ := runtime.pluginsCatalogYellow(0); ok {
		t.Fatal("零字节目录不得报黄牌")
	}
	if ok, reason := runtime.pluginsCatalogYellow(pluginsCatalogTokenWarn + 1); !ok || !strings.Contains(reason, "6k") {
		t.Fatalf(">6k token 应报黄牌并说清判据，得 (%v, %q)", ok, reason)
	}
	if window := runtime.ContextWindow(); window > 0 && window/50+2 <= pluginsCatalogTokenWarn {
		justInside, justOutside := window/50, window/50+1
		if ok, _ := runtime.pluginsCatalogYellow(justInside); ok {
			t.Fatalf("窗口 %d 的 2%% = %d token，%d 不该报黄", window, window/50, justInside)
		}
		if ok, reason := runtime.pluginsCatalogYellow(justOutside); !ok || !strings.Contains(reason, "2%") {
			t.Fatalf("超过窗口 2%% 应报黄牌并说清判据，得 (%v, %q)", ok, reason)
		}
	}

	// ⑤ 真的顶到 6k 的目录：回执必须报黄，且**装配照旧受理**（黄牌不拒）。
	big := strings.Repeat("x", pluginsCatalogTokenWarn*4+400)
	registry.SetPluginSkills(assemblyChainOpsPlugin, []skill.Skill{{Name: "ops-runbook", Description: big}})
	receipt, err = runtime.teamPlanHandler(ctx, `{
		"team_id": "t-readings-big",
		"milestones": [{"id":"m-1"}],
		"members": [{"role":"exec","plugins":["ops"]}]
	}`)
	if err != nil {
		t.Fatalf("超阈值的目录必须**受理**（黄牌不拒）：%v", err)
	}
	if err := json.Unmarshal([]byte(receipt), &got); err != nil {
		t.Fatalf("回执解析失败：%v", err)
	}
	bigView := got.Assemblies[0]
	if !bigView.Yellow || !strings.Contains(bigView.YellowReason, "6k") {
		t.Fatalf("超 6k token 的装配必须报黄牌读数：%#v", bigView)
	}
	if bigView.SkillCatalogTokensEst <= pluginsCatalogTokenWarn || bigView.SkillCatalogRunes != len([]rune(runtime.roleSkillCatalog([]string{assemblyChainOpsPlugin}))) {
		t.Fatalf("黄牌读数与目录字节必须自洽：%#v", bigView)
	}

	// ⑥ 配置口径：发行档里的 limits.plugins.per_teammate 必须真的能读出来（上限不是
	// 散在代码里的常量），且与回执报的是同一个数。
	limits, err := seelexctx.LoadLimits(filepath.Join("..", "config", "seelex.yaml"))
	if err != nil {
		t.Fatalf("读发行配置：%v", err)
	}
	if limits.Plugins.PerTeammate != seelexctx.DefaultPluginsPerTeammate {
		t.Fatalf("config/seelex.yaml 的 limits.plugins.per_teammate = %d，want %d（回执报的就是这一项）",
			limits.Plugins.PerTeammate, seelexctx.DefaultPluginsPerTeammate)
	}
}

// ── 探针与工具 ──────────────────────────────────────────────────────────────

// assemblyChainRendezvous 是会合点：两个回合各自报告"我已进 ChatStream"，然后一起等放行。
// 用它排定交错，**不用 sleep**（sleep 排定的交错既慢又不稳定，且会掩盖真正的顺序依赖）。
type assemblyChainRendezvous struct {
	arrived chan string
	release chan struct{}
}

// assemblyChainProbe 是最小角色引擎：记录本轮 system prompt、ctx 里的装配集合，以及
// **本轮真正可见的工具面**（用运行期的 Policy 实例 + 本轮 ctx 现算，即生产那条路）。
type assemblyChainProbe struct {
	id      string
	runtime *Runtime
	all     []types.Tool
	marshal *assemblyChainRendezvous

	mu         sync.Mutex
	prompts    []string
	ctx        context.Context
	ctxPlugins []string
	visible    []string
}

func newAssemblyChainProbe(runtime *Runtime, id string) *assemblyChainProbe {
	return &assemblyChainProbe{id: id, runtime: runtime, all: assemblyChainTools()}
}

func (p *assemblyChainProbe) SessionID() string { return p.id }

func (p *assemblyChainProbe) SetSystemPrompt(prompt string) {
	p.mu.Lock()
	p.prompts = append(p.prompts, prompt)
	p.mu.Unlock()
}

func (p *assemblyChainProbe) SetMaxLoops(int)          {}
func (p *assemblyChainProbe) ClearHistory()            {}
func (p *assemblyChainProbe) History() []types.Message { return nil }

func (p *assemblyChainProbe) ChatStream(ctx context.Context, _ string, _ func(string)) (string, error) {
	if p.marshal != nil {
		p.marshal.arrived <- p.id
		<-p.marshal.release
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ctx = ctx
	p.ctxPlugins = tools.RolePluginsFromContext(ctx)
	p.visible = assemblyChainNames(p.runtime.visibilityPolicy.Filter(ctx, p.all))
	return "done", nil
}

type assemblyChainSnapshot struct {
	prompts    []string
	ctx        context.Context
	ctxPlugins []string
	visible    []string
}

func (p *assemblyChainProbe) snapshot() assemblyChainSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return assemblyChainSnapshot{
		prompts:    append([]string(nil), p.prompts...),
		ctx:        p.ctx,
		ctxPlugins: append([]string(nil), p.ctxPlugins...),
		visible:    append([]string(nil), p.visible...),
	}
}

func (s assemblyChainSnapshot) lastPrompt() string {
	if len(s.prompts) == 0 {
		return ""
	}
	return s.prompts[len(s.prompts)-1]
}

// assemblyChainEngineFactory 按会话号发引擎；工厂会被并发调用（两个回合同时开会话）。
func assemblyChainEngineFactory(engines map[string]roleEngine) func(string) (roleEngine, error) {
	var mu sync.Mutex
	return func(sessionID string) (roleEngine, error) {
		mu.Lock()
		defer mu.Unlock()
		engine := engines[sessionID]
		if engine == nil {
			return nil, fmt.Errorf("装配链路探针没有为会话 %q 准备引擎", sessionID)
		}
		return engine, nil
	}
}

// newAssemblyChainRuntime 是装配链路的基座：宿主全局激活 cad，另有 docs / ops 两个只
// 按会话装配才拿得到的插件（三个插件的 include 互不重叠）。
func newAssemblyChainRuntime(t testing.TB) *Runtime {
	t.Helper()
	runtime := newTestRuntime(t)
	for _, name := range assemblyChainToolNames {
		name := name
		runtime.RegisterTool(name, name, map[string]interface{}{"type": "object"},
			func(context.Context, string) (string, error) { return "ok", nil })
	}
	for _, spec := range []struct {
		name    string
		include []string
	}{
		{assemblyChainHostPlugin, []string{"cad_*"}},
		{assemblyChainDocsPlugin, []string{"doc_*"}},
		{assemblyChainOpsPlugin, []string{"get_*"}},
	} {
		if err := runtime.DefinePlugin(spec.name, strings.ToUpper(spec.name), spec.include, nil); err != nil {
			t.Fatalf("DefinePlugin(%s): %v", spec.name, err)
		}
	}
	if err := runtime.ActivatePlugin(assemblyChainHostPlugin); err != nil {
		t.Fatalf("ActivatePlugin: %v", err)
	}
	return runtime
}

func assemblyChainTools() []types.Tool {
	built := make([]types.Tool, 0, len(assemblyChainToolNames))
	for _, name := range assemblyChainToolNames {
		built = append(built, types.Tool{Type: "function", Function: types.ToolFunction{Name: name}})
	}
	return built
}

func assemblyChainNames(tools []types.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Function.Name)
	}
	return names
}

func assemblyChainSameStrings(got, want []string) bool {
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

// assemblyChainWantError 断言错误文案（不是只看 err != nil）：拒绝了什么、为什么、
// 以及"不静默忽略/截断"这几个词必须出现在回给 leader 的那句话里。
func assemblyChainWantError(t *testing.T, name string, err error, want ...string) {
	t.Helper()
	message := err.Error()
	for _, substring := range want {
		if !strings.Contains(message, substring) {
			t.Fatalf("%s：错误文案 %q 必须包含 %q（拒绝必须说清理由，不靠上层猜）", name, message, substring)
		}
	}
}

type assemblyChainPlanResult struct {
	err   error
	empty bool
}

// assemblyChainPlanStore 调一次 team_plan 并回报：错误，以及"这次调用是否**没有**留下计划"。
func assemblyChainPlanStore(t *testing.T, runtime *Runtime, sessionID, argsJSON string) assemblyChainPlanResult {
	t.Helper()
	store := &memPlanStore{}
	if err := runtime.SetTeamworkBackend(teamworkTestBackend(store, sessionID)); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	ctx := seeletelemetry.WithSessionID(context.Background(), sessionID)
	_, err := runtime.teamPlanHandler(ctx, argsJSON)
	plan, readErr := store.ReadPlan(context.Background(), sessionstore.Key{ProjectID: "p-team", SessionID: sessionID})
	return assemblyChainPlanResult{err: err, empty: readErr != nil || plan.TeamID == ""}
}

func assemblyChainCuratedEntry(name string) plugin.CuratedEntry {
	return plugin.CuratedEntry{
		Name: name, Description: name + " plugin", Installed: true,
		Source: plugin.CuratedSource{
			Kind: "builtin", URL: "https://example.com/" + name, License: "MIT",
			Pinned: "v1", ReadAt: "2026-10-05",
		},
	}
}
