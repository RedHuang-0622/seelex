package core

import (
	"context"
	assemble "github.com/RedHuang-0622/seelex/backend/engine/assemble"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	applicationadmin "github.com/RedHuang-0622/seelex/application/admin"
)

func TestSchedulerApplicationIsAssembled(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("schedulerApplication")
	if !ok {
		t.Fatal("serviceComponents 未装配 schedulerApplication")
	}
	if field.Type != reflect.TypeOf((*schedulerApplication)(nil)) {
		t.Fatalf("schedulerApplication 字段类型 = %s", field.Type)
	}
}

func TestRuntimeApplicationIsAssembled(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("runtimeApplication")
	if !ok {
		t.Fatal("serviceComponents 未装配 runtimeApplication")
	}
	if field.Type != reflect.TypeOf((*applicationadmin.RuntimeApplication)(nil)) {
		t.Fatalf("runtimeApplication 字段类型 = %s", field.Type)
	}
}

func TestScheduledRunApplicationIsAssembled(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("scheduledRunApplication")
	if !ok {
		t.Fatal("serviceComponents 未装配 scheduledRunApplication")
	}
	if field.Type != reflect.TypeOf((*scheduledRunApplication)(nil)) {
		t.Fatalf("scheduledRunApplication 字段类型 = %s", field.Type)
	}
}

func TestScheduledSessionApplicationIsAssembled(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("scheduledSessionApplication")
	if !ok {
		t.Fatal("serviceComponents 未装配 scheduledSessionApplication")
	}
	if field.Type != reflect.TypeOf((*scheduledSessionApplication)(nil)) {
		t.Fatalf("scheduledSessionApplication 字段类型 = %s", field.Type)
	}
}

func TestSchedulerApplicationCreateStopsOnValidationError(t *testing.T) {
	application := newSchedulerApplication(schedulerApplicationDeps{
		ValidateWorkspace: func(string) error {
			t.Fatal("参数校验失败后不得校验工作区")
			return nil
		},
		ScheduleTask: func(context.Context, assemble.ScheduledTaskSpec) (*assemble.ScheduledTaskStatus, error) {
			t.Fatal("参数校验失败后不得调用调度器")
			return nil, nil
		},
	})

	_, err := application.ScheduleTask(context.Background(), assemble.ScheduledTaskSpec{LoopLimit: -1})
	if err == nil {
		t.Fatal("ScheduleTask accepted invalid loop limit")
	}
}

func TestSchedulerFacadeOnlyDelegatesToApplication(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "..", "compat", "frontend.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"ScheduleTask":        false,
		"UpdateScheduledTask": false,
		"CancelScheduledTask": false,
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name == nil || !functionHasReceiver(function, "Frontend") {
			continue
		}
		if _, tracked := want[function.Name.Name]; !tracked {
			continue
		}
		want[function.Name.Name] = true
		if len(function.Body.List) != 1 {
			t.Fatalf("%s 有 %d 条语句，want 一行 schedulerApplication 转发", function.Name.Name, len(function.Body.List))
		}
		returnStatement, ok := function.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(returnStatement.Results) != 1 {
			t.Fatalf("%s 不是单行 return 转发", function.Name.Name)
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("未找到 %s", name)
		}
	}
}

func TestRefreshRuntimeSnapshotFacadeOnlyDelegatesToRuntimeApplication(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "..", "compat", "frontend.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name == nil || function.Name.Name != "RefreshRuntimeSnapshot" ||
			!functionHasReceiver(function, "Frontend") {
			continue
		}
		if len(function.Body.List) != 1 {
			t.Fatalf("RefreshRuntimeSnapshot 有 %d 条语句，want 一行 runtimeApplication 转发", len(function.Body.List))
		}
		if _, ok := function.Body.List[0].(*ast.ExprStmt); !ok {
			t.Fatal("RefreshRuntimeSnapshot 不是单行调用转发")
		}
		return
	}
	t.Fatal("未找到 RefreshRuntimeSnapshot")
}

func TestAssembleScheduledRunFacadeOnlyDelegatesToApplication(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "..", "compat", "frontend.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name == nil || function.Name.Name != "AssembleScheduledRun" ||
			!functionHasReceiver(function, "Frontend") {
			continue
		}
		if len(function.Body.List) != 1 {
			t.Fatalf("AssembleScheduledRun 有 %d 条语句，want 一行 scheduledRunApplication 转发", len(function.Body.List))
		}
		returnStatement, ok := function.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(returnStatement.Results) != 1 {
			t.Fatal("AssembleScheduledRun 不是单行 return 转发")
		}
		return
	}
	t.Fatal("未找到 AssembleScheduledRun")
}

func TestStartScheduledSessionFacadeOnlyDelegatesToApplication(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "..", "compat", "frontend.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name == nil || function.Name.Name != "StartScheduledSession" ||
			!functionHasReceiver(function, "Frontend") {
			continue
		}
		if len(function.Body.List) != 1 {
			t.Fatalf("StartScheduledSession 有 %d 条语句，want 一行 scheduledSessionApplication 转发", len(function.Body.List))
		}
		returnStatement, ok := function.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(returnStatement.Results) != 1 {
			t.Fatal("StartScheduledSession 不是单行 return 转发")
		}
		return
	}
	t.Fatal("未找到 StartScheduledSession")
}

// TestStartScheduledSessionCreatesNewSessionInWorkspace 钉住定时触发的会话落点：
//
//  1. **默认新建会话**（不是投给当前会话、也不切用户的视图指针）；
//  2. 指定工作区时，新会话真的装配到那个项目——会话绑定、framework 显式键、
//     工具面按会话解析的项目根三处都要对上（少一处就是"选了工作区但任务
//     跑在别的项目里"，即工作区污染）；
//  3. 提示词落在**新会话**的可见投影里，当前视图会话一条都不多。
//
// 会话记录本身仍走会话自己的存储/读写纪律（这里只验"落在哪个会话、绑哪个
// 项目"这一层，落盘口径由 store 的用例钉住）。
func TestStartScheduledSessionCreatesNewSessionInWorkspace(t *testing.T) {
	root := t.TempDir()
	engine := &fakeEngine{chunks: []string{"收到"}}
	runtime := &fakeRuntime{}
	workspaces := newFakeWorkspace()
	workspaces.items["project-1"] = model.WorkspaceInfo{ID: "project-1", Name: "巡检项目", RootPath: root}
	sessions := &scopedSessions{}
	service := mustNew(t, contract.Dependencies{
		Engine: engine, Runtime: runtime, Plugins: &fakePlugins{current: model.PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer frontendOfTestService(service).Shutdown()

	before := service.Snapshot().Session.ID
	sessionID, err := frontendOfTestService(service).StartScheduledSession(context.Background(), assemble.ScheduledTaskSpec{
		Prompt: "每天巡检一次", WorkspaceID: "project-1",
	})
	if err != nil {
		t.Fatalf("StartScheduledSession: %v", err)
	}
	if sessionID == "" || sessionID == before {
		t.Fatalf("定时触发必须落到新会话：new=%q current=%q", sessionID, before)
	}
	if !strings.HasPrefix(sessionID, "sched") {
		t.Fatalf("新建会话 ID 前缀 = %q，want sched 前缀（一眼能看出是定时任务开的会话）", sessionID)
	}
	if workspaces.bindings[sessionID] != "project-1" {
		t.Fatalf("新会话没有绑定工作区：bindings=%v", workspaces.bindings)
	}
	// 装配：权限档位落到**这个会话**（默认 full access），插件/权限不改进程默认。
	if got := service.permissionTierForSession(sessionID); got != dto.PermissionTierFull {
		t.Fatalf("定时会话的权限档位 = %q，want full（任务默认全权）", got)
	}
	if err := frontendOfTestService(service).WaitForIdle(context.Background()); err != nil {
		t.Fatalf("定时会话没有回到 idle：%v", err)
	}
	// 工具根按会话解析：回合起点（runChat）会把新会话自己的项目根绑上，
	// 因此这里读到的必须是工作区根，而不是进程默认根。
	if got := runtime.toolRootForSession(sessionID); got != root {
		t.Fatalf("新会话的工具根 = %q，want 工作区根 %q", got, root)
	}
	if got := service.Snapshot().Session.ID; got != before {
		t.Fatalf("定时任务把用户的视图会话切走了：before=%q after=%q", before, got)
	}
	if view := conversationContents(service.Snapshot().Conversation); strings.Contains(view, "每天巡检一次") {
		t.Fatalf("提示词落到了当前视图会话，而不是新会话：%q", view)
	}
	snapshot, err := frontendOfTestService(service).SnapshotOf(sessionID)
	if err != nil {
		t.Fatalf("SnapshotOf(%q): %v", sessionID, err)
	}
	if got := conversationContents(snapshot.Conversation); !strings.Contains(got, "每天巡检一次") {
		t.Fatalf("新会话里看不到提示词：%q", got)
	}
}

// TestStartScheduledSessionWithoutWorkspaceKeepsSessionUnbound 钉住"工作区是
// 可选"这一半：不给工作区就是**不绑项目**的新会话——不能继承视图会话的项目
// 绑定（否则在项目 A 里建的定时任务会偷偷跑在项目 A 上）。
func TestStartScheduledSessionWithoutWorkspaceKeepsSessionUnbound(t *testing.T) {
	root := t.TempDir()
	engine := &fakeEngine{chunks: []string{"收到"}}
	runtime := &fakeRuntime{}
	workspaces := newFakeWorkspace()
	workspaces.items["project-1"] = model.WorkspaceInfo{ID: "project-1", Name: "巡检项目", RootPath: root}
	service := mustNew(t, contract.Dependencies{
		Engine: engine, Runtime: runtime, Plugins: &fakePlugins{current: model.PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: &scopedSessions{}, Workspace: workspaces,
	})
	defer frontendOfTestService(service).Shutdown()

	// 当前会话先绑到项目（视图会话的项目绑定不该传染给定时会话）。
	workspaces.BindSession(service.Snapshot().Session.ID, "project-1")

	sessionID, err := frontendOfTestService(service).StartScheduledSession(context.Background(), assemble.ScheduledTaskSpec{
		Prompt: "无工作区巡检",
	})
	if err != nil {
		t.Fatalf("StartScheduledSession: %v", err)
	}
	if _, bound := workspaces.bindings[sessionID]; bound {
		t.Fatalf("未指定工作区的新会话不该绑项目：%v", workspaces.bindings)
	}
	if err := frontendOfTestService(service).WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 没有按会话绑根 = 这个会话的工具面走进程默认根（会话切换的正常语义），
	// 关键是它**没有**自己的项目根——没人把别的项目塞给它。
	if got := runtime.sessionProjectRoot(sessionID); got != "" {
		t.Fatalf("无工作区的新会话被绑上了项目根 %q", got)
	}
}

// sessionProjectRoot 读 fakeRuntime 的按会话工具根（同包测试直读，加锁）。
func (runtime *fakeRuntime) sessionProjectRoot(sessionID string) string {
	runtime.sessionProjectRootsMu.Lock()
	defer runtime.sessionProjectRootsMu.Unlock()
	return runtime.sessionProjectRoots[sessionID]
}

// conversationContents 把可见会话拼成一段文本（判据只看"出现过没有"）。
func conversationContents(messages []model.Message) string {
	parts := make([]string, 0, len(messages))
	for _, message := range messages {
		parts = append(parts, message.Content)
	}
	return strings.Join(parts, "\n")
}

// TestUpdateScheduledTaskForwardsAndValidatesWorkspace 钉住编辑这条链的应用层口径：
// 定义合法性交给调度器（与创建同一份判据），**工作区存在性**在应用层判（与创建
// 同一份判据）——改到不存在的工作区要当场拒绝，而不是等到触发那天才失败。
func TestUpdateScheduledTaskForwardsAndValidatesWorkspace(t *testing.T) {
	runtime := &fakeRuntime{}
	workspaces := newFakeWorkspace()
	workspaces.items["project-1"] = model.WorkspaceInfo{ID: "project-1", Name: "巡检项目", RootPath: t.TempDir()}
	service := mustNew(t, contract.Dependencies{
		Engine: &fakeEngine{}, Runtime: runtime, Plugins: &fakePlugins{current: model.PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: &fakeSessions{}, Workspace: workspaces,
	})
	defer frontendOfTestService(service).Shutdown()

	updated, err := frontendOfTestService(service).UpdateScheduledTask(context.Background(), "sched_test", assemble.ScheduledTaskSpec{
		Name: "改后的名字", Kind: assemble.ScheduledTaskPrompt, Prompt: "改后的提示词",
		WorkspaceID: "project-1", Interval: time.Hour, Enabled: true,
	})
	if err != nil {
		t.Fatalf("UpdateScheduledTask: %v", err)
	}
	if updated == nil || updated.ID != "sched_test" || updated.Name != "改后的名字" {
		t.Fatalf("编辑结果 = %+v", updated)
	}
	if len(runtime.updatedTasks) != 1 || runtime.updatedTasks[0] != "sched_test" {
		t.Fatalf("编辑没有按 ID 转发：%v", runtime.updatedTasks)
	}

	// 不存在的工作区：当场拒绝，且不惊动调度器。
	if _, err := frontendOfTestService(service).UpdateScheduledTask(context.Background(), "sched_test", assemble.ScheduledTaskSpec{
		Name: "n", Kind: assemble.ScheduledTaskPrompt, Prompt: "P", WorkspaceID: "ws_missing", Interval: time.Hour,
	}); err == nil {
		t.Fatal("编辑到不存在的工作区必须报错")
	}
	if len(runtime.updatedTasks) != 1 {
		t.Fatalf("被拒的编辑不该转发到调度器：%v", runtime.updatedTasks)
	}

	// 空 ID：编辑没有目标，直接拒绝。
	if _, err := frontendOfTestService(service).UpdateScheduledTask(context.Background(), "   ", assemble.ScheduledTaskSpec{
		Name: "n", Kind: assemble.ScheduledTaskPrompt, Prompt: "P", Interval: time.Hour,
	}); err == nil {
		t.Fatal("空任务 ID 必须报错")
	}
}

// TestAssembleScheduledRunAppliesTierAndPlugins 钉住任务的**装配**落到哪儿：
//
//   - 权限档位进**这个会话**（档位槽 + 执行门；空档位按默认 full access 兜住），
//     不改进程默认、不影响其它会话；
//   - 插件装配带进**这一轮的执行 ctx**（fake 记下最近一次装配），且只在声明了
//     插件时才装配（空 = 继承宿主，不往 ctx 里塞空集合）。
func TestAssembleScheduledRunAppliesTierAndPlugins(t *testing.T) {
	runtime := &fakeRuntime{}
	engine := &fakeEngine{}
	service := mustNew(t, contract.Dependencies{
		Engine: engine, Runtime: runtime, Plugins: &fakePlugins{current: model.PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: &fakeSessions{},
	})
	defer frontendOfTestService(service).Shutdown()

	sessionID := "sched_assemble"
	service.ViewMu.Lock()
	service.sessionUnitLocked(sessionID)
	service.ViewMu.Unlock()

	// 空档位（未归一的 spec 直接进来）：按默认 full access 兜住。
	ctx, err := frontendOfTestService(service).AssembleScheduledRun(context.Background(), sessionID, assemble.ScheduledTaskSpec{Plugins: []string{"code"}})
	if err != nil {
		t.Fatalf("AssembleScheduledRun: %v", err)
	}
	if ctx == nil {
		t.Fatal("装配后必须返回可用的 ctx")
	}
	if got := service.permissionTierForSession(sessionID); got != dto.PermissionTierFull {
		t.Fatalf("空档位没有兜成 full：%q", got)
	}
	if got := runtime.PluginAssembly(); len(got) != 1 || got[0] != "code" {
		t.Fatalf("插件装配没有带进 ctx：%v", got)
	}
	// 执行参数（未声明 = 老定义/裸 spec）：循环次数按会话钉在引擎那一格，思考强度记
	// 一笔租约等该会话的回合起点取走（账号池是进程级共享的，只能按会话记一笔）。
	if got := engine.sessionLoopsFor(sessionID); got != 384 {
		t.Fatalf("未声明循环次数没有落成默认档位读数：%d, want 384", got)
	}
	if wire, ok := service.knobs.TakeSessionReasoningEffort(sessionID); !ok || wire != dto.ReasoningEffortHigh {
		t.Fatalf("未声明思考强度没有落成默认档位读数：%q（ok=%v）, want high", wire, ok)
	}
	if _, again := service.knobs.TakeSessionReasoningEffort(sessionID); again {
		t.Fatal("强度租约必须只被取走一次（它属于那一次运行）")
	}

	// 声明了两条参数：按声明的落，不被默认值盖掉。
	if _, err := frontendOfTestService(service).AssembleScheduledRun(context.Background(), sessionID, assemble.ScheduledTaskSpec{
		ReasoningEffort: dto.ReasoningEffortLow, LoopLimit: 48,
	}); err != nil {
		t.Fatalf("AssembleScheduledRun: %v", err)
	}
	if got := engine.sessionLoopsFor(sessionID); got != 48 {
		t.Fatalf("声明的循环次数没有落到那次会话：%d, want 48", got)
	}
	if wire, ok := service.knobs.TakeSessionReasoningEffort(sessionID); !ok || wire != dto.ReasoningEffortLow {
		t.Fatalf("声明的思考强度没有记上：%q（ok=%v）, want low", wire, ok)
	}
	// 别的会话一个字都不受影响（装配是会话级决定，不进进程默认）。
	if got := engine.sessionLoopsFor("another_session"); got != 0 {
		t.Fatalf("循环次数钉值泄漏到了别的会话：%d", got)
	}

	// 显式档位 + 空插件：档位照写，插件面不再装配（上一次的读数保持不变）。
	if _, err := frontendOfTestService(service).AssembleScheduledRun(context.Background(), sessionID, assemble.ScheduledTaskSpec{
		PermissionTier: dto.PermissionTierEdit,
	}); err != nil {
		t.Fatalf("AssembleScheduledRun: %v", err)
	}
	if got := service.permissionTierForSession(sessionID); got != dto.PermissionTierEdit {
		t.Fatalf("显式档位没有落到会话：%q", got)
	}
	if got := runtime.PluginAssembly(); len(got) != 1 || got[0] != "code" {
		t.Fatalf("空插件声明不该触发新的装配：%v", got)
	}

	// 其它会话不受影响（装配是会话级决定，不进进程默认）。
	if got := service.permissionTierForSession("another_session"); got != dto.PermissionTierManual {
		t.Fatalf("装配泄漏到了别的会话：%q", got)
	}
	if _, err := frontendOfTestService(service).AssembleScheduledRun(context.Background(), "  ", assemble.ScheduledTaskSpec{}); err == nil {
		t.Fatal("空会话 ID 必须报错")
	}
	if _, err := frontendOfTestService(service).AssembleScheduledRun(context.Background(), sessionID, assemble.ScheduledTaskSpec{PermissionTier: "root"}); err == nil {
		t.Fatal("未知档位必须报错（不静默降级）")
	}
}

// TestScheduledRunParamsDefaultsAndValidation 钉住定时任务那两条执行参数（思考强度 /
// 循环次数）的三件事：
//
//  1. 创建时**不声明** → 定义里落成**默认参数**（默认档位 high 的两条读数：high/384）
//     ——面板上因此看得到触发时真正要用的值；
//  2. 声明了就按声明的转发（应用层不拿默认值去盖用户的选择）；
//  3. 非法取值在创建那一刻就拒（不是等到触发那天变成一次失败的运行）。
func TestScheduledRunParamsDefaultsAndValidation(t *testing.T) {
	runtime := &fakeRuntime{}
	service := mustNew(t, contract.Dependencies{
		Engine: &fakeEngine{}, Runtime: runtime, Plugins: &fakePlugins{current: model.PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: &fakeSessions{},
	})
	defer frontendOfTestService(service).Shutdown()

	if _, err := frontendOfTestService(service).ScheduleTask(context.Background(), assemble.ScheduledTaskSpec{
		Name: "巡检", Kind: assemble.ScheduledTaskPrompt, Prompt: "P", Interval: time.Hour,
	}); err != nil {
		t.Fatalf("ScheduleTask: %v", err)
	}
	if len(runtime.scheduledSpecs) != 1 {
		t.Fatalf("创建没有转发到调度器：%v", runtime.scheduledSpecs)
	}
	if got := runtime.scheduledSpecs[0]; got.ReasoningEffort != dto.ReasoningEffortHigh || got.LoopLimit != 384 {
		t.Fatalf("未声明的执行参数没有落成默认参数：(%q, %d)，want (high, 384)", got.ReasoningEffort, got.LoopLimit)
	}

	if _, err := frontendOfTestService(service).ScheduleTask(context.Background(), assemble.ScheduledTaskSpec{
		Name: "带参数", Kind: assemble.ScheduledTaskPrompt, Prompt: "P", Interval: time.Hour,
		ReasoningEffort: dto.ReasoningEffortLow, LoopLimit: 48,
	}); err != nil {
		t.Fatalf("ScheduleTask: %v", err)
	}
	if got := runtime.scheduledSpecs[1]; got.ReasoningEffort != dto.ReasoningEffortLow || got.LoopLimit != 48 {
		t.Fatalf("声明的执行参数被默认值盖掉了：(%q, %d)，want (low, 48)", got.ReasoningEffort, got.LoopLimit)
	}

	for _, testCase := range []struct {
		name    string
		spec    assemble.ScheduledTaskSpec
		wantErr string
	}{
		{"未识别的思考强度", assemble.ScheduledTaskSpec{ReasoningEffort: "ludicrous"}, "无效的思考强度"},
		{"循环次数越上限", assemble.ScheduledTaskSpec{LoopLimit: Limits().InputLoopLimit + 1}, "循环次数"},
		{"循环次数为负", assemble.ScheduledTaskSpec{LoopLimit: -1}, "循环次数"},
	} {
		spec := testCase.spec
		spec.Name = testCase.name
		spec.Kind = assemble.ScheduledTaskPrompt
		spec.Prompt = "P"
		spec.Interval = time.Hour
		_, err := frontendOfTestService(service).ScheduleTask(context.Background(), spec)
		if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
			t.Fatalf("%s：err = %v，want 含 %q", testCase.name, err, testCase.wantErr)
		}
	}
	if len(runtime.scheduledSpecs) != 2 {
		t.Fatalf("被拒的任务不该转发到调度器：%v", runtime.scheduledSpecs)
	}
}

// TestScheduledPluginsValidatedAgainstCatalog 钉住应用层的插件装配校验：未知插件名
// 在**创建/编辑**这一刻就拒绝（而不是等到触发那天变成"这一轮什么工具都没有"）。
func TestScheduledPluginsValidatedAgainstCatalog(t *testing.T) {
	runtime := &fakeRuntime{}
	service := mustNew(t, contract.Dependencies{
		Engine: &fakeEngine{}, Runtime: runtime, Plugins: &fakePlugins{current: model.PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: &fakeSessions{},
	})
	defer frontendOfTestService(service).Shutdown()

	// 目录里有 code：创建放行，且装配随 spec 交给调度器。
	if _, err := frontendOfTestService(service).ScheduleTask(context.Background(), assemble.ScheduledTaskSpec{
		Name: "带装配", Kind: assemble.ScheduledTaskPrompt, Prompt: "P", Interval: time.Hour,
		PermissionTier: dto.PermissionTierAuto, Plugins: []string{"code"},
	}); err != nil {
		t.Fatalf("已知插件的任务被拒：%v", err)
	}
	if len(runtime.scheduledSpecs) != 1 || runtime.scheduledSpecs[0].PermissionTier != dto.PermissionTierAuto {
		t.Fatalf("装配没有随创建一起转发：%+v", runtime.scheduledSpecs)
	}

	// 目录里没有 ghost：创建与编辑都当场拒绝，且不惊动调度器。
	if _, err := frontendOfTestService(service).ScheduleTask(context.Background(), assemble.ScheduledTaskSpec{
		Name: "未知插件", Kind: assemble.ScheduledTaskPrompt, Prompt: "P", Interval: time.Hour,
		Plugins: []string{"ghost"},
	}); err == nil {
		t.Fatal("未知插件必须被拒")
	}
	if _, err := frontendOfTestService(service).UpdateScheduledTask(context.Background(), "sched_test", assemble.ScheduledTaskSpec{
		Name: "n", Kind: assemble.ScheduledTaskPrompt, Prompt: "P", Interval: time.Hour,
		Plugins: []string{"code", "ghost"},
	}); err == nil {
		t.Fatal("编辑到未知插件必须被拒")
	}
	if len(runtime.scheduledSpecs) != 1 || len(runtime.updatedTasks) != 0 {
		t.Fatalf("被拒的装配不该转发：specs=%d updated=%v", len(runtime.scheduledSpecs), runtime.updatedTasks)
	}
	// 重复声明与超上限交给同一份 dto.NormalizePlugins 判据，这里只确认它会冒出来。
	if _, err := frontendOfTestService(service).ScheduleTask(context.Background(), assemble.ScheduledTaskSpec{
		Name: "重复声明", Kind: assemble.ScheduledTaskPrompt, Prompt: "P", Interval: time.Hour,
		Plugins: []string{"code", "code"},
	}); err == nil {
		t.Fatal("重复声明的插件必须被拒")
	}
}
