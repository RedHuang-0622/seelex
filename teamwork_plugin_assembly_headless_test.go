package main

// teamwork_plugin_assembly_headless_test.go — 「装配 plugin 的 teammate」在**组合根装配**上跑一遍。
//
// 分工（两份证据各回答一半问题）：
//   - `seelebridge/assembly_chain_real_partition_test.go`：用注入探针钉"这条链的函数对不对"
//     （不跑真回合、不开 worktree、不落计划）；
//   - 本文件：把同一件事放到**真实装配**上跑——真 store / 真 workspace / 真插件根
//     （`pluginRoots()` 解析到的那一份，与 main.go 的 initPluginSystem 同一条路）/ 真计划落盘 /
//     真 role session / 真 worktree，模型侧用脚本化 provider（不真调模型）。
//
// 为什么非要这一层：装配的**落点**是"员工那一轮的 system prompt 里有没有装配进来的技能目录"
// ——它只存在于 wire 上。只在函数层断言，"派发时带了、执行时丢了"永远测不到（上一轮的
// 对抗复核点名的就是这个失败模式）。
//
// 阶段（每段都留痕，失败时能指出是哪一跳断的）：
//
//	A1 计划与回执读数     —— members[].plugins 落盘；回执带 plugin_limit_per_teammate + 逐成员 assemblies
//	A2 装 impeccable 的 teammate —— 真 worker 回合的 system prompt 里带**它自己的**技能目录
//	A3 不装配的对照           —— 同一支团队里没声明 plugins 的那位：没有目录段（默认 default 面）
//	A4 当前划分上的错别字     —— 显式拒绝，且被拒的计划不覆盖已落盘的那一份

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/plugin"
	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/skill"
)

// assemblyProbeMarker 是塞进工作项 goal 的探针标记：worker 回合靠它辨认"这轮是不是我派的活"。
const assemblyProbeMarker = "ASSEMBLY-PROBE "

// assemblyView 是回执里一条逐成员装配读数（键与 runtime_role_plugins.go 的
// rolePluginAssemblyView 对齐）。
type assemblyView struct {
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
}

type assemblyReceipt struct {
	TeamID      string         `json:"team_id"`
	PluginLimit *int           `json:"plugin_limit_per_teammate"`
	Assemblies  []assemblyView `json:"assemblies"`
}

func TestTeamworkPluginAssemblyHeadlessSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("真实装配冒烟，short 模式跳过")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	const (
		uiProbe    = assemblyProbeMarker + "ui-eng"
		plainProbe = assemblyProbeMarker + "plain-eng"
	)
	provider := &teamworkSmokeProvider{leaderCalls: []scriptedResponse{
		// A1：计划 = 一名装 impeccable 的设计工程 + 一名不装配的对照（一道里程碑）。
		{toolName: "team_plan", toolArgs: `{"team_id":"plugin-assembly-team",` +
			`"members":[{"role":"ui-eng","plugins":["impeccable"]},{"role":"plain-eng","tools_policy":"readonly"}],` +
			`"milestones":[{"id":"m-chain","name":"装配链路"}]}`},
		{text: "leader：团队立好了"},
		// A2/A3：排活 + 派两名。goal 里带探针标记。
		{toolName: "team_work", toolArgs: `{"milestone":"m-chain","items":[` +
			`{"id":"wi-ui","role":"ui-eng","name":"装 impeccable 的设计工程","goal":"` + uiProbe +
			`：把你这一轮 system prompt 里『## Available Skills』段的原文贴出来；没有该段就写『没有该段』。"},` +
			`{"id":"wi-plain","role":"plain-eng","name":"不装配的对照","goal":"` + plainProbe + `：同上。"}]}`},
		{toolName: "team_dispatch", toolArgs: `{"item":"wi-ui","goal":"` + uiProbe + `"}`},
		{toolName: "team_dispatch", toolArgs: `{"item":"wi-plain","goal":"` + plainProbe + `"}`},
		{text: "leader：两名都派出去了"},
		// A4：当前划分上的错别字（未知名）。
		{toolName: "team_plan", toolArgs: `{"team_id":"plugin-assembly-typo",` +
			`"members":[{"role":"ui-eng","plugins":["ghost"]}],"milestones":[{"id":"m-x"}]}`},
		{text: "leader：知道了"},
	}}
	smoke := newTeamworkSmoke(t, provider)
	report := smoke.report
	attachRealSkills(t, smoke.harness.runtime)

	root := initSmokeGitRepo(t)
	if err := smoke.app.CreateWorkspace("plugin-assembly", root, ""); err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	if err := smoke.app.Submit(ctx, "开一个团队：一名装 impeccable 的设计工程 + 一名不装配的对照"); err != nil {
		t.Fatalf("A1 Submit: %v", err)
	}
	if err := smoke.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("A1 WaitForIdle: %v", err)
	}
	sessionID := smoke.app.Snapshot().Session.ID
	if sessionID == "" {
		t.Fatal("会话没有物化")
	}

	// ── A1：计划落盘 + 回执读数（真插件根下的三份划分）────────────────────
	receipt := decodeAssemblyReceipt(t, smoke.toolResultOf("team_plan"))
	if receipt.PluginLimit == nil || *receipt.PluginLimit != seelexctx.DefaultPluginsPerTeammate {
		t.Fatalf("A1 失败：回执没带上限读数（上限必须来自配置，不是散在代码里的常量）：%q",
			smoke.toolResultOf("team_plan"))
	}
	if len(receipt.Assemblies) != 2 {
		t.Fatalf("A1 失败：回执应逐成员一条读数，得 %d 条：%q", len(receipt.Assemblies), smoke.toolResultOf("team_plan"))
	}
	ui, plain := assemblyViewFor(t, receipt, "ui-eng"), assemblyViewFor(t, receipt, "plain-eng")
	if ui.Mode != "replace" || !assemblySameStrings(ui.Plugins, []string{"impeccable"}) || ui.PluginCount != 1 {
		t.Fatalf("A1 失败：装 impeccable 的成员读数不对：%+v", ui)
	}
	if ui.SkillCount != 1 || ui.SkillCatalogRunes <= 0 || ui.SkillCatalogTokensEst <= 0 {
		t.Fatalf("A1 失败：impeccable 的目录段读数不对（1 份技能、非零字节）：%+v", ui)
	}
	if ui.PluginFaceTools <= 0 || ui.PluginFaceTools != ui.TotalTools {
		t.Fatalf("A1 失败：impeccable 的 include/exclude 皆空 ⇒ 工具面不收窄（读数应等于全量）：%+v", ui)
	}
	if ui.Yellow {
		t.Fatalf("A1 失败：1 份技能的目录段不该报黄牌：%+v", ui)
	}
	if plain.Mode != "inherit-host" || len(plain.Plugins) != 0 || plain.SkillCount != 0 || plain.SkillCatalogRunes != 0 {
		t.Fatalf("A1 失败：不装配的成员必须显式写成 inherit-host 且不注入目录：%+v", plain)
	}
	if plain.PluginFaceTools != plain.TotalTools || plain.PluginFaceTools != ui.PluginFaceTools {
		t.Fatalf("A1 失败：不装配 = 宿主当前装配（启动基线 default，include/exclude 皆空）⇒ 面 = 全量、"+
			"且与装 impeccable 的那位相同：plain=%+v ui=%+v", plain, ui)
	}
	// 声明落盘（回执是投影，计划才是事实）。
	key, ok := smoke.keyFor(sessionID)
	if !ok {
		t.Fatal("A1 失败：会话没有项目作用域键")
	}
	plan, err := smoke.planStore.ReadPlan(ctx, key)
	if err != nil {
		t.Fatalf("A1 失败：读落盘计划：%v", err)
	}
	if plan.TeamID != "plugin-assembly-team" || len(plan.Members) != 2 {
		t.Fatalf("A1 失败：落盘计划不对：%+v", plan)
	}
	if !assemblySameStrings(plan.Members[0].Plugins, []string{"impeccable"}) || len(plan.Members[1].Plugins) != 0 {
		t.Fatalf("A1 失败：成员装配没落盘（或空集被写成了非空）：%+v", plan.Members)
	}
	report.add("A1 计划与回执读数",
		"上限=%d · ui-eng=%s/%v 技能=%d 目录=%d 字节 面=%d/%d · plain-eng=%s 面=%d/%d",
		*receipt.PluginLimit, ui.Mode, ui.Plugins, ui.SkillCount, ui.SkillCatalogRunes,
		ui.PluginFaceTools, ui.TotalTools, plain.Mode, plain.PluginFaceTools, plain.TotalTools)

	// ── A2/A3：排活 + 派两名 → 真 worker 回合 ────────────────────────────
	if err := smoke.app.Submit(ctx, "排活并派两名成员"); err != nil {
		t.Fatalf("A2 Submit: %v", err)
	}
	if err := smoke.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("A2 WaitForIdle: %v", err)
	}
	handles := assemblyDispatchHandles(t, smoke.toolResultsOf("team_dispatch"))
	if len(handles) != 2 {
		t.Fatalf("A2 失败：两次派发应拿到两个句柄，得 %d 个：%v", len(handles), handles)
	}
	for _, handle := range handles {
		job := smoke.waitJobTerminal(t, handle)
		if job.ExitCode != 0 {
			t.Fatalf("A2 失败：teammate 作业 %s exit=%d：%+v", handle, job.ExitCode, job)
		}
		report.add("A2 teammate 作业", "handle=%s state=%s exit=%d 工作项=%s", job.Handle, job.State, job.ExitCode, job.WorkItem)
	}

	samples := provider.workerSamples()
	uiSample := assemblySampleFor(t, samples, "ui-eng", uiProbe)
	plainSample := assemblySampleFor(t, samples, "plain-eng", plainProbe)

	// A2：装 impeccable 的那位——**它自己的**技能目录进了 system prompt。
	if !strings.Contains(uiSample.SystemPrompt, "## Available Skills") {
		t.Fatalf("A2 失败：装配 impeccable 的员工回合 system prompt 里没有技能目录段：%q", uiSample.SystemPrompt)
	}
	if !strings.Contains(uiSample.SystemPrompt, "impeccable") {
		t.Fatalf("A2 失败：目录段里没有 impeccable 这份技能：%q", uiSample.SystemPrompt)
	}
	if !strings.Contains(uiSample.SystemPrompt, "cannot switch plugins") {
		t.Fatalf("A2 失败：目录段缺少员工面的口径纠正（那一行是刻意追加的）：%q", uiSample.SystemPrompt)
	}
	// 装配是**替换**集合，不是叠加：default 的那 11 份技能一份都不该进来。
	for _, sentinel := range []string{"code-aesthetics", "cli-design"} {
		if strings.Contains(uiSample.SystemPrompt, sentinel) {
			t.Fatalf("A2 失败：default 的技能 %q 混进了装配 impeccable 的员工提示词：%q", sentinel, uiSample.SystemPrompt)
		}
	}
	report.add("A2 装配落到真回合", "ui-eng 的 system prompt %d 字节，带目录段（impeccable + 口径纠正）",
		len(uiSample.SystemPrompt))

	// A3：不装配的对照——同一条链、同一支团队，只有装配这一格不同。
	if strings.Contains(plainSample.SystemPrompt, "## Available Skills") {
		t.Fatalf("A3 失败：不装配的员工不该有技能目录段：%q", plainSample.SystemPrompt)
	}
	if strings.Contains(plainSample.SystemPrompt, "impeccable") {
		t.Fatalf("A3 失败：不装配的员工看到了 impeccable 的东西：%q", plainSample.SystemPrompt)
	}
	if uiSample.SystemPrompt == plainSample.SystemPrompt {
		t.Fatal("A3 失败：两轮 system prompt 逐字相同 ⇒ 装配没落到员工面上")
	}
	report.add("A3 不装配的对照", "plain-eng 的 system prompt %d 字节，无目录段（默认 default 面、目录不注入）",
		len(plainSample.SystemPrompt))

	// ── A4：当前划分上的错别字：显式拒绝，且不覆盖已落盘的计划 ─────────────
	if err := smoke.app.Submit(ctx, "再写一份计划，故意把一个插件名写错"); err != nil {
		t.Fatalf("A4 Submit: %v", err)
	}
	if err := smoke.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("A4 WaitForIdle: %v", err)
	}
	statuses := smoke.toolStatuses("team_plan")
	if len(statuses) < 2 || statuses[0] != "success" || statuses[len(statuses)-1] != "error" {
		t.Fatalf("A4 失败：期望 team_plan 两次调用 [success, error]，得到 %v\n会话逐行：\n%s",
			statuses, smoke.conversationDump())
	}
	board := smoke.harness.runtime.TeamworkBoardSnapshot(sessionID)
	if board == nil || board.TeamID != "plugin-assembly-team" || len(board.Members) != 2 {
		t.Fatalf("A4 失败：被拒的计划覆盖了已落盘的那一份：%+v", board)
	}
	if after, err := smoke.planStore.ReadPlan(ctx, key); err != nil || after.TeamID != "plugin-assembly-team" {
		t.Fatalf("A4 失败：落盘计划被拒绝路径改动：err=%v plan=%+v", err, after)
	}
	// 拒绝理由在事实面（工具状态 = error）；它在**呈现面**是否可见另记一条读数
	// （呈现层会把工具错误换成通用文案，这条不进断言，只留痕）。
	reason := smoke.toolResultMatching("team_plan", "ghost")
	report.add("A4 错别字被拒", "team_plan 状态=%v · 落盘计划仍是 plugin-assembly-team · 拒绝原文可见=%v（%q）",
		statuses, reason != "", firstLineOf(reason))

	t.Log("\n===== plugin assembly headless smoke =====\n" + report.text())
}

// ── 辅助 ────────────────────────────────────────────────────────────────────

func decodeAssemblyReceipt(t *testing.T, text string) assemblyReceipt {
	t.Helper()
	start := strings.Index(text, "{")
	if start < 0 {
		t.Fatalf("回执里没有 JSON：%q", text)
	}
	var receipt assemblyReceipt
	if err := json.Unmarshal([]byte(text[start:]), &receipt); err != nil {
		t.Fatalf("回执不是合法 JSON（%v）：%q", err, text)
	}
	return receipt
}

func assemblyViewFor(t *testing.T, receipt assemblyReceipt, role string) assemblyView {
	t.Helper()
	for _, view := range receipt.Assemblies {
		if view.Role == role {
			return view
		}
	}
	t.Fatalf("回执里没有成员 %q 的装配读数：%+v", role, receipt.Assemblies)
	return assemblyView{}
}

// assemblyDispatchHandles 从派发受理回执里取句柄（受理回执是 JSON，但工具结果可能被
// 呈现层加前缀，所以退一步抓 handle 字面量）。
func assemblyDispatchHandles(t *testing.T, receipts []string) []string {
	t.Helper()
	handles := make([]string, 0, len(receipts))
	for _, text := range receipts {
		var parsed struct {
			Handle string `json:"handle"`
		}
		start := strings.Index(text, "{")
		if start >= 0 {
			_ = json.Unmarshal([]byte(text[start:]), &parsed)
		}
		if parsed.Handle == "" {
			const marker = `"handle":"`
			index := strings.Index(text, marker)
			if index < 0 {
				t.Fatalf("派发受理回执里拿不到句柄：%q", text)
			}
			rest := text[index+len(marker):]
			parsed.Handle = rest[:strings.Index(rest, `"`)]
		}
		if parsed.Handle != "" {
			handles = append(handles, parsed.Handle)
		}
	}
	return handles
}

// attachRealSkills 把**真插件根**的技能装上 Runtime 的员工目录读面。
//
// 为什么要自己补这一跳：员工回合的技能目录读的是 `Runtime.skills`（见
// seelebridge/runtime_role_plugins.go 的 skillRegistryFor），组合根在 main.go:609 经
// `ApplyDeps(RuntimeDeps{SkillRegistry: …})` 把它装上；全链路 harness 只把 registry 交给
// 插件系统与应用，**没有装上 Runtime**。少了这一跳，装配进来的目录段会**静默为零**
// （skill_count=0、system prompt 里没有目录段），本用例就会把"基座缺一跳"误判成
// "装配没落到员工面"——所以这里按组合根那条路补上（同一条 `pluginRoots()` 责任链 +
// 同一条启动基线 `default`），而不是去改公共 harness（那会改到别的全链路用例的提示词字节）。
func attachRealSkills(t *testing.T, runtime *seelebridge.Runtime) {
	t.Helper()
	loaded, err := plugin.NewLoader(pluginRoots()...).LoadAll()
	if err != nil {
		t.Fatalf("加载真插件根失败：%v", err)
	}
	if len(loaded) == 0 {
		t.Fatal("责任链上一个插件都没加载到：装配用例没有可装配的对象")
	}
	registry := skill.NewRegistry()
	for _, item := range loaded {
		if err := registry.PublishPluginSkills(item.Name, item.Skills); err != nil {
			t.Fatalf("发布插件 %q 的技能失败：%v", item.Name, err)
		}
	}
	// 启动基线的技能集（宿主面 = default 那一份，与 activateDefaultPlugin 同姿势）。
	if err := registry.ActivatePluginSkills("default"); err != nil {
		t.Fatalf("激活 default 的技能失败：%v", err)
	}
	runtime.SetSkillRegistry(registry)
}

// assemblySampleFor 取某个 worker 回合的采样：按角色名辨认，并**用探针标记再确认一次**
// （否则"员工回合"可能来自别的用例或别的一轮活）。
func assemblySampleFor(t *testing.T, samples []workerSample, role, marker string) workerSample {
	t.Helper()
	for _, sample := range samples {
		if sample.Role == role && strings.Contains(sample.LastUser, marker) {
			return sample
		}
	}
	t.Fatalf("没有采到角色 %q / 标记 %q 的 worker 回合；采到 %d 次：%+v", role, marker, len(samples), samples)
	return workerSample{}
}

func assemblySameStrings(got, want []string) bool {
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
