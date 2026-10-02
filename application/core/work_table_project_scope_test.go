package core

// S2 靶场：**切换项目**之后的跨项目工作表格污染（请求尾部打点块）。
//
// 症状（红灯复现，见 TestProjectSwitchDoesNotInjectForeignWorkTableRows）：
// 会话 A 绑定项目 A 并在实时注册表里带着活动任务；用户切到项目 B 时，绑定路径
// （workspace_usecase.go 的 bindWorkspaceInfo，startFreshSession 分支）新建并
// 激活了一条独立会话，但**没有像 /new、热挂载、冷恢复、冷加载那样调用
// SwitchSessionTasks**——于是注册表指针仍停在项目 A 的会话上。项目 B 新会话的
// 首轮请求装配时，workTableTraceBlockFor(新会话) 走「视图会话 = 实时注册表」
// 那一条读面，把项目 A 的活动任务行前置进 currentInput：项目 B 的上下文里出现
// 别的项目的打点（跨项目上下文污染），而新会话自己的行（没有）当然也看不到。
//
// 与压缩时机的关系（两条装配路径都要复现，见 ...AcrossCompaction）：
//   ① 未压缩装配：块直接前置进首轮请求（污染原样发给 provider）；
//   ② 压缩时机：块的注入在压缩判据**之前**（coordinator.go 里先拼 currentInput
//      再算 rawTokens / 压缩），所以污染同时进判据量、进压缩后的请求——压缩
//      不会把它洗掉，下一轮装配照样重新拼一遍（污染按轮持续）。
//
// 边界（本靶场刻意钉住，避免"修成另一个 bug"）：
//   - 工作表格本体是**项目/全局台账**，切项目不得让旧项目的行从表里消失
//     （TestProjectSwitchKeepsForeignRowsInLedger）；
//   - 打点块是**会话级实发面**，只含「正在组装这个请求的会话」自己的行。

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// foreignTaskLabel 是项目 A 的活动任务标题（断言"它是否被注入项目 B 的上下文"）。
const foreignTaskLabel = "A 项目的活动任务"

// projectSwitchHarness 是「两个项目 + 一次项目切换」的最小现场：会话先在项目 A
// 跑一轮（让引擎带历史、切换才会新建独立会话），再切到项目 B。
type projectSwitchHarness struct {
	service    *Service
	runtime    *fakeRuntime
	engine     *fakeEngine
	workspaces *multiProjectWorkspace
	sessionA   string
	sessionB   string
}

func newProjectSwitchHarness(t *testing.T) *projectSwitchHarness {
	t.Helper()
	engine := &fakeEngine{chunks: []string{"ok"}}
	runtime := &fakeRuntime{}
	workspaces := newMultiProjectWorkspace()
	service := mustNew(t, Dependencies{
		Engine: engine,
		Runtime: runtimeWithContextLimits{
			fakeRuntime: runtime, window: 200_000, output: 8_192,
		},
		Plugins: &fakePlugins{current: PluginInfo{Name: "default"}}, Skills: fakeSkills{},
		Sessions: &scopedSessions{}, Workspace: workspaces,
	})
	t.Cleanup(service.Shutdown)
	if _, err := workspaces.Create("A", t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := workspaces.Create("B", t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	return &projectSwitchHarness{service: service, runtime: runtime, engine: engine, workspaces: workspaces}
}

// enterProjectA 在项目 A 里跑一轮（会话 A 成为已加载会话，引擎带历史），并把一条
// 活动任务放进实时注册表——它就是"不该被带到项目 B"的那一行。
func (harness *projectSwitchHarness) enterProjectA(t *testing.T) {
	t.Helper()
	if err := harness.service.BeginNewSession(); err != nil {
		t.Fatal(err)
	}
	if err := harness.service.BindWorkspace("project-a"); err != nil {
		t.Fatal(err)
	}
	if err := harness.service.Submit(context.Background(), "A 的第一轮"); err != nil {
		t.Fatal(err)
	}
	waitSessionIdle(t, harness.service)
	harness.sessionA = harness.service.Snapshot().Session.ID
	if harness.sessionA == "" {
		t.Fatal("会话 A 未物化")
	}
	if _, _, err := harness.runtime.TaskAdd(dto.TaskSpec{
		Phase: dto.TaskPhaseTask, Task: foreignTaskLabel, Kind: "task",
	}); err != nil {
		t.Fatal(err)
	}
}

// switchToProjectB 触发项目切换（startFreshSession：新建独立会话）。
func (harness *projectSwitchHarness) switchToProjectB(t *testing.T) {
	t.Helper()
	if err := harness.service.BindWorkspace("project-b"); err != nil {
		t.Fatal(err)
	}
	waitSessionIdle(t, harness.service)
	harness.sessionB = harness.service.Snapshot().Session.ID
	if harness.sessionB == "" || harness.sessionB == harness.sessionA {
		t.Fatalf("项目切换应新建独立会话（否则这条用例没有判别力）：A=%q B=%q",
			harness.sessionA, harness.sessionB)
	}
}

// appendProjectRoundsFor 给**指定会话**追加 rounds 个已定稿轮次（每轮约
// chars/4 个 token）。用 For 变体显式落会话：靶场里"材料落在哪个会话的
// transcript"不是自变量（会话指针没换是另一条用例盯的现场）。
func appendProjectRoundsFor(t *testing.T, service *Service, sessionID string, rounds, chars int) {
	t.Helper()
	service.ViewMu.Lock()
	defer service.ViewMu.Unlock()
	for index := 0; index < rounds; index++ {
		mark := fmt.Sprintf("round-%02d", index)
		service.components.tasks.AppendTranscriptEventForLocked(sessionID, TranscriptEvent{
			TaskID: "task-b-fold", Role: "user", Content: "q-" + mark,
		})
		service.components.tasks.AppendTranscriptEventForLocked(sessionID, TranscriptEvent{
			TaskID: "task-b-fold", Role: "assistant", Content: mark + ":" + strings.Repeat("A", chars),
		})
	}
}

// TestProjectSwitchDoesNotInjectForeignWorkTableRows —— 时机①：切换项目后
// **未压缩**的首轮装配。
//
// 红灯断言两处：数据面（新会话的打点块必须为空）与实发面（装配返回的请求输入
// 里不得出现旧项目的活动任务行）。
func TestProjectSwitchDoesNotInjectForeignWorkTableRows(t *testing.T) {
	harness := newProjectSwitchHarness(t)
	harness.enterProjectA(t)

	// 前置：项目 A 自己的会话看得到自己的行（合法），否则"污染"无从谈起。
	if block := harness.service.workTableTraceBlockFor(harness.sessionA); !strings.Contains(block, foreignTaskLabel) {
		t.Fatalf("前置不成立：项目 A 自己那一轮的打点块应含自建任务：%q", block)
	}

	harness.switchToProjectB(t)

	// ①a 数据面：新会话（没有任何自己的条目）的打点块必须为空。
	if block := harness.service.workTableTraceBlockFor(harness.sessionB); block != "" {
		t.Errorf("跨项目工作表格污染：项目 A 的活动任务进了项目 B 会话 %q 的打点块：\n%s",
			harness.sessionB, block)
	}
	// ①b 实发面：装配出的请求输入里不得含旧项目的行。
	out, err := harness.service.components.context.PrepareExecutionContextFor(
		harness.sessionB, "task-b-first", "B 的第一问")
	if err != nil {
		t.Fatalf("装配项目 B 首轮上下文: %v", err)
	}
	if strings.Contains(out, foreignTaskLabel) {
		t.Errorf("跨项目工作表格污染：项目 A 的活动任务被前置进项目 B 的请求输入：\n%s", out)
	}
	if !strings.Contains(out, "B 的第一问") {
		t.Errorf("装配丢掉了本会话的当前输入：%q", out)
	}
}

// TestProjectSwitchWorkTableBlockStaysCleanAcrossCompaction —— 时机②：切换项目
// 后的**压缩轮**与压缩之后的下一轮。
//
// 判别力在两处：这一轮必须真的压缩（否则它与时机①同一条路径），以及压缩前后
// 的请求输入都不含旧项目的行——污染在压缩判据之前就进了 currentInput，压缩不是
// 一次清洗，切项目后它按轮重拼。
func TestProjectSwitchWorkTableBlockStaysCleanAcrossCompaction(t *testing.T) {
	pinMechanismCompactionRatios(t)
	harness := newProjectSwitchHarness(t)
	harness.enterProjectA(t)
	harness.switchToProjectB(t)

	// 压缩轮：给新会话攒够越线材料（20 轮 × 约 8k token），并建立执行纪元。
	// 材料与纪元都按**显式的会话**落（For 变体）：这条用例问的是"切换项目后新
	// 会话的上下文干净不干净"，不拿"材料/回合登记落在哪个会话"当自变量——后者
	// 由 TestProjectSwitchRebindsTaskRegistryToNewSession 单独盯。
	appendProjectRoundsFor(t, harness.service, harness.sessionB, 20, 32_000)
	harness.service.ViewMu.Lock()
	harness.service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-b-fold"}
	harness.service.components.tasks.BeginTaskFor(harness.sessionB, "task-b-fold", "切换项目后的第一轮", "high", nil, TaskCheckpoint{})
	harness.service.ViewMu.Unlock()

	out, err := harness.service.components.context.PrepareExecutionContextFor(
		harness.sessionB, "task-b-fold", "B 的压缩轮")
	if err != nil {
		t.Fatalf("装配项目 B 压缩轮上下文: %v", err)
	}
	records := compactionRecords(harness.service, harness.sessionB)
	if len(records) == 0 {
		t.Fatal("夹具应触发自动压缩（否则两条时机其实是同一条路径，用例没有判别力）")
	}
	if strings.Contains(out, foreignTaskLabel) {
		t.Errorf("跨项目工作表格污染（压缩轮）：项目 A 的活动任务进了项目 B 压缩后的请求输入：\n%s", out)
	}

	// 压缩之后的下一轮：块按轮重拼，污染不会随压缩消失。
	harness.service.ViewMu.Lock()
	previous := harness.service.components.tasks.CurrentTaskExecutionFor(harness.sessionB)
	harness.service.components.tasks.BeginTaskFor(harness.sessionB, "task-b-next", "B 的下一轮", "high", previous, TaskCheckpoint{})
	harness.service.ViewMu.Unlock()
	next, err := harness.service.components.context.PrepareExecutionContextFor(
		harness.sessionB, "task-b-next", "B 的下一轮")
	if err != nil {
		t.Fatalf("装配项目 B 压缩后下一轮上下文: %v", err)
	}
	if strings.Contains(next, foreignTaskLabel) {
		t.Errorf("跨项目工作表格污染（压缩后）：压缩之后仍把项目 A 的行拼进项目 B 的请求输入：\n%s", next)
	}
	if block := harness.service.workTableTraceBlockFor(harness.sessionB); block != "" {
		t.Errorf("跨项目工作表格污染（压缩后数据面）：新会话打点块非空：\n%s", block)
	}
}

// TestProjectSwitchRebindsTaskRegistryToNewSession —— 同一根因的数据面：项目切换
// 新建独立会话后，**任务注册表指针与会话域活跃指针**都必须换到新会话。
//
// 两个现场事实（修复前）：
//   - runtime 的 currentTaskSession 仍停在项目 A 的会话上——「视图会话 = 实时注册表」
//     那条读面于是把 A 的行当成新会话自己的行（上游那条无会话维度的老缺陷）；
//   - 会话域活跃指针也仍停在 A：RequestID → 会话的反查把新项目的回合登记到**旧
//     项目的会话**上（CurrentTaskExecutionFor(B) 为空、A 却拿到了这个 RequestID），
//     于是任务纪元/plan/transcript 这一整套会话级状态都落在旧会话里。
func TestProjectSwitchRebindsTaskRegistryToNewSession(t *testing.T) {
	harness := newProjectSwitchHarness(t)
	harness.enterProjectA(t)
	harness.switchToProjectB(t)

	// 注册表指针：实时注册表属于新会话（A 的行应已搬进 A 自己的 scope 分区）。
	if got := harness.runtime.currentTaskSession; got != harness.sessionB {
		t.Errorf("实时注册表指针 = %q, want %q（切项目后 A 的行仍被当成新会话自己的行）", got, harness.sessionB)
	}
	if records := harness.runtime.TaskSnapshotFor(""); len(records) != 0 {
		t.Errorf("新会话的实时注册表应为空（自己的条目一条都还没有）：%+v", records)
	}

	// 会话域活跃指针：回合登记必须落在新会话。
	harness.service.ViewMu.Lock()
	harness.service.components.tasks.BeginTask("task-routing", "切换项目后的一轮", "high", nil, TaskCheckpoint{})
	harness.service.ViewMu.Unlock()
	if got := harness.service.components.tasks.SessionIDForRequest("task-routing"); got != harness.sessionB {
		t.Errorf("回合 task-routing 被登记到 %q，want %q（新项目的回合落在旧项目的会话上）", got, harness.sessionB)
	}
	harness.service.ViewMu.RLock()
	newSessionState := harness.service.components.tasks.CurrentTaskExecutionFor(harness.sessionB)
	oldSessionState := harness.service.components.tasks.CurrentTaskExecutionFor(harness.sessionA)
	harness.service.ViewMu.RUnlock()
	if newSessionState == nil || newSessionState.RequestID != "task-routing" {
		t.Errorf("新会话的执行纪元 = %+v, want RequestID=task-routing", newSessionState)
	}
	if oldSessionState != nil && oldSessionState.RequestID == "task-routing" {
		t.Errorf("旧会话拿到了新项目的 RequestID（跨项目串台）：%+v", oldSessionState)
	}
}

// TestProjectSwitchKeepsForeignRowsInLedger —— 边界：工作表格是**项目/全局台账**，
// 切换项目只换"谁在实发"，旧项目的行必须留在台账里（连归属会话一起），否则修复
// 就把"切会话丢行"的老缺陷又买回来了（见 docs/devlog/2026-09-19-worktable-global-scope.md）。
func TestProjectSwitchKeepsForeignRowsInLedger(t *testing.T) {
	harness := newProjectSwitchHarness(t)
	harness.enterProjectA(t)
	harness.switchToProjectB(t)

	ledger := harness.runtime.TaskSnapshot()
	found := false
	for _, record := range ledger {
		if !strings.Contains(record.Task, foreignTaskLabel) {
			continue
		}
		found = true
		if record.SessionID != harness.sessionA {
			t.Errorf("台账行的归属会话 = %q, want %q（切项目不得改归属）", record.SessionID, harness.sessionA)
		}
	}
	if !found {
		t.Fatalf("切换项目把旧项目的行从全局台账里弄丢了：%+v", ledger)
	}

	// 旧项目会话的会话级读面同样保留（落盘/恢复读的就是它）。
	if records := harness.runtime.TaskSnapshotFor(harness.sessionA); len(records) == 0 {
		t.Fatalf("项目 A 会话自己的 scope 读面为空（行被丢弃而不是搬进分区）：%+v", records)
	}
}

// TestProjectSwitchWithoutFreshSessionKeepsOwnRows —— 对照：**没有**新建独立会话
// 的项目切换（重复绑定同一个项目：currentWorkspaceID == workspace.ID）不换会话
// 指针，本会话自己的行照旧留在打点块里。修复只对"新建会话"那一条分支生效，
// 不得顺手把同会话的行也清掉。
func TestProjectSwitchWithoutFreshSessionKeepsOwnRows(t *testing.T) {
	harness := newProjectSwitchHarness(t)
	harness.enterProjectA(t)

	if err := harness.service.BindWorkspace("project-a"); err != nil {
		t.Fatal(err)
	}
	if got := harness.service.Snapshot().Session.ID; got != harness.sessionA {
		t.Fatalf("重复绑定同一项目不应新建会话：会话 = %q, want %q", got, harness.sessionA)
	}
	block := harness.service.workTableTraceBlockFor(harness.sessionA)
	if !strings.Contains(block, foreignTaskLabel) {
		t.Fatalf("同会话自己的活动任务应留在打点块里：%q", block)
	}
}
