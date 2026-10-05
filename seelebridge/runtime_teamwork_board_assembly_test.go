package seelebridge

// runtime_teamwork_board_assembly_test.go — 钉住 teammate 的**插件装配读数**进团队看板
// （2026-10-05，契约 docs/arch/team-board-gui-tui-contract.md §2）：
//
//   - 成员行的装配两格：`plugins`（声明面，从计划搬）+ `assembly`（生效面读数）；
//   - 三态可区分：装 A / 装 B / 空集（inherit-host），且读数**跟着装配集合走**
//     （不是三个人拿到同一个数——那种"读数"等于没读）；
//   - 空集必须**写出来**是 inherit-host，且 Assembly 非 nil（前端据此显示"继承宿主"
//     而不是"没有装配这一格"）；
//   - 失灵（声明还在、定义没了）在**看板上**也要说出来：PluginFaceFaulted=true、
//     PluginFaceTools=0，而且 Mode 仍 replace（失灵 ≠ 没装配）；
//   - 回执与看板**同一个类型**（别名）：两份 JSON 解到同一个类型上是同一个值。
//
// 判别力来源：不写死工具数（会随内置工具增长而变），而是从**这一轮的装配事实**读出来
// ——插件 include 前缀（doc_* / get_* / cad_*）与技能目录字节都由 runtime 现算。

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	seeletelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/sessionstore"
	"github.com/RedHuang-0622/seelex/skill"
)

const boardAssemblySessionID = "s-board-assembly"

// boardAssemblyRuntime 是这块看板的基座：宿主全局激活 cad（`cad_*`），另有两个只能按
// 会话装配拿到的插件 docs（`doc_*`，1 份技能）与 ops（`get_*`，2 份技能）。
//
// 三种面的**工具名集合**刻意不同（cad_draw / doc_read+doc_edit / get_time），这样"读数
// 跟着装配走"才有可断言的差异；技能数也不同（1 vs 2），mode 之外的判别力也有一条。
//
// 顺带把计划桩交回给用例：声明面的断言要对**落盘的计划**做，不能对投影自证。
func boardAssemblyRuntime(t *testing.T) (*Runtime, *memPlanStore) {
	t.Helper()
	runtime := newAssemblyChainRuntime(t)
	registry := skill.NewRegistry()
	registry.SetPluginSkills(assemblyChainHostPlugin, []skill.Skill{{Name: "cad-tips", Description: "draw precisely"}})
	registry.SetPluginSkills(assemblyChainDocsPlugin, []skill.Skill{{Name: "docs-guide", Description: "how to read the docs"}})
	registry.SetPluginSkills(assemblyChainOpsPlugin, []skill.Skill{
		{Name: "ops-runbook", Description: "how to run the ops"},
		{Name: "ops-deploy", Description: "how to deploy"},
	})
	runtime.SetSkillRegistry(registry)
	plans := &memPlanStore{}
	if err := runtime.SetTeamworkBackend(teamworkTestBackend(plans, boardAssemblySessionID)); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	return runtime, plans
}

// boardAssemblyPlan 立一支三成员的团队：exec 装 docs / pm 不装（空集）/ writer 装 ops。
func boardAssemblyPlan(t *testing.T, runtime *Runtime) string {
	t.Helper()
	ctx := seeletelemetry.WithSessionID(context.Background(), boardAssemblySessionID)
	receipt, err := runtime.teamPlanHandler(ctx, `{
		"team_id": "t-board-assembly",
		"milestones": [{"id": "m-1", "name": "装配读数"}],
		"members": [
			{"role": "exec", "plugins": ["docs"]},
			{"role": "pm"},
			{"role": "writer", "plugins": ["ops"]}
		]
	}`)
	if err != nil {
		t.Fatalf("team_plan: %v", err)
	}
	return receipt
}

// boardToolsWithPrefix 从**全量工具面**里数出某个插件 include 前缀下的工具数：它是本
// 基座里的装配事实（插件定义写的就是 `doc_*` 这类前缀），与装配判据的实现无关——
// 判别力因此不来自"复述实现"，也不写死 81/12 这类会随内置工具增长的数字。
func boardToolsWithPrefix(runtime *Runtime, prefix string) int {
	count := 0
	for _, tool := range runtime.AllTools() {
		if strings.HasPrefix(tool.Name, prefix) {
			count++
		}
	}
	return count
}

func boardMemberByRole(t *testing.T, members []dto.TeamworkMemberView, role string) dto.TeamworkMemberView {
	t.Helper()
	for _, member := range members {
		if member.Role == role {
			return member
		}
	}
	t.Fatalf("看板里没有成员 %q：%+v", role, members)
	return dto.TeamworkMemberView{}
}

// TestTeamworkBoardCarriesMemberAssemblyReadings 是主用例：三态可区分 + 与计划声明一致。
func TestTeamworkBoardCarriesMemberAssemblyReadings(t *testing.T) {
	runtime, plans := boardAssemblyRuntime(t)
	defer runtime.Shutdown()
	boardAssemblyPlan(t, runtime)

	board := runtime.TeamworkBoardSnapshot(boardAssemblySessionID)
	if board == nil {
		t.Fatal("有计划就必须给出看板投影")
	}
	if len(board.Members) != 3 {
		t.Fatalf("三名在编成员：%+v", board.Members)
	}
	// 计划才是事实：声明面必须与**落盘计划**里的那一份逐条一致（看板只是投影）。
	plan, err := plans.ReadPlan(context.Background(),
		sessionstore.Key{ProjectID: "p-team", SessionID: boardAssemblySessionID})
	if err != nil {
		t.Fatalf("读落盘计划：%v", err)
	}
	if len(plan.Members) != 3 {
		t.Fatalf("落盘计划少了成员：%+v", plan.Members)
	}
	for _, member := range board.Members {
		var declared []string
		for _, planned := range plan.Members {
			if planned.Role == member.Role {
				declared = planned.Plugins
			}
		}
		if !boardSameStrings(member.Plugins, declared) {
			t.Fatalf("成员 %q 的声明面必须逐条等于计划里的那一份：看板 %v / 计划 %v",
				member.Role, member.Plugins, declared)
		}
	}

	exec, pm, writer := boardMemberByRole(t, board.Members, "exec"),
		boardMemberByRole(t, board.Members, "pm"),
		boardMemberByRole(t, board.Members, "writer")

	total := len(runtime.AllTools())
	for _, member := range board.Members {
		if member.Assembly == nil {
			t.Fatalf("成员 %q 的装配读数缺失（看板必须给得出读数）：%+v", member.Role, member)
		}
		if member.Assembly.Role != member.Role {
			t.Fatalf("读数必须标明是谁的：%+v / %q", member.Assembly, member.Role)
		}
		if member.Assembly.TotalTools != total {
			t.Fatalf("全量工具面读数 %d，实际 %d（成员 %q）", member.Assembly.TotalTools, total, member.Role)
		}
		if member.Assembly.PluginCount != len(member.Plugins) || member.Assembly.PluginCount != len(member.Assembly.Plugins) {
			t.Fatalf("声明面三处（member.plugins / assembly.plugins / plugin_count）不一致：%+v", member)
		}
	}

	// ① 装 docs 的成员：replace + 声明面 + 目录读数 + 插件面收窄（doc_* 那一份）。
	docsCatalog := runtime.roleSkillCatalog([]string{assemblyChainDocsPlugin})
	docsFace := boardToolsWithPrefix(runtime, "doc_")
	if len(exec.Plugins) != 1 || exec.Plugins[0] != assemblyChainDocsPlugin {
		t.Fatalf("声明面必须从计划搬：%+v", exec.Plugins)
	}
	if exec.Assembly.Mode != assemblyModeReplace {
		t.Fatalf("声明了集合 ⇒ replace：%+v", exec.Assembly)
	}
	if exec.Assembly.SkillCount != 1 || exec.Assembly.SkillCatalogRunes != len([]rune(docsCatalog)) {
		t.Fatalf("目录段读数必须与**真会进 system prompt 的那一份**一致：%+v（目录 %d runes）",
			exec.Assembly, len([]rune(docsCatalog)))
	}
	if exec.Assembly.SkillCatalogTokensEst != (exec.Assembly.SkillCatalogRunes+3)/4 {
		t.Fatalf("token 估算口径不对：%+v", exec.Assembly)
	}
	if exec.Assembly.PluginFaceTools != docsFace || docsFace == total {
		t.Fatalf("插件面读数 %d，want %d（docs 的 include=doc_*，必须真的收窄且不等于全量 %d）",
			exec.Assembly.PluginFaceTools, docsFace, total)
	}

	// ② 空集成员：**写出来**是 inherit-host，不靠字段缺失暗示；读数仍是真数（宿主面）。
	hostFace := boardToolsWithPrefix(runtime, "cad_")
	if pm.Plugins != nil || len(pm.Assembly.Plugins) != 0 || pm.Assembly.PluginCount != 0 {
		t.Fatalf("空集 = 不覆盖：声明面必须是空的：%+v", pm)
	}
	if pm.Assembly.Mode != assemblyModeInheritHost {
		t.Fatalf("空集必须显式写 inherit-host：%+v", pm.Assembly)
	}
	if pm.Assembly.SkillCount != 0 || pm.Assembly.SkillCatalogRunes != 0 || pm.Assembly.SkillCatalogTokensEst != 0 {
		t.Fatalf("inherit-host 不注入技能目录（运行事实）：%+v", pm.Assembly)
	}
	if pm.Assembly.PluginFaceTools != hostFace {
		t.Fatalf("inherit-host 的插件面 = **宿主当前装配**收窄后的真数 %d，得 %d（不是 0 占位）",
			hostFace, pm.Assembly.PluginFaceTools)
	}
	if pm.Assembly.PluginFaceFaulted || pm.Assembly.Yellow {
		t.Fatalf("空集不是失灵、小目录不黄牌：%+v", pm.Assembly)
	}

	// ③ 装 ops 的成员：第三个面，且与 exec 的读数**不同**（判别力：读数若被写成常量，
	// 这里就会相等）。
	opsCatalog := runtime.roleSkillCatalog([]string{assemblyChainOpsPlugin})
	if writer.Assembly.Mode != assemblyModeReplace || writer.Assembly.SkillCount != 2 {
		t.Fatalf("writer 装 ops（2 份技能）：%+v", writer.Assembly)
	}
	if writer.Assembly.SkillCatalogRunes != len([]rune(opsCatalog)) {
		t.Fatalf("writer 的目录读数 %d，want %d", writer.Assembly.SkillCatalogRunes, len([]rune(opsCatalog)))
	}
	if writer.Assembly.PluginFaceTools != boardToolsWithPrefix(runtime, "get_") {
		t.Fatalf("ops 的插件面应收窄到 get_*：%+v", writer.Assembly)
	}
	if exec.Assembly.PluginFaceTools == pm.Assembly.PluginFaceTools ||
		exec.Assembly.SkillCatalogRunes == writer.Assembly.SkillCatalogRunes {
		t.Fatalf("三种装配必须读出不同的数（否则读数等于没读）：exec=%+v pm=%+v writer=%+v",
			exec.Assembly, pm.Assembly, writer.Assembly)
	}
}

// TestTeamworkBoardAssemblyFaultIsVisible 钉"失灵 ≠ 没装配"：声明过的插件被撤掉之后，
// 看板上仍是 replace + 那份声明，但工具面读数为 0 且**说清失灵**——前端不会把"0 个工具"
// 读成"这个人没装东西"。
func TestTeamworkBoardAssemblyFaultIsVisible(t *testing.T) {
	runtime, _ := boardAssemblyRuntime(t)
	defer runtime.Shutdown()
	boardAssemblyPlan(t, runtime)

	before := boardMemberByRole(t, runtime.TeamworkBoardSnapshot(boardAssemblySessionID).Members, "exec")
	if before.Assembly.PluginFaceFaulted || before.Assembly.PluginFaceTools == 0 {
		t.Fatalf("前置：撤销之前 docs 是可用的：%+v", before.Assembly)
	}

	// 声明还在计划里，定义没了（root 撤销 / 名字漂了）。
	runtime.UndefinePlugin(assemblyChainDocsPlugin)

	after := boardMemberByRole(t, runtime.TeamworkBoardSnapshot(boardAssemblySessionID).Members, "exec")
	if len(after.Plugins) != 1 || after.Plugins[0] != assemblyChainDocsPlugin {
		t.Fatalf("失灵时声明面仍是计划里那一份（失灵 ≠ 没声明）：%+v", after.Plugins)
	}
	if after.Assembly.Mode != assemblyModeReplace {
		t.Fatalf("失灵不是「没装配」：mode 仍是 replace，得 %q", after.Assembly.Mode)
	}
	if !after.Assembly.PluginFaceFaulted || after.Assembly.PluginFaceTools != 0 {
		t.Fatalf("失灵必须显形为空工具面：%+v", after.Assembly)
	}
	if len(after.Assembly.PluginFaceMissing) != 1 || after.Assembly.PluginFaceMissing[0] != assemblyChainDocsPlugin {
		t.Fatalf("失灵读数必须点名是谁没了：%+v", after.Assembly.PluginFaceMissing)
	}
	for _, want := range []string{"失灵", assemblyChainDocsPlugin, "工具面为空"} {
		if !strings.Contains(after.Assembly.PluginFaceNote, want) {
			t.Fatalf("失灵说明 %q 必须包含 %q", after.Assembly.PluginFaceNote, want)
		}
	}
	// 阴性对照：同一个看板上，不装配的那位（inherit-host）读数不受影响 ⇒ 上面那条不是
	// "整个读数坏掉"式的空断言。
	pm := boardMemberByRole(t, runtime.TeamworkBoardSnapshot(boardAssemblySessionID).Members, "pm")
	if pm.Assembly.PluginFaceFaulted || pm.Assembly.PluginFaceTools != boardToolsWithPrefix(runtime, "cad_") {
		t.Fatalf("别人的装配不吃这个失灵：%+v", pm.Assembly)
	}
}

// TestTeamworkBoardAssemblySharesOneShapeWithReceipt 是**同一形状**的回归断言：
// 回执 JSON 里的那一条与看板 JSON 里的那一条，解到**同一个类型**上是同一个值。
//
// 为什么钉在 JSON 上而不是只钉类型：类型别名能挡住"两份手抄结构体"，但挡不住有人在
// dto 上加一个 tag 只在一条路径上有值——两条路径都在这里被解进同一份字段，任何一侧
// 少写/写错一个键都会让这条断言红。
func TestTeamworkBoardAssemblySharesOneShapeWithReceipt(t *testing.T) {
	// 编译期 + 运行期两条：别名（`=`）意味着它们本来就是同一个类型。
	var _ dto.PluginAssemblyView = rolePluginAssemblyView{}
	var _ rolePluginAssemblyView = dto.PluginAssemblyView{}
	if reflect.TypeOf(rolePluginAssemblyView{}) != reflect.TypeOf(dto.PluginAssemblyView{}) {
		t.Fatal("回执与看板必须共用同一个装配读数类型（别名，不是同形的新类型）")
	}

	runtime, _ := boardAssemblyRuntime(t)
	defer runtime.Shutdown()
	receipt := boardAssemblyPlan(t, runtime)

	var decoded struct {
		Assemblies []dto.PluginAssemblyView `json:"assemblies"`
	}
	if err := json.Unmarshal([]byte(receipt), &decoded); err != nil {
		t.Fatalf("team_plan 回执不是可解析的 JSON（%v）：%s", err, receipt)
	}
	board := runtime.TeamworkBoardSnapshot(boardAssemblySessionID)
	if board == nil {
		t.Fatal("有计划就必须给出看板投影")
	}
	if len(decoded.Assemblies) != len(board.Members) {
		t.Fatalf("回执 %d 条读数、看板 %d 行成员：%s", len(decoded.Assemblies), len(board.Members), receipt)
	}
	for _, member := range board.Members {
		var fromReceipt *dto.PluginAssemblyView
		for index := range decoded.Assemblies {
			if decoded.Assemblies[index].Role == member.Role {
				fromReceipt = &decoded.Assemblies[index]
			}
		}
		if fromReceipt == nil {
			t.Fatalf("回执里没有成员 %q 的读数：%s", member.Role, receipt)
		}
		if member.Assembly == nil {
			t.Fatalf("看板里没有成员 %q 的读数：%+v", member.Role, member)
		}
		if !reflect.DeepEqual(*fromReceipt, *member.Assembly) {
			t.Fatalf("成员 %q 的读数在回执与看板之间不一致（同一份声明、同一判据 ⇒ 必须同值）：\n"+
				"回执 %+v\n看板 %+v", member.Role, *fromReceipt, *member.Assembly)
		}
	}
}

// TestTeamBoardArchiveCarriesSameAssemblyReadings 钉**存档**那一侧也带读数：存档与下发
// 走同一条组装路径（buildTeamworkBoardView），所以重启恢复出来的看板与活体下发的同形
// ——包括装配两格（少带一格就是"恢复之后装配格消失"这种只在重启后才出现的缺陷）。
func TestTeamBoardArchiveCarriesSameAssemblyReadings(t *testing.T) {
	runtime := newAssemblyChainRuntime(t)
	defer runtime.Shutdown()
	registry := skill.NewRegistry()
	registry.SetPluginSkills(assemblyChainDocsPlugin, []skill.Skill{{Name: "docs-guide", Description: "how to read the docs"}})
	runtime.SetSkillRegistry(registry)
	boards := &memBoardStore{}
	if err := runtime.SetTeamworkBackend(archiveBackend(&memPlanStore{}, boards, boardAssemblySessionID)); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	boardAssemblyPlan(t, runtime)

	key := sessionstore.Key{ProjectID: "p-team", SessionID: boardAssemblySessionID}
	meta, ok := boards.latest(key)
	if !ok {
		t.Fatal("team_plan 之后必须有存档（写侧与下发同一条组装路径）")
	}
	var archived dto.TeamworkBoardView
	if err := json.Unmarshal(meta.Snapshot, &archived); err != nil {
		t.Fatalf("存档载荷不是可解析的看板（%v）：%s", err, meta.Snapshot)
	}
	live := runtime.TeamworkBoardSnapshot(boardAssemblySessionID)
	if live == nil {
		t.Fatal("有计划就必须给出看板投影")
	}
	if len(archived.Members) != len(live.Members) {
		t.Fatalf("存档 %d 行成员、活体 %d 行：%s", len(archived.Members), len(live.Members), meta.Snapshot)
	}
	for index, member := range live.Members {
		stored := archived.Members[index]
		if !boardSameStrings(stored.Plugins, member.Plugins) {
			t.Fatalf("存档的声明面与活体不一致：%v vs %v", stored.Plugins, member.Plugins)
		}
		if stored.Assembly == nil || member.Assembly == nil || !reflect.DeepEqual(*stored.Assembly, *member.Assembly) {
			t.Fatalf("存档的装配读数与活体不一致（成员 %q）：\n存档 %+v\n活体 %+v",
				member.Role, stored.Assembly, member.Assembly)
		}
	}
}

func boardSameStrings(got, want []string) bool {
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
