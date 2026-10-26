package core

// project_board_test.go — M2 交付的单测（T2.1–T2.9 + 触发表/落点/I6 三条配套）。
//
// 判据一句话：**投影读出来的每一行都能在活体投影里指到出处；活体与领域判定永远不看投影。**
// （T2.1 一致、T2.2 零判定、T2.9 反例；其余是同步器语义。）
import (
	"context"
	"encoding/json"
	"errors"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/middle/view/board"
	"github.com/RedHuang-0622/seelex/middle/view/board/boardtest"
	"github.com/RedHuang-0622/seelex/store"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	applicationexecution "github.com/RedHuang-0622/seelex/application/execution"
)

func TestProjectBoardQueryIsAssembled(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("projectBoardQuery")
	if !ok {
		t.Fatal("serviceComponents 未装配 projectBoardQuery")
	}
	if field.Type != reflect.TypeOf((*applicationexecution.ProjectBoardApplication)(nil)) {
		t.Fatalf("projectBoardQuery 字段类型 = %s", field.Type)
	}
}

func TestProjectBoardReadFacadeUsesPorts(t *testing.T) {
	serviceType := reflect.TypeOf((*Service)(nil))
	for _, name := range []string{"ProjectBoardTeamPlanFor", "ProjectBoardTeamStateFor"} {
		if _, ok := serviceType.MethodByName(name); ok {
			t.Fatalf("Service.%s must use FrontendPorts instead", name)
		}
	}
	ports := reflect.TypeOf(FrontendPorts{})
	field, ok := ports.FieldByName("ProjectBoard")
	if !ok || field.Type != reflect.TypeOf((*applicationexecution.ProjectBoardApplication)(nil)) {
		t.Fatalf("FrontendPorts.ProjectBoard = %v", field.Type)
	}
}

func newBoardTestService(t *testing.T, options ...testServiceOption) (*Service, *fakeRuntime, string) {
	t.Helper()
	service := newTestService(t, &fakeEngine{}, options...)
	runtime, ok := service.Deps.Runtime.(*fakeRuntime)
	if !ok {
		t.Fatalf("测试夹具的 Runtime 不是 *fakeRuntime")
	}
	session := service.Snapshot().Session.ID
	runtime.currentTaskSession = session
	return service, runtime, session
}

func TestBuildProjectBoardRowsMatchesLiveProjection(t *testing.T) {
	service, runtime, session := newBoardTestService(t)
	runtime.todoItems = []dto.TodoItem{{Text: "调研", Status: dto.TodoItemDoing}}
	runtime.tasks = map[string]dto.TaskRecord{
		"task:1":     {ID: "task:1", Phase: "task", Task: "写投影", Status: dto.TaskRunning, Kind: "task"},
		"plan:n1":    {ID: "plan:n1", Phase: "plan", Task: "计划节点", Status: dto.TaskPending, Kind: "plan", SourceID: "n1"},
		"subagent:1": {ID: "subagent:1", Phase: "subagent", Task: "子代理", Status: dto.TaskRunning, Kind: "subagent"},
	}
	live := frontendOfTestService(service).WorkTableRowsFor(session)
	if len(live) != 4 {
		t.Fatalf("活体工作表格行数 = %d，希望 4（todo/task/plan/subagent 各一）", len(live))
	}

	scope := store.ProjectBoardScope{Project: boardtest.Project, Session: session}
	rows := board.BuildProjectBoardRows(scope, 1, boardtest.At, live, nil, nil, 800)
	again := board.BuildProjectBoardRows(scope, 1, boardtest.At, live, nil, nil, 800)
	if boardtest.MarshalRows(t, rows) != boardtest.MarshalRows(t, again) {
		t.Fatalf("同一输入跑两遍结果不同：%s vs %s", boardtest.MarshalRows(t, rows), boardtest.MarshalRows(t, again))
	}

	// 活体侧的期望行：kind ∈ {todo, task}（测试自己列，不复用生产判据表），折叠读排序口径
	//（Kind, Key）下逐行对齐。
	expected := make([]model.WorkItem, 0, len(live))
	for _, item := range live {
		if item.Kind == "todo" || item.Kind == "task" {
			expected = append(expected, item)
		}
	}
	sort.Slice(expected, func(left, right int) bool { return expected[left].ID < expected[right].ID })

	folded := store.FoldProjectBoardRows(rows, nil)
	if len(folded) != len(expected) {
		t.Fatalf("折叠读行数 = %d，希望 %d（活体里的 todo/task 行数）", len(folded), len(expected))
	}
	for index, row := range folded {
		item := expected[index]
		if row.Kind != store.ProjectBoardKindWork {
			t.Fatalf("折叠读第 %d 行 kind = %q，希望 work", index, row.Kind)
		}
		if row.Key != "work:"+item.ID {
			t.Fatalf("折叠读第 %d 行 key = %q，希望 work:%s", index, row.Key, item.ID)
		}
		if got := boardtest.PayloadText(row, "status"); got != item.Status {
			t.Fatalf("折叠读第 %d 行 status = %q，活体 = %q", index, got, item.Status)
		}
		if got := boardtest.PayloadText(row, "task"); got != item.Task {
			t.Fatalf("折叠读第 %d 行 task = %q，活体 = %q", index, got, item.Task)
		}
		if row.Seq != 1 || row.At != boardtest.At || row.Schema != store.ProjectBoardSchema {
			t.Fatalf("折叠读第 %d 行信封 = seq:%d at:%d schema:%d", index, row.Seq, row.At, row.Schema)
		}
		if row.Scope != scope {
			t.Fatalf("折叠读第 %d 行 scope = %+v，希望 %+v", index, row.Scope, scope)
		}
	}
}

// ---------------------------------------------------------------------------
// T2.2 零判定：输入 status=x ⇒ 输出 status=x（含"空串也原样"）
// ---------------------------------------------------------------------------

func TestProjectBoardIsNotAnInput(t *testing.T) {
	service, runtime, session := newBoardTestService(t)
	runtime.todoItems = []dto.TodoItem{{Text: "调研", Status: dto.TodoItemDoing}}

	before := frontendOfTestService(service).WorkTableRowsFor(session)
	if len(before) != 1 || before[0].Status != "doing" {
		t.Fatalf("活体投影 = %+v，希望一条 doing", before)
	}

	// 篡改投影文件：把那条 todo 改成 done（外加一行 schema/kind 都认不得的垃圾行）。
	st := boardtest.NewStore()
	if _, err := st.AppendProjectBoardRows(boardtest.ProjectRoot, session, store.ProjectBoardWorkFile,
		[]store.ProjectBoardRow{
			{Schema: store.ProjectBoardSchema, Kind: store.ProjectBoardKindWork, Key: "work:todo:0", Seq: 99, At: boardtest.At,
				Scope:   store.ProjectBoardScope{Project: boardtest.Project, Session: session},
				Payload: map[string]any{"id": "todo:0", "kind": "todo", "status": "done"}},
			{Schema: 99, Kind: "not-a-kind", Key: "work:todo:1", Seq: 100, At: boardtest.At,
				Payload: map[string]any{"status": "done"}},
		}); err != nil {
		t.Fatalf("造假投影文件: %v", err)
	}

	// 文件的读**确实**被改了（否则这个反例什么都没证明）。
	folded := st.Folded(boardtest.ProjectRoot, session, store.ProjectBoardWorkFile)
	if len(folded) != 1 || boardtest.PayloadText(folded[0], "status") != "done" {
		t.Fatalf("篡改后的文件读 = %+v，希望一条 done", folded)
	}

	// 而活体投影与领域判定一字不变：投影**不是任何东西的输入**。
	after := frontendOfTestService(service).WorkTableRowsFor(session)
	if marshalWorkItems(t, after) != marshalWorkItems(t, before) {
		t.Fatalf("篡改投影文件后活体投影变了：\n before=%s\n after =%s", marshalWorkItems(t, before), marshalWorkItems(t, after))
	}
	records := runtime.TaskSnapshotFor(session)
	if len(records) != 1 || records[0].Status != dto.TaskDoing {
		t.Fatalf("篡改投影文件后 task 注册表变了：%+v", records)
	}
	// 投影输入（活体取数）也不受文件影响：再映射一次仍是 doing。
	rows := board.BuildProjectBoardRows(store.ProjectBoardScope{Project: boardtest.Project, Session: session},
		1, boardtest.At, frontendOfTestService(service).WorkTableRowsFor(session), nil, nil, 800)
	if len(rows) != 1 || boardtest.PayloadText(rows[0], "status") != "doing" {
		t.Fatalf("活体输入映射出的行 = %+v，希望 status=doing", rows)
	}
}

func marshalWorkItems(t *testing.T, items []model.WorkItem) string {
	t.Helper()
	encoded, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("编码活体行: %v", err)
	}
	return string(encoded)
}

// ---------------------------------------------------------------------------
// 配套 1：触发装配表（off / todo_list / task_complete / tool_complete / all）
// ---------------------------------------------------------------------------

func TestProjectBoardReadersForAssembly(t *testing.T) {
	// 已装配 teamwork 计划端口（Runtime 额外实现了 board.TeamworkPlanSource）：装配期注入，不做事后改 Deps。
	planSource := &boardPlanRuntime{
		fakeRuntime: &fakeRuntime{},
		plan: store.TeamworkPlan{
			TeamID: "team-1",
			State:  store.TeamworkState{State: store.TeamworkStateClosed},
		},
	}
	service := newTestService(t, &fakeEngine{},
		withTestRuntime(planSource),
		func(deps *contract.Dependencies) { deps.Workspace = newFakeWorkspace() })
	session := service.Snapshot().Session.ID
	planSource.fakeRuntime.currentTaskSession = session

	if _, err := service.Deps.Workspace.Create("proj", `C:\proj`, ""); err != nil {
		t.Fatalf("建工作区: %v", err)
	}
	// 落点是**会话所属工作区的项目根**（`<项目根>/.seelex/board/`）：未绑定 ⇒ 空串 ⇒
	// 不落盘（契约 §3 未关联会话不落盘）；绑定后取的是工作区的 RootPath，**不是**工作区 ID。
	if got := frontendOfTestService(service).ProjectRootFor(session); got != "" {
		t.Fatalf("未绑定的会话 ProjectRootFor = %q，希望空串", got)
	}
	service.Deps.Workspace.BindSession(session, "project-1")
	if got := frontendOfTestService(service).ProjectRootFor(session); got != `C:\proj` {
		t.Fatalf("绑定后 ProjectRootFor = %q，希望工作区的 RootPath（C:\\proj）", got)
	}

	// 运行态取的就是计划载荷里那一格（TeamworkPlan.State 就是 TeamworkState）。
	plan := NewFrontendPorts(service).ProjectBoard.ProjectBoardTeamPlanFor(session)
	if plan == nil || plan.TeamID != "team-1" {
		t.Fatalf("装配后计划 = %+v，希望 team-1", plan)
	}
	state := NewFrontendPorts(service).ProjectBoard.ProjectBoardTeamStateFor(session)
	if state == nil || state.State != store.TeamworkStateClosed {
		t.Fatalf("装配后运行态 = %+v，希望 closed", state)
	}
	if got := len(frontendOfTestService(service).WorkTableRowsFor(session)); got != 0 {
		t.Fatalf("空注册表的工作表格行数 = %d，希望 0", got)
	}

	// 未装配端口（Runtime 不实现 board.TeamworkPlanSource）⇒ 计划与运行态都是 nil：
	// "没有就是没有"，而不是让 RuntimePort 长出恒返回零值的成员。
	plain := newTestService(t, &fakeEngine{})
	if plan := NewFrontendPorts(plain).ProjectBoard.ProjectBoardTeamPlanFor(plain.Snapshot().Session.ID); plan != nil {
		t.Fatalf("未装配时计划 = %+v，希望 nil", plan)
	}
	if state := NewFrontendPorts(plain).ProjectBoard.ProjectBoardTeamStateFor(plain.Snapshot().Session.ID); state != nil {
		t.Fatalf("未装配时运行态 = %+v，希望 nil", state)
	}
	if _, ok := plain.Deps.Runtime.(board.TeamworkPlanSource); ok {
		t.Fatal("fakeRuntime 不该实现 board.TeamworkPlanSource（这个负例才有意义）")
	}
}

// boardPlanRuntime 是"Runtime 额外实现 board.TeamworkPlanSource"的桩（窄可选端口的正例）。

type boardPlanRuntime struct {
	*fakeRuntime
	plan store.TeamworkPlan
}

func (runtime *boardPlanRuntime) TeamworkPlanFor(sessionID string) (store.TeamworkPlan, bool) {
	if strings.TrimSpace(sessionID) == "" {
		return store.TeamworkPlan{}, false
	}
	return runtime.plan, true
}

// ---------------------------------------------------------------------------
// T3.6（主用例）：看板挂在**既有**同步点上——真实工具完成路径 ⇒ 订阅口收到本次调用
// ---------------------------------------------------------------------------

// recordingBoardSink 是"注入的订阅口"的记录替身。
//
// 它可选地把调用**转发**给真同步器：转发路径上判据仍只有同步器那一份——本替身只负责
// 记下"完成段到底把什么交给了下家"（名字/错误/会话），不复制任何判定。
type recordingBoardSink struct {
	mu    sync.Mutex
	calls []boardSinkCall
	next  board.ProjectBoardSink
}

type boardSinkCall struct {
	name      string
	err       error
	sessionID string
}

func (sink *recordingBoardSink) OnToolComplete(name string, err error, sessionID string) {
	sink.mu.Lock()
	sink.calls = append(sink.calls, boardSinkCall{name: name, err: err, sessionID: sessionID})
	next := sink.next
	sink.mu.Unlock()
	if next != nil {
		next.OnToolComplete(name, err, sessionID)
	}
}

func (sink *recordingBoardSink) snapshot() []boardSinkCall {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return append([]boardSinkCall(nil), sink.calls...)
}

// boardSessionRows 取"该会话在该文件里"的行数。
//
// 落点**按会话分目录**（`<项目根>/.seelex/board/<会话 ID>/<file>`）：这个会话的行只可能在
// 它自己的文件里，所以行数 = 该文件的折叠读行数。多会话断言因此天然互不串味——这正是
// 分目录要的"防串写"，也是本文件不再需要按 scope.session 过滤的原因。

func TestProjectBoardHooksOnExistingSyncPoint(t *testing.T) {
	const idle = 20 * time.Millisecond
	service, runtime, session := newBoardTestService(t)
	runtime.todoItems = []dto.TodoItem{{Text: "调研", Status: dto.TodoItemDoing}}

	// 装配期交接（生产里这一步在 wiring/bootstrap）：注入订阅口；取数走**既有活体读法**
	// （service.WorkTableRowsFor），不重算任何状态。
	st := boardtest.NewStore()
	// 装配入参就地拼一次（不另立夹具函数）：值与 middle/view/board/boardtest 的
	// 夹具常量同源（落点 = boardtest.ProjectRoot、触发档 = 默认档、时钟 = boardtest.At）。
	syncer := board.NewProjectBoardSyncer(board.ProjectBoardDeps{
		Store:          st,
		WorkTableRows:  frontendOfTestService(service).WorkTableRowsFor,
		EvidenceChars:  func() int { return 800 },
		ProjectRootFor: func(string) string { return boardtest.ProjectRoot },
		Triggers:       board.TriggerFor(board.ProjectBoardHookModeTodoList),
		FlushIdle:      idle,
		Now:            func() time.Time { return time.Unix(boardtest.At, 0) },
	})
	defer syncer.Close()
	recorder := &recordingBoardSink{next: syncer}
	service.SetProjectBoardSink(recorder)

	workFile := store.ProjectBoardWorkFile
	complete := func(ctx context.Context, name, id string, toolErr error) {
		service.handleToolCompleteObserved(ctx, name, id, "", "result", toolErr, time.Millisecond, nil)
	}

	// ① 命中触发面（todo_list 档的 todo_add）：真实完成路径把本次调用交给了订阅口。
	complete(withSessionID(context.Background(), session), "todo_add", "tool-1", nil)
	calls := recorder.snapshot()
	if len(calls) != 1 {
		t.Fatalf("完成段转发给 sink 的次数 = %d，希望 1（%+v）", len(calls), calls)
	}
	if calls[0].name != "todo_add" || calls[0].err != nil || calls[0].sessionID != session {
		t.Fatalf("sink 收到 (%q, %v, %q)，希望 (todo_add, <nil>, %q)", calls[0].name, calls[0].err, calls[0].sessionID, session)
	}
	// 缝不是"通了但没效果"：投影真的按既有落点落了盘。
	boardtest.WaitForWrites(t, st, workFile, 1)
	if got := boardtest.SessionRows(t, st, session, workFile); got == 0 {
		t.Fatal("命中触发面后 worktable 里一行都没有")
	}
	afterHit := boardtest.SessionRows(t, st, session, workFile)

	// ② 未命中触发面（read_file 不在 todo_list 档）：投影**零动作**——不落盘、不加行。
	//
	// 完成段照旧把调用交给订阅口（触发面判定只有同步器一份，见本文件 配套 1/2：
	// 这里若再判一次就成了第二份判据）。所以"未命中不被调用"落在**效果面**上：
	// 下家收到调用后一行都不写。这一格是 M3 的接口事实，故显式断言"确实转发了"。
	complete(withSessionID(context.Background(), session), "read_file", "tool-2", nil)
	if got := len(recorder.snapshot()); got != 2 {
		t.Fatalf("完成段转发次数 = %d，希望 2（未命中也要交接，判据在下家）", got)
	}
	boardtest.Settle(idle)
	if got := boardtest.SessionRows(t, st, session, workFile); got != afterHit {
		t.Fatalf("未命中触发面却写了投影：行数 %d → %d", afterHit, got)
	}

	// ③ 工具失败：同样零动作（I4：失败的调用不改动"活"），但错误要原样交给下家。
	boom := errors.New("tool failed")
	complete(withSessionID(context.Background(), session), "todo_add", "tool-3", boom)
	last := recorder.snapshot()[2]
	if last.err != boom {
		t.Fatalf("sink 收到的 err = %v，希望原样透传 %v", last.err, boom)
	}
	boardtest.Settle(idle)
	if got := boardtest.SessionRows(t, st, session, workFile); got != afterHit {
		t.Fatalf("失败调用却写了投影：行数 %d → %d", afterHit, got)
	}

	// ④ 会话归属取"完成段当场"的会话，不退化成当前视图会话（后台会话的完成
	// 必须落在它自己身上，否则投影会把两个会话的行搅在一起）。
	complete(withSessionID(context.Background(), "session-other"), "todo_add", "tool-4", nil)
	if got := recorder.snapshot()[3]; got.sessionID != "session-other" {
		t.Fatalf("后台完成的会话 = %q，希望 session-other", got.sessionID)
	}

	// ⑤ 订阅口为 nil（能力缺席 / 装配根没挂）：完成段判空后什么都不做——不 panic、
	// 不再转交、不落盘。这就是核心侧"关就是关"的那一半（另一半是装配门，见 wi-m3-switch）。
	service.SetProjectBoardSink(nil)
	complete(withSessionID(context.Background(), session), "todo_add", "tool-5", nil)
	boardtest.Settle(idle)
	if got := len(recorder.snapshot()); got != 4 {
		t.Fatalf("收回订阅口后仍转发了 %d 次（希望仍是 4）", got)
	}
	if got := boardtest.SessionRows(t, st, session, workFile); got != afterHit {
		t.Fatalf("收回订阅口后仍写了投影：行数 %d → %d", afterHit, got)
	}
}

// ---------------------------------------------------------------------------
// T3.4 缺口（写盘失败）：不落半行、记账不推进 ⇒ 下次触发重试
// ---------------------------------------------------------------------------

// boardtest.FlakyStore 是"头 N 次追加失败"的存储面替身：失败不落任何行（模拟追加未提交）。
