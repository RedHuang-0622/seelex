package seelebridge

// runtime_teamwork_curated_test.go — 「精选目录进运行期」在**装配入口**的回归证（A 的第二半）。
//
// 这里不重复 plugin 包已经钉住的目录解析（那是 plugin/curated_test.go 与根包
// plugin_curated_runtime_test.go 的事）；本文件钉的是**运行期那一条链**：
//
//	团队计划 members[].plugins → teamPlanHandler 的校验入口（runtime_teamwork.go:283 一带）
//	→ 未定义的名字问一句"它是谁"（桥上的 UnassembledReason，产品启动期注入）
//	→ 判决函数（精选目录读数）→ 显式拒绝 + 上报文案；被拒的计划**不落盘**。
//
// 排掉的错（A 的原症状）：库里读得到 pending、运行期却只报一句"未定义"——
// 于是"清单上看见的路线图候选"与"写错别字"在回执里长得一模一样。
//
// 六条：① pending 走真判决函数（真目录读数）被拒且点名来源；② 谁都不认识被拒且说清
// 两边都不在；③ 目录读不到也拒（不得静默当空目录）；④ 已装插件走原路径、计划照落；
// ⑤ 未注入判决函数时回落既有"未定义"口径（行为不变）；⑥ 语法先于语义（超限报上限）。

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/plugin"
)

// curatedRuntimeRead 造一份**运行期会见到**的精选目录读数：entries 三个已装插件、
// 一个 pending 候选（next-cad，带上游来源与实读页），实读页写死在文案里。
func curatedRuntimeRead(t *testing.T) plugin.CuratedRead {
	t.Helper()
	installed := []string{"default", assemblyChainHostPlugin, assemblyChainDocsPlugin, assemblyChainOpsPlugin}
	catalog := plugin.CuratedCatalog{
		SchemaVersion: plugin.CurrentSchemaVersion,
		Kind:          plugin.CuratedKind,
		ReadAt:        "2026-10-05",
		Entries: []plugin.CuratedEntry{
			assemblyChainCuratedEntry("default"),
			assemblyChainCuratedEntry(assemblyChainHostPlugin),
			assemblyChainCuratedEntry(assemblyChainDocsPlugin),
			assemblyChainCuratedEntry(assemblyChainOpsPlugin),
		},
		Presets: []plugin.CuratedPreset{
			{Name: plugin.CuratedBaselinePreset, Description: "baseline", PermissionTier: "manual",
				Plugins: []string{"default"}, Pending: []plugin.CuratedPending{}},
			{Name: "cad", Description: "cad", PermissionTier: "manual",
				Plugins: installed,
				Pending: []plugin.CuratedPending{{
					Name: curatedRuntimeRoadmap, Upstream: curatedRuntimeUpstream,
					Verified: plugin.CuratedVerifiedListingOnly,
					Promote:  plugin.CuratedPromoteQuestions,
					Source: plugin.CuratedSource{
						Kind: plugin.CuratedSourceCommunityListing, URL: curatedRuntimeListing,
						License: "MIT", Pinned: "listing", ReadAt: "2026-10-05",
					},
				}}},
		},
	}
	// 目录本身必须合法：否则下面那组拒绝可以靠"目录永远报错"通过。
	if err := plugin.ValidateCuratedCatalog(catalog, installed); err != nil {
		t.Fatalf("夹具目录不合法：%v", err)
	}
	return plugin.CuratedRead{Catalog: catalog, Path: curatedRuntimePage, Roots: []string{"plugins"}}
}

const (
	curatedRuntimeRoadmap  = "next-cad"
	curatedRuntimeUpstream = "acme/next-cad"
	curatedRuntimeListing  = "https://example.com/awesome-skills"
	curatedRuntimePage     = "plugins/curated.yaml"
)

// newCuratedRuntime 造装配链路的基座（cad/docs/ops 三个已定义插件）并注入真判决函数，
// 与产品启动面（main.go 的 curatedAssemblyJudge → SetPluginUnassembledReason）同一条路。
func newCuratedRuntime(t *testing.T, read plugin.CuratedRead) *Runtime {
	t.Helper()
	runtime := newAssemblyChainRuntime(t)
	if err := runtime.DefinePlugin("default", "Baseline", nil, nil); err != nil {
		t.Fatalf("DefinePlugin(default): %v", err)
	}
	runtime.SetPluginUnassembledReason(read.Judge)
	return runtime
}

// TestTeamPlanRejectsPendingPluginWithSource 钉住 A 的主句：pending（路线图）名字进
// team_plan 被拒，文案点名 pending + 上游来源 + 实读页；且**被拒的计划不落盘**。
func TestTeamPlanRejectsPendingPluginWithSource(t *testing.T) {
	runtime := newCuratedRuntime(t, curatedRuntimeRead(t))
	defer runtime.Shutdown()

	rejected := assemblyChainPlanStore(t, runtime, "s-curated-pending",
		`{"team_id":"t-curated-pending","milestones":[{"id":"m-1"}],"members":[{"role":"exec","plugins":["`+curatedRuntimeRoadmap+`"]}]}`)
	if rejected.err == nil {
		t.Fatalf("装配到 pending %q 必须被拒（库里读到了它，运行期就得认出来）", curatedRuntimeRoadmap)
	}
	assemblyChainWantError(t, "pending 候选（team_plan）", rejected.err,
		"pending", curatedRuntimeRoadmap, curatedRuntimeUpstream, curatedRuntimeListing,
		plugin.CuratedVerifiedListingOnly, curatedRuntimePage, "显式拒绝")
	if !rejected.empty {
		t.Fatal("被拒的计划**不得落盘**（拒绝了却仍然写进去 = 拒绝只是文案）")
	}
}

// TestTeamPlanRejectsUnknownPluginAgainstBothLists 钉住"谁都不认识"的那一句：
// 必须同时说清"不在已装插件"与"也不在精选目录"（两边名单都写上——错别字要靠名单自查）。
func TestTeamPlanRejectsUnknownPluginAgainstBothLists(t *testing.T) {
	runtime := newCuratedRuntime(t, curatedRuntimeRead(t))
	defer runtime.Shutdown()

	rejected := assemblyChainPlanStore(t, runtime, "s-curated-ghost",
		`{"team_id":"t-curated-ghost","milestones":[{"id":"m-1"}],"members":[{"role":"exec","plugins":["gost"]}]}`)
	if rejected.err == nil {
		t.Fatal("谁都不认识的名字必须被拒")
	}
	assemblyChainWantError(t, "未知名（team_plan）", rejected.err,
		"gost", "不在已装插件", "也不在精选目录", curatedRuntimePage,
		assemblyChainHostPlugin, assemblyChainDocsPlugin, "不静默忽略")
	if !rejected.empty {
		t.Fatal("被拒的计划不得落盘")
	}
}

// TestTeamPlanRejectsWhenCuratedCatalogIsUnreadable 钉住"读不到不得静默当空目录"：
// 目录缺失/解析失败时，装配面的未定义名仍被拒，且文案说清**找过哪些根**、为什么没读到。
func TestTeamPlanRejectsWhenCuratedCatalogIsUnreadable(t *testing.T) {
	roots := []string{"<exe>/plugins", "plugins"}
	unreadable := plugin.CuratedRead{
		Roots: roots,
		Err:   errCuratedMissing(),
	}
	runtime := newCuratedRuntime(t, unreadable)
	defer runtime.Shutdown()

	rejected := assemblyChainPlanStore(t, runtime, "s-curated-missing",
		`{"team_id":"t-curated-missing","milestones":[{"id":"m-1"}],"members":[{"role":"exec","plugins":["`+curatedRuntimeRoadmap+`"]}]}`)
	if rejected.err == nil {
		t.Fatal("目录读不到时，未定义的名字必须被拒（不许当成'目录里没有它'）")
	}
	assemblyChainWantError(t, "目录读不到（team_plan）", rejected.err,
		"没读到", "curated.yaml", roots[0], roots[1], "静默当空目录")
	if !rejected.empty {
		t.Fatal("被拒的计划不得落盘")
	}
}

// TestTeamPlanKeepsInstalledPluginsOnTheOldPath 钉住回归：已装插件**不问判决函数**、
// 走原路径（校验照做、计划照落、装配集合照写回）。这正是"行为不变"的那一半。
func TestTeamPlanKeepsInstalledPluginsOnTheOldPath(t *testing.T) {
	runtime := newCuratedRuntime(t, curatedRuntimeRead(t))
	defer runtime.Shutdown()
	// 判决函数被调用就说明已装插件被多问了一嘴——把"没被调用"变成可断言的事实。
	called := 0
	runtime.SetPluginUnassembledReason(func(name string, installed []string) error {
		called++
		return plugin.CuratedRead{Path: curatedRuntimePage}.Judge(name, installed)
	})

	accepted := assemblyChainPlanStore(t, runtime, "s-curated-ok",
		`{"team_id":"t-curated-ok","milestones":[{"id":"m-1"}],"members":[{"role":"exec","plugins":[" `+assemblyChainDocsPlugin+` "]}]}`)
	if accepted.err != nil {
		t.Fatalf("已装插件必须照旧装配得起来：%v", accepted.err)
	}
	if accepted.empty {
		t.Fatal("合法装配必须落盘")
	}
	if called != 0 {
		t.Fatalf("已装插件不该进判决函数（调用 %d 次）：装配路径必须逐字不变", called)
	}
}

// TestTeamPlanWithoutCuratedJudgeKeepsTheOldMessage 钉住"未注入 = 原口径"：
// 不注入判决函数（比如不带产品启动面的库用法）时，未知名仍旧是 validateMemberPlugins
// 那句"未定义"——不是"没注入就放行"。
func TestTeamPlanWithoutCuratedJudgeKeepsTheOldMessage(t *testing.T) {
	runtime := newAssemblyChainRuntime(t)
	defer runtime.Shutdown()
	if err := runtime.DefinePlugin("default", "Baseline", nil, nil); err != nil {
		t.Fatal(err)
	}

	rejected := assemblyChainPlanStore(t, runtime, "s-curated-none",
		`{"team_id":"t-curated-none","milestones":[{"id":"m-1"}],"members":[{"role":"exec","plugins":["ghost"]}]}`)
	if rejected.err == nil {
		t.Fatal("未知名在未注入判决函数时也必须被拒")
	}
	assemblyChainWantError(t, "未注入（team_plan）", rejected.err, "未定义", "ghost", "显式拒绝")
	if !rejected.empty {
		t.Fatal("被拒的计划不得落盘")
	}
}

// TestTeamPlanGrammarOutranksCuratedSemantics 钉住分层：超上限是**语法**层的话，
// 语义层（这个名字是不是 pending）不许盖住它——否则用户先被引去核路线图，修完才发现
// 真正的错是"声明了 4 个"。
func TestTeamPlanGrammarOutranksCuratedSemantics(t *testing.T) {
	runtime := newCuratedRuntime(t, curatedRuntimeRead(t))
	defer runtime.Shutdown()

	rejected := assemblyChainPlanStore(t, runtime, "s-curated-limit",
		`{"team_id":"t-curated-limit","milestones":[{"id":"m-1"}],"members":[{"role":"exec","plugins":[`+
			`"`+assemblyChainHostPlugin+`","`+assemblyChainDocsPlugin+`","`+assemblyChainOpsPlugin+`","default"]}]}`)
	if rejected.err == nil {
		t.Fatal("超上限必须被拒")
	}
	assemblyChainWantError(t, "超上限（team_plan）", rejected.err, "上限 3", "声明了 4 个", "不静默截断")
	if strings.Contains(rejected.err.Error(), "pending") {
		t.Fatalf("超限成员的话该由语法层说，不该被语义层的 pending 抢走：%v", rejected.err)
	}
	if !rejected.empty {
		t.Fatal("被拒的计划不得落盘")
	}
}

func errCuratedMissing() error { return curatedMissingError{} }

type curatedMissingError struct{}

func (curatedMissingError) Error() string {
	return "责任链上 2 个根都没有 curated.yaml（根解析见 pluginRootChain）"
}
