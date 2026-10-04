package seelebridge

// runtime_role_plugins_fault_test.go — 对抗复核三条缺陷的回归钉子（C / D / G）。
//
//	C. **读数与运行事实必须同一判据**：回执里那个"插件面有多少个工具"的数，与运行期
//	   真的算出来的工具面必须是**同一个判据**算出来的。修前两处会相反：
//	     · `len(plugins) > 0` 才读数 ⇒ `inherit-host` 成员报 0/0（而继承面其实非零）；
//	     · `defs, _ := pluginDefs(...)` 吞掉 missing ⇒ 插件被撤销后读数报**满面**，
//	       而运行面是**空**（plugin.VisibleName 对空 defs 返回 true 的那条规则是给
//	       "没装配"用的，不该被"装配了但定义没了"复用）。
//	D. **重复声明不能被静默合并**：唯一生效的口径必须是"显式拒绝"（与"不静默"同口径）。
//	G. **派发三跳补钉子**：team_plan 的声明要一路断到 `WorkerRequest.Plugins` 与
//	   最终进 ctx 的装配集合——"派发时带了、执行时丢了"是本特性自定的失败模式。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/types"

	seeletelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/seelebridge/tools"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// ── C：读数与运行同判据 ─────────────────────────────────────────────────────

// TestAssemblyReadingMatchesRuntimeFaceForInheritedMember 钉住 C 的前半：**没声明装配**
// （inherit-host）也要有真读数——继承面多少就报多少，不是 0/0 的占位。
//
// 判别力：修前 `PluginFaceTools`/`TotalTools` 在 inherit-host 上是 0/0，与运行期
// 实际可见的工具面（宿主全局装配收窄后的那一份）根本不相等。
func TestAssemblyReadingMatchesRuntimeFaceForInheritedMember(t *testing.T) {
	runtime := newAssemblyChainRuntime(t)
	defer runtime.Shutdown()
	all := assemblyChainTools()

	view := runtime.assemblyViews([]sessionstore.TeamworkMember{{Role: "pm"}})[0]
	if view.Mode != assemblyModeInheritHost {
		t.Fatalf("没声明 = inherit-host，得 %q", view.Mode)
	}
	// 运行事实：空集那一轮走的就是宿主面（与 PluginFilter 逐字一致的那条早退路）。
	fact := assemblyChainNames(runtime.visibilityPolicy.Filter(context.Background(), all))
	if view.TotalTools != len(all) {
		t.Fatalf("读数必须知道全量工具面有多大（%d），得 %d", len(all), view.TotalTools)
	}
	if view.PluginFaceTools != len(fact) {
		t.Fatalf("inherit-host 的读数 %d 与运行事实 %d 不是同一判据（宿主面：%v）",
			view.PluginFaceTools, len(fact), fact)
	}
	if view.PluginFaceTools == len(all) {
		t.Fatalf("基座前提不成立：宿主全局激活 cad，继承面必须被收窄，得 %d/%d", view.PluginFaceTools, len(all))
	}
	if view.PluginFaceFaulted || view.PluginFaceNote != "" {
		t.Fatalf("没声明装配不是失灵，得 %#v", view)
	}
	// 目录段仍然是"不注入"（这才是空集在技能面上的事实）。
	if view.SkillCatalogRunes != 0 || view.PluginCount != 0 {
		t.Fatalf("inherit-host 不注入目录、没有声明集合，得 %#v", view)
	}
}

// TestAssemblyReadingAgreesWithRuntimeAfterPluginRevoked 钉住 C 的后半 + ③：
// **声明过的插件之后被撤销**（root 撤销 / 名字漂了）时，
//   - 运行面 = 空（失灵，不放宽成宿主面）；
//   - 读数 = 0，**且说清失灵**（不是报满面，也不是假装"没装配"）；
//   - 两者相等（回执不得与运行事实相反）。
func TestAssemblyReadingAgreesWithRuntimeAfterPluginRevoked(t *testing.T) {
	runtime := newAssemblyChainRuntime(t)
	defer runtime.Shutdown()
	member := sessionstore.TeamworkMember{Role: "exec", Plugins: []string{assemblyChainDocsPlugin}}
	all := assemblyChainTools()
	ctx := tools.WithRolePlugins(context.Background(), member.Plugins)

	// 声明时：读数与运行面一致且非零（这一段是基线，证明后面的差异不是"读数一直为 0"）。
	before := runtime.assemblyViews([]sessionstore.TeamworkMember{member})[0]
	beforeFact := assemblyChainNames(runtime.visibilityPolicy.Filter(ctx, all))
	if before.PluginFaceFaulted || before.PluginFaceTools != len(beforeFact) || len(beforeFact) == 0 {
		t.Fatalf("声明时的读数与运行面必须一致且非零：读数 %#v，运行面 %v", before, beforeFact)
	}

	// root 撤销这个插件；仍按**同一份声明**使用（这正是"撤销之后再用它"的现场）。
	runtime.UndefinePlugin(assemblyChainDocsPlugin)

	afterFact := assemblyChainNames(runtime.visibilityPolicy.Filter(ctx, all))
	if len(afterFact) != 0 {
		t.Fatalf("定义没了必须失灵（空面），得 %v", afterFact)
	}
	after := runtime.assemblyViews([]sessionstore.TeamworkMember{member})[0]
	if after.PluginFaceTools != len(afterFact) {
		t.Fatalf("失灵后读数 %d 与运行事实 %d 相反（修前的表现：读数报满面、运行面为空）",
			after.PluginFaceTools, len(afterFact))
	}
	if !after.PluginFaceFaulted || !assemblyChainSameStrings(after.PluginFaceMissing, []string{assemblyChainDocsPlugin}) {
		t.Fatalf("失灵必须**可观察**（哪个名字没了），得 %#v", after)
	}
	for _, want := range []string{"声明", assemblyChainDocsPlugin, "失灵", "工具面为空"} {
		if !strings.Contains(after.PluginFaceNote, want) {
			t.Fatalf("失灵读数 %q 必须包含 %q（回执/日志要说清：声明 X，现已失灵，工具面为空）", after.PluginFaceNote, want)
		}
	}
	// 失灵不等于"没装配"：模式仍是 replace（声明还在），集合还是那一份。
	if after.Mode != assemblyModeReplace || !assemblyChainSameStrings(after.Plugins, []string{assemblyChainDocsPlugin}) {
		t.Fatalf("失灵不是「撤销声明」：得 %#v", after)
	}
}

// ── D：重复声明显式拒绝 ─────────────────────────────────────────────────────

// TestDuplicatePluginDeclarationIsRejectedNotMerged 钉住 D：重复声明的唯一生效口径是
// **显式拒绝**（不静默合并、不静默截断），且被拒的计划不落盘。
//
// 修前的现场：`team_plan` → `dto.NormalizePlugins`（静默去重）→ 存储校验里那条"插件
// 重复（显式拒绝，不静默去重）"**不可达**，于是"未知名/超限显式拒绝"与"重复静默合并"
// 两个口径同时生效——计划自己写的话与运行实现相反。
func TestDuplicatePluginDeclarationIsRejectedNotMerged(t *testing.T) {
	runtime := newAssemblyChainRuntime(t)
	defer runtime.Shutdown()
	if err := runtime.DefinePlugin("default", "Baseline", nil, nil); err != nil {
		t.Fatal(err)
	}
	rejected := assemblyChainPlanStore(t, runtime, "s-duplicate",
		`{"team_id":"t-dup","milestones":[{"id":"m-1"}],"members":[{"role":"exec","plugins":["docs"," docs ","docs"]}]}`)
	if rejected.err == nil {
		t.Fatal("重复声明必须被显式拒绝（唯一生效口径 = 显式拒绝，不静默合并）")
	}
	assemblyChainWantError(t, "重复声明（team_plan）", rejected.err, "重复", "docs", "不静默")
	if !rejected.empty {
		t.Fatal("被拒的计划不得落盘")
	}
	// 阴性对照：同一份声明去重之后（只写一次）必须收——否则上面的拒绝可以靠"什么都拒"通过。
	accepted := assemblyChainPlanStore(t, runtime, "s-duplicate-ok",
		`{"team_id":"t-dup-ok","milestones":[{"id":"m-1"}],"members":[{"role":"exec","plugins":[" docs "]}]}`)
	if accepted.err != nil || accepted.empty {
		t.Fatalf("去空白后的合法声明必须收：err=%v empty=%v", accepted.err, accepted.empty)
	}
	// 空白项仍然只是"脏数据被清掉"（不是重复声明的同类）：清完为空 = 不覆盖。
	blank := assemblyChainPlanStore(t, runtime, "s-blank",
		`{"team_id":"t-blank","milestones":[{"id":"m-1"}],"members":[{"role":"exec","plugins":["   "]}]}`)
	if blank.err != nil || blank.empty {
		t.Fatalf("纯空白项清完为空 = 不覆盖，得 err=%v empty=%v", blank.err, blank.empty)
	}
}

// ── G：派发三跳到 WorkerRequest.Plugins / 本轮 ctx ────────────────────────────

// pluginChainRound 是探针记录的一轮角色回合：这一轮 ctx 里的装配集合 + 这一轮**真的**
// 可见的工具面（用运行期的 Policy 实例现算，即生产那条路）。
type pluginChainRound struct {
	ctxPlugins []string
	visible    []string
}

// pluginChainProbe 是最小角色引擎：把"作业真的跑起来了吗、跑起来时装配是什么"记下来。
// 它经 SetRoleEngineFactory 注入，任何角色会话号都发它（Work Item 会话号是派发时派生的，
// 用例不该去猜那个字符串）。
type pluginChainProbe struct {
	runtime *Runtime
	all     []types.Tool
	rounds  chan pluginChainRound
}

func newPluginChainProbe(runtime *Runtime) *pluginChainProbe {
	return &pluginChainProbe{runtime: runtime, all: assemblyChainTools(), rounds: make(chan pluginChainRound, 8)}
}

func (p *pluginChainProbe) SessionID() string        { return "role-plugin-chain" }
func (p *pluginChainProbe) SetSystemPrompt(string)   {}
func (p *pluginChainProbe) SetMaxLoops(int)          {}
func (p *pluginChainProbe) ClearHistory()            {}
func (p *pluginChainProbe) History() []types.Message { return nil }

func (p *pluginChainProbe) ChatStream(ctx context.Context, _ string, _ func(string)) (string, error) {
	p.rounds <- pluginChainRound{
		ctxPlugins: tools.RolePluginsFromContext(ctx),
		visible:    assemblyChainNames(p.runtime.visibilityPolicy.Filter(ctx, p.all)),
	}
	return "done", nil
}

// nextRound 等这一轮真的跑起来（作业是后台跑的：受理即返回，不等待）。
func (p *pluginChainProbe) nextRound(t *testing.T) pluginChainRound {
	t.Helper()
	select {
	case round := <-p.rounds:
		return round
	case <-time.After(5 * time.Second):
		t.Fatal("派出去的作业没有在窗口内跑起角色回合（三跳里有一跳断了）")
		return pluginChainRound{}
	}
}

// TestDispatchChainCarriesPluginsFromPlanToWorkerContext 是 G：从 `team_plan` 一路断到
// `WorkerRequest.Plugins`（items.go 的 DispatchItem）与**本轮 ctx 的装配集合**
// （runtime_teamwork.go 的 workerRoleRoundSpec）。
//
// 判别力：中途任何一跳丢掉 Plugins，`rolePluginAssembly` 就落到"角色自带"（本用例没注入
// ⇒ 空集），探针这一轮的 ctxPlugins 会是 nil、可见面会是宿主面（cad_*）——与断言相反。
func TestDispatchChainCarriesPluginsFromPlanToWorkerContext(t *testing.T) {
	runtime := newAssemblyChainRuntime(t)
	defer runtime.Shutdown()
	const sessionID = "s-plugin-chain"
	store := &memPlanStore{}
	if err := runtime.SetTeamworkBackend(teamworkTestBackend(store, sessionID)); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	probe := newPluginChainProbe(runtime)
	runtime.SetRoleEngineFactory(func(string) (roleEngine, error) { return probe, nil })
	ctx := seeletelemetry.WithSessionID(context.Background(), sessionID)

	// ① 声明面：leader 写 plan。
	if _, err := runtime.teamPlanHandler(ctx, `{
		"team_id": "t-plugin-chain",
		"milestones": [{"id": "m-1", "name": "装配链路"}],
		"members": [{"role": "exec", "plugins": ["docs"]}]
	}`); err != nil {
		t.Fatalf("team_plan: %v", err)
	}
	// ② 排活面。
	if _, err := runtime.teamWorkHandler(ctx, `{
		"milestone": "m-1",
		"items": [{"id": "wi-1", "role": "exec", "name": "把声明带进载荷"}]
	}`); err != nil {
		t.Fatalf("team_work: %v", err)
	}
	// ③ 派发面（受理即返回，作业后台跑）。
	receipt, err := runtime.teamDispatchHandler(ctx, `{"item":"wi-1"}`)
	if err != nil {
		t.Fatalf("team_dispatch: %v", err)
	}
	// ④ 执行面：这一轮真的跑起来了，且这一轮的装配就是声明的那一份。
	round := probe.nextRound(t)
	if !assemblyChainSameStrings(round.ctxPlugins, []string{assemblyChainDocsPlugin}) {
		t.Fatalf("这一轮的 ctx 装配 = %v，want [docs]（三跳里有一跳把 Plugins 丢了）", round.ctxPlugins)
	}
	if !assemblyChainSameStrings(round.visible, []string{"doc_read", "doc_edit"}) {
		t.Fatalf("这一轮可见工具 = %v，want docs 面的 [doc_read doc_edit]（装配没落到工具面）", round.visible)
	}
	// ⑤ 回执读数与运行事实同判据（同一份声明的两次计算）。
	var dispatched struct {
		Item       string `json:"item"`
		PluginFace struct {
			Role            string   `json:"role"`
			Mode            string   `json:"mode"`
			Plugins         []string `json:"plugins"`
			PluginFaceTools int      `json:"plugin_face_tools"`
			TotalTools      int      `json:"total_tools"`
			Faulted         bool     `json:"plugin_face_faulted"`
			Note            string   `json:"plugin_face_note"`
		} `json:"plugin_face"`
	}
	if err := json.Unmarshal([]byte(receipt), &dispatched); err != nil {
		t.Fatalf("派发回执不是可解析的 JSON（%v）：%s", err, receipt)
	}
	if dispatched.PluginFace.PluginFaceTools != len(round.visible) || dispatched.PluginFace.Faulted {
		t.Fatalf("派发回执的装配读数 %#v 与这一轮的实际工具面 %v 不是同一判据",
			dispatched.PluginFace, round.visible)
	}
}

// TestDispatchReceiptReportsFaultedFaceAfterRevoke 是 C ③ 的**端到端**版本：先把插件
// 撤掉，**再派发**去用它——回执必须说清"失灵、工具面为空"，而不是报满面；运行面必须
// 真的是空的；两个数必须相等。
func TestDispatchReceiptReportsFaultedFaceAfterRevoke(t *testing.T) {
	runtime := newAssemblyChainRuntime(t)
	defer runtime.Shutdown()
	const sessionID = "s-plugin-revoked"
	store := &memPlanStore{}
	if err := runtime.SetTeamworkBackend(teamworkTestBackend(store, sessionID)); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	probe := newPluginChainProbe(runtime)
	runtime.SetRoleEngineFactory(func(string) (roleEngine, error) { return probe, nil })
	ctx := seeletelemetry.WithSessionID(context.Background(), sessionID)

	if _, err := runtime.teamPlanHandler(ctx, `{
		"team_id": "t-plugin-revoked",
		"milestones": [{"id": "m-1", "name": "撤销之后"}],
		"members": [{"role": "exec", "plugins": ["docs"]}]
	}`); err != nil {
		t.Fatalf("team_plan: %v", err)
	}
	if _, err := runtime.teamWorkHandler(ctx, `{
		"milestone": "m-1",
		"items": [{"id": "wi-revoked", "role": "exec", "name": "撤销之后再用它"}]
	}`); err != nil {
		t.Fatalf("team_work: %v", err)
	}
	// 声明仍然在计划里，插件定义没了（root 撤销 / 名字漂了）。
	runtime.UndefinePlugin(assemblyChainDocsPlugin)

	receipt, err := runtime.teamDispatchHandler(ctx, `{"item":"wi-revoked"}`)
	if err != nil {
		t.Fatalf("team_dispatch 不该因为插件失灵而拒收（失灵是运行面的事实，排活已成立）：%v", err)
	}
	round := probe.nextRound(t)
	if len(round.visible) != 0 {
		t.Fatalf("失灵必须空工具面（不放宽成宿主面），得 %v", round.visible)
	}
	if !assemblyChainSameStrings(round.ctxPlugins, []string{assemblyChainDocsPlugin}) {
		t.Fatalf("失灵时声明仍然要进 ctx（是「失灵」不是「没声明」），得 %v", round.ctxPlugins)
	}

	var dispatched struct {
		PluginFace struct {
			Mode            string   `json:"mode"`
			Plugins         []string `json:"plugins"`
			PluginFaceTools int      `json:"plugin_face_tools"`
			Faulted         bool     `json:"plugin_face_faulted"`
			Missing         []string `json:"plugin_face_missing"`
			Note            string   `json:"plugin_face_note"`
		} `json:"plugin_face"`
	}
	if err := json.Unmarshal([]byte(receipt), &dispatched); err != nil {
		t.Fatalf("派发回执不是可解析的 JSON（%v）：%s", err, receipt)
	}
	face := dispatched.PluginFace
	if face.PluginFaceTools != len(round.visible) {
		t.Fatalf("回执报 %d 个工具面，运行面是 %d 个——回执不得与运行事实相反（修前报的是满面）",
			face.PluginFaceTools, len(round.visible))
	}
	if !face.Faulted || !assemblyChainSameStrings(face.Missing, []string{assemblyChainDocsPlugin}) {
		t.Fatalf("回执必须标明失灵与失灵的名字，得 %#v", face)
	}
	for _, want := range []string{"失灵", assemblyChainDocsPlugin, "工具面为空"} {
		if !strings.Contains(face.Note, want) {
			t.Fatalf("失灵读数 %q 必须包含 %q", face.Note, want)
		}
	}
	if face.Mode != assemblyModeReplace {
		t.Fatalf("失灵不是「没装配」：模式仍是 replace，得 %q", face.Mode)
	}
}
