package core

// project_board_test.go — M2 交付的单测（T2.1–T2.9 + 触发表/落点/I6 三条配套）。
//
// 判据一句话：**投影读出来的每一行都能在活体投影里指到出处；活体与领域判定永远不看投影。**
// （T2.1 一致、T2.2 零判定、T2.9 反例；其余是同步器语义。）

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

const (
	boardTestProject = "ws-board"
	boardTestAt      = int64(1700000000)
)

// ---------------------------------------------------------------------------
// 夹具
// ---------------------------------------------------------------------------

// fakeBoardStore 是 ProjectBoardStore 的内存实现：模拟存储面的两个语义
// （水位 = 文件内最大 Seq；追加即可读回），只够同步器用。
type fakeBoardStore struct {
	mu      sync.Mutex
	rows    map[string][]sessionstore.ProjectBoardRow
	appends []fakeBoardAppend
}

type fakeBoardAppend struct {
	project string
	session string
	file    string
	rows    []sessionstore.ProjectBoardRow
}

func newFakeBoardStore() *fakeBoardStore {
	return &fakeBoardStore{rows: make(map[string][]sessionstore.ProjectBoardRow)}
}

func boardStoreKey(project, session, file string) string {
	return project + "/" + session + "/" + file
}

func (store *fakeBoardStore) AppendProjectBoardRows(project, session, file string, rows []sessionstore.ProjectBoardRow) (uint64, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.appends = append(store.appends, fakeBoardAppend{
		project: project, session: session, file: file,
		rows: append([]sessionstore.ProjectBoardRow(nil), rows...),
	})
	if len(rows) > 0 {
		key := boardStoreKey(project, session, file)
		store.rows[key] = append(store.rows[key], rows...)
	}
	return store.watermarkLocked(project, session, file), nil
}

func (store *fakeBoardStore) ProjectBoardWatermark(project, session, file string) (uint64, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.watermarkLocked(project, session, file), nil
}

func (store *fakeBoardStore) watermarkLocked(project, session, file string) uint64 {
	var watermark uint64
	for _, row := range store.rows[boardStoreKey(project, session, file)] {
		if row.Seq > watermark {
			watermark = row.Seq
		}
	}
	return watermark
}

func (store *fakeBoardStore) writeCount(file string) int {
	store.mu.Lock()
	defer store.mu.Unlock()
	count := 0
	for _, call := range store.appends {
		if call.file == file && len(call.rows) > 0 {
			count++
		}
	}
	return count
}

func (store *fakeBoardStore) lastWrite(file string) []sessionstore.ProjectBoardRow {
	store.mu.Lock()
	defer store.mu.Unlock()
	for index := len(store.appends) - 1; index >= 0; index-- {
		if store.appends[index].file == file && len(store.appends[index].rows) > 0 {
			return store.appends[index].rows
		}
	}
	return nil
}

// raw 是文件的**原始行**（未折叠；I6 断言"空集合不建文件"用它）。
func (store *fakeBoardStore) raw(project, session, file string) []sessionstore.ProjectBoardRow {
	store.mu.Lock()
	defer store.mu.Unlock()
	return append([]sessionstore.ProjectBoardRow(nil), store.rows[boardStoreKey(project, session, file)]...)
}

// folded 复用存储面**唯一读法**（折叠读），不自己实现第二份。
func (store *fakeBoardStore) folded(project, session, file string) []sessionstore.ProjectBoardRow {
	return sessionstore.FoldProjectBoardRows(store.raw(project, session, file), nil)
}

func (store *fakeBoardStore) dropFile(project, session, file string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.rows, boardStoreKey(project, session, file))
}

// newBoardTestService 造一个带 fakeRuntime 的 Service，并返回其**当前视图会话**
// （视图会话 = 实时 task 注册表，与打点表/工作表格的取数口径一致）。
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

// boardDeps 造同步器入参（默认走默认触发档，落点固定 ws-board）。
func boardDeps(store ProjectBoardStore, idle time.Duration, rows func(string) []WorkItem) ProjectBoardDeps {
	return ProjectBoardDeps{
		Store:         store,
		WorkTableRows: rows,
		ProjectIDFor:  func(string) string { return boardTestProject },
		Triggers:      TriggerFor("todo_list"),
		FlushIdle:     idle,
		Now:           func() time.Time { return time.Unix(boardTestAt, 0) },
	}
}

func waitForBoardWrites(t *testing.T, store *fakeBoardStore, file string, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if store.writeCount(file) >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("project board %s 落盘次数 = %d，希望至少 %d", file, store.writeCount(file), want)
}

// settleBoard 等过若干倍静默窗：用来证明"没有第 N 次落盘"（消极断言需要时间）。
func settleBoard(idle time.Duration) {
	time.Sleep(6 * idle)
}

func marshalBoardRows(t *testing.T, rows []sessionstore.ProjectBoardRow) string {
	t.Helper()
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("编码投影行: %v", err)
	}
	return string(encoded)
}

func boardRowByKey(rows []sessionstore.ProjectBoardRow, key string) (sessionstore.ProjectBoardRow, bool) {
	for _, row := range rows {
		if row.Key == key {
			return row, true
		}
	}
	return sessionstore.ProjectBoardRow{}, false
}

func boardPayloadText(row sessionstore.ProjectBoardRow, key string) string {
	value, ok := row.Payload[key].(string)
	if !ok {
		return ""
	}
	return value
}

// boardKeys 取行集的 key 集（排序后），用于"投影/不投影"的集合断言。
func boardKeys(rows []sessionstore.ProjectBoardRow) []string {
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, row.Key)
	}
	sort.Strings(keys)
	return keys
}

func boardTodoItem(id, text, status string) WorkItem {
	return WorkItem{ID: id, Kind: "todo", Phase: "tasklist", Task: text, Status: status}
}

func boardTaskItem(id, text, status string) WorkItem {
	return WorkItem{ID: id, Kind: "task", Phase: "task", Task: text, Status: status}
}

// boardLiveRows 是"活体取数"的测试替身：flush 在独立 goroutine 上读它，测试在另一个
// goroutine 上改它——必须自己加锁（真装配里那一侧是 Service 的视图锁）。
type boardLiveRows struct {
	mu    sync.Mutex
	items []WorkItem
}

func newBoardLiveRows(items ...WorkItem) *boardLiveRows {
	return &boardLiveRows{items: items}
}

func (live *boardLiveRows) set(items []WorkItem) {
	live.mu.Lock()
	defer live.mu.Unlock()
	live.items = items
}

func (live *boardLiveRows) add(item WorkItem) {
	live.mu.Lock()
	defer live.mu.Unlock()
	live.items = append(live.items, item)
}

func (live *boardLiveRows) drop(index int) {
	live.mu.Lock()
	defer live.mu.Unlock()
	if index < 0 || index >= len(live.items) {
		return
	}
	live.items = append(live.items[:index:index], live.items[index+1:]...)
}

func (live *boardLiveRows) rows() []WorkItem {
	live.mu.Lock()
	defer live.mu.Unlock()
	return append([]WorkItem(nil), live.items...)
}

// ---------------------------------------------------------------------------
// T2.1 核心一致性：同一输入跑两遍，折叠读结果与活体投影逐行相等
// ---------------------------------------------------------------------------

func TestBuildProjectBoardRowsMatchesLiveProjection(t *testing.T) {
	service, runtime, session := newBoardTestService(t)
	runtime.todoItems = []dto.TodoItem{{Text: "调研", Status: dto.TodoItemDoing}}
	runtime.tasks = map[string]dto.TaskRecord{
		"task:1":     {ID: "task:1", Phase: "task", Task: "写投影", Status: dto.TaskRunning, Kind: "task"},
		"plan:n1":    {ID: "plan:n1", Phase: "plan", Task: "计划节点", Status: dto.TaskPending, Kind: "plan", SourceID: "n1"},
		"subagent:1": {ID: "subagent:1", Phase: "subagent", Task: "子代理", Status: dto.TaskRunning, Kind: "subagent"},
	}
	live := service.WorkTableRowsFor(session)
	if len(live) != 4 {
		t.Fatalf("活体工作表格行数 = %d，希望 4（todo/task/plan/subagent 各一）", len(live))
	}

	scope := sessionstore.ProjectBoardScope{Project: boardTestProject, Session: session}
	rows := BuildProjectBoardRows(scope, 1, boardTestAt, live, nil, nil)
	again := BuildProjectBoardRows(scope, 1, boardTestAt, live, nil, nil)
	if marshalBoardRows(t, rows) != marshalBoardRows(t, again) {
		t.Fatalf("同一输入跑两遍结果不同：%s vs %s", marshalBoardRows(t, rows), marshalBoardRows(t, again))
	}

	// 活体侧的期望行：kind ∈ {todo, task}（测试自己列，不复用生产判据表），折叠读排序口径
	//（Kind, Key）下逐行对齐。
	expected := make([]WorkItem, 0, len(live))
	for _, item := range live {
		if item.Kind == "todo" || item.Kind == "task" {
			expected = append(expected, item)
		}
	}
	sort.Slice(expected, func(left, right int) bool { return expected[left].ID < expected[right].ID })

	folded := sessionstore.FoldProjectBoardRows(rows, nil)
	if len(folded) != len(expected) {
		t.Fatalf("折叠读行数 = %d，希望 %d（活体里的 todo/task 行数）", len(folded), len(expected))
	}
	for index, row := range folded {
		item := expected[index]
		if row.Kind != sessionstore.ProjectBoardKindWork {
			t.Fatalf("折叠读第 %d 行 kind = %q，希望 work", index, row.Kind)
		}
		if row.Key != "work:"+item.ID {
			t.Fatalf("折叠读第 %d 行 key = %q，希望 work:%s", index, row.Key, item.ID)
		}
		if got := boardPayloadText(row, "status"); got != item.Status {
			t.Fatalf("折叠读第 %d 行 status = %q，活体 = %q", index, got, item.Status)
		}
		if got := boardPayloadText(row, "task"); got != item.Task {
			t.Fatalf("折叠读第 %d 行 task = %q，活体 = %q", index, got, item.Task)
		}
		if row.Seq != 1 || row.At != boardTestAt || row.Schema != sessionstore.ProjectBoardSchema {
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

func TestBuildProjectBoardRowsNoJudgement(t *testing.T) {
	scope := sessionstore.ProjectBoardScope{Project: boardTestProject, Session: "s-1"}
	items := []WorkItem{
		{ID: "todo:0", Kind: "todo", Phase: "tasklist", Task: "a", Status: "x", Dependencies: []string{"task:9"}},
		boardTaskItem("task:1", "b", "y"),
	}
	plan := &sessionstore.TeamworkPlan{Milestones: []sessionstore.TeamworkMilestone{{
		ID: "m1", Status: "declared-weird", DependsOn: []string{"m0"},
		Items: []sessionstore.TeamworkWorkItem{{
			ID: "wi1", Role: "exec", Name: "干活", Status: "item-weird", DependsOn: []string{"wi0"},
		}},
	}}}
	state := &sessionstore.TeamworkState{Milestones: map[string]string{"m1": "runtime-weird"}}

	rows := BuildProjectBoardRows(scope, 3, boardTestAt, items, plan, state)

	todo, ok := boardRowByKey(rows, "work:todo:0")
	if !ok {
		t.Fatal("work:todo:0 没有投影")
	}
	if got := boardPayloadText(todo, "status"); got != "x" {
		t.Fatalf("work 行 status = %q，希望原样 x", got)
	}
	dependencies, _ := todo.Payload["dependencies"].([]string)
	if len(dependencies) != 1 || dependencies[0] != "task:9" {
		t.Fatalf("work 行 dependencies = %v，希望原样搬运", todo.Payload["dependencies"])
	}

	milestone, ok := boardRowByKey(rows, "milestone:m1")
	if !ok {
		t.Fatal("milestone:m1 没有投影")
	}
	if got := boardPayloadText(milestone, "status"); got != "runtime-weird" {
		t.Fatalf("里程碑 status = %q，希望运行态原样 runtime-weird", got)
	}
	item, ok := boardRowByKey(rows, "milestone_item:wi1")
	if !ok {
		t.Fatal("milestone_item:wi1 没有投影")
	}
	if got := boardPayloadText(item, "status"); got != "item-weird" {
		t.Fatalf("工作项 status = %q，希望原样 item-weird", got)
	}
	if got := boardPayloadText(item, "milestone"); got != "m1" {
		t.Fatalf("工作项 milestone = %q，希望父节点 id m1", got)
	}

	// 空状态**不补值**（"空 = pending"是存储层的约定，投影不发明第二份默认）。
	emptyPlan := &sessionstore.TeamworkPlan{Milestones: []sessionstore.TeamworkMilestone{{
		ID: "m2", Items: []sessionstore.TeamworkWorkItem{{ID: "wi2", Role: "exec", Status: ""}},
	}}}
	empty := BuildProjectBoardRows(scope, 3, boardTestAt, nil, emptyPlan, nil)
	for _, key := range []string{"milestone:m2", "milestone_item:wi2"} {
		row, found := boardRowByKey(empty, key)
		if !found {
			t.Fatalf("%s 没有投影", key)
		}
		if _, present := row.Payload["status"]; present {
			t.Fatalf("%s 落了空状态（status=%q）：投影不替存储层补默认值", key, boardPayloadText(row, "status"))
		}
	}
}

// ---------------------------------------------------------------------------
// T2.3 kind 映射表：plan/todo/task/subagent/tasklist 每个 kind 显式投影/不投影
// ---------------------------------------------------------------------------

func TestBuildProjectBoardRowsKindMapping(t *testing.T) {
	scope := sessionstore.ProjectBoardScope{Project: boardTestProject, Session: "s-1"}
	items := []WorkItem{
		{ID: "plan:n1", Kind: "plan", Phase: "plan", Task: "p", Status: "running"},
		boardTodoItem("todo:0", "t0", "doing"),
		boardTaskItem("task:1", "t1", "running"),
		{ID: "subagent:2", Kind: "subagent", Phase: "subagent", Task: "s", Status: "running"},
		{ID: "weird:3", Kind: "tasklist", Phase: "tasklist", Task: "w", Status: "running"},
	}
	rows := BuildProjectBoardRows(scope, 1, boardTestAt, items, nil, nil)
	// 每种 kind 显式一句：投影 / 不投影 + 理由（理由与 project_board.go 的判据表一致）。
	cases := []struct {
		kind      string
		projected bool
		why       string
	}{
		{"todo", true, "自己列的活（todolist）"},
		{"task", true, "主动登记的活（taskadd）"},
		{"plan", false, "计划节点是过程、整份替换型内容，权威读面是计划/团队面"},
		{"subagent", false, "子代理是执行隔离体（一次性、随派发爆炸），看板看的是这件事而不是跑它的容器"},
		{"tasklist", false, "它不是 kind（todo 行的 phase 才是），过程类一律不搬"},
	}
	for _, testCase := range cases {
		projected := false
		for _, row := range rows {
			if row.Kind == sessionstore.ProjectBoardKindWork && boardPayloadText(row, "kind") == testCase.kind {
				projected = true
			}
		}
		if projected != testCase.projected {
			t.Fatalf("kind=%s 投影 = %v，希望 %v（%s）", testCase.kind, projected, testCase.projected, testCase.why)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("投影行数 = %d，希望 2（只有 todo/task）", len(rows))
	}
}

// ---------------------------------------------------------------------------
// T2.4 debounce：同批多次触发只落一次
// ---------------------------------------------------------------------------

func TestProjectBoardSyncerDebounce(t *testing.T) {
	const idle = 20 * time.Millisecond
	store := newFakeBoardStore()
	live := newBoardLiveRows(boardTodoItem("todo:0", "a", "doing"))
	syncer := NewProjectBoardSyncer(boardDeps(store, idle, func(string) []WorkItem { return live.rows() }))
	defer syncer.Close()

	for index := 0; index < 5; index++ {
		syncer.OnToolComplete("todo_add", nil, "s-1")
	}
	waitForBoardWrites(t, store, sessionstore.ProjectBoardWorkFile, 1)
	settleBoard(idle)
	if got := store.writeCount(sessionstore.ProjectBoardWorkFile); got != 1 {
		t.Fatalf("同批 5 次触发落盘 %d 次，希望 1 次", got)
	}
}

// ---------------------------------------------------------------------------
// T2.5 指纹去重：内容没变不写盘
// ---------------------------------------------------------------------------

func TestProjectBoardSyncerFingerprint(t *testing.T) {
	const idle = 20 * time.Millisecond
	store := newFakeBoardStore()
	live := newBoardLiveRows(boardTodoItem("todo:0", "a", "doing"))
	syncer := NewProjectBoardSyncer(boardDeps(store, idle, func(string) []WorkItem { return live.rows() }))
	defer syncer.Close()

	syncer.OnToolComplete("todo_add", nil, "s-1")
	waitForBoardWrites(t, store, sessionstore.ProjectBoardWorkFile, 1)

	// 活体变了 ⇒ 必须落第二次（这一步同时证明"管道还活着"，否则"没落盘"的断言毫无意义）。
	live.add(boardTaskItem("task:1", "b", "running"))
	syncer.OnToolComplete("todo_add", nil, "s-1")
	waitForBoardWrites(t, store, sessionstore.ProjectBoardWorkFile, 2)

	// 内容没变（连触发都给了）⇒ 不写盘。
	syncer.OnToolComplete("todo_add", nil, "s-1")
	syncer.OnToolComplete("task_add", nil, "s-1")
	settleBoard(idle)
	if got := store.writeCount(sessionstore.ProjectBoardWorkFile); got != 2 {
		t.Fatalf("内容没变仍落盘 %d 次，希望 2 次（指纹去重）", got)
	}
	if got := len(store.folded(boardTestProject, "s-1", sessionstore.ProjectBoardWorkFile)); got != 2 {
		t.Fatalf("折叠读行数 = %d，希望 2", got)
	}
}

// ---------------------------------------------------------------------------
// T2.6 水位：seq = 水位 + 1；重复提交是空操作
// ---------------------------------------------------------------------------

func TestProjectBoardSyncerWatermark(t *testing.T) {
	const idle = 20 * time.Millisecond
	store := newFakeBoardStore()
	live := newBoardLiveRows(boardTodoItem("todo:0", "a", "doing"))
	syncer := NewProjectBoardSyncer(boardDeps(store, idle, func(string) []WorkItem { return live.rows() }))
	defer syncer.Close()

	syncer.OnToolComplete("todo_add", nil, "s-1")
	waitForBoardWrites(t, store, sessionstore.ProjectBoardWorkFile, 1)
	first := store.lastWrite(sessionstore.ProjectBoardWorkFile)
	if len(first) != 1 || first[0].Seq != 1 {
		t.Fatalf("第一批 seq = %+v，希望 1（水位 0 + 1）", first)
	}
	if watermark, _ := store.ProjectBoardWatermark(boardTestProject, "s-1", sessionstore.ProjectBoardWorkFile); watermark != 1 {
		t.Fatalf("水位 = %d，希望 1", watermark)
	}

	// 重复提交（内容没变）⇒ 空操作：水位不动、没有新的追加批。
	syncer.OnToolComplete("todo_add", nil, "s-1")
	settleBoard(idle)
	if got := store.writeCount(sessionstore.ProjectBoardWorkFile); got != 1 {
		t.Fatalf("重复提交落盘 %d 次，希望仍是 1 次", got)
	}
	if watermark, _ := store.ProjectBoardWatermark(boardTestProject, "s-1", sessionstore.ProjectBoardWorkFile); watermark != 1 {
		t.Fatalf("重复提交后水位 = %d，希望仍是 1", watermark)
	}

	// 内容变了 ⇒ 水位推进：下一批 seq = 2。
	live.add(boardTaskItem("task:1", "b", "running"))
	syncer.OnToolComplete("todo_add", nil, "s-1")
	waitForBoardWrites(t, store, sessionstore.ProjectBoardWorkFile, 2)
	second := store.lastWrite(sessionstore.ProjectBoardWorkFile)
	for _, row := range second {
		if row.Seq != 2 {
			t.Fatalf("第二批 seq = %d，希望 2（水位 1 + 1）", row.Seq)
		}
	}
	if watermark, _ := store.ProjectBoardWatermark(boardTestProject, "s-1", sessionstore.ProjectBoardWorkFile); watermark != 2 {
		t.Fatalf("水位 = %d，希望 2", watermark)
	}
}

// ---------------------------------------------------------------------------
// T2.7 里程碑状态：运行态优先、声明态兜底；整队收口读 TeamworkState.State
// ---------------------------------------------------------------------------

func TestProjectBoardTeamworkMilestonePriority(t *testing.T) {
	scope := sessionstore.ProjectBoardScope{Project: boardTestProject, Session: "s-1"}
	plan := &sessionstore.TeamworkPlan{Milestones: []sessionstore.TeamworkMilestone{
		{ID: "m1", Name: "第一步", Status: sessionstore.TeamworkMilestonePending, DependsOn: []string{"m0"}},
		{ID: "m2", Name: "第二步", Status: sessionstore.TeamworkMilestoneDone},
	}}

	// 运行态优先：m1 在运行态里是 active（计划里声明的是 pending）；m2 运行态没说 ⇒ 声明兜底。
	state := &sessionstore.TeamworkState{Milestones: map[string]string{"m1": sessionstore.TeamworkMilestoneActive}}
	rows := BuildProjectBoardRows(scope, 1, boardTestAt, nil, plan, state)
	m1, _ := boardRowByKey(rows, "milestone:m1")
	if got := boardPayloadText(m1, "status"); got != sessionstore.TeamworkMilestoneActive {
		t.Fatalf("m1 status = %q，希望运行态 active（运行态优先）", got)
	}
	m2, _ := boardRowByKey(rows, "milestone:m2")
	if got := boardPayloadText(m2, "status"); got != sessionstore.TeamworkMilestoneDone {
		t.Fatalf("m2 status = %q，希望声明态 done（运行态兜底）", got)
	}

	// 运行态整份缺席 ⇒ 全用声明态。
	rows = BuildProjectBoardRows(scope, 1, boardTestAt, nil, plan, nil)
	m1, _ = boardRowByKey(rows, "milestone:m1")
	if got := boardPayloadText(m1, "status"); got != sessionstore.TeamworkMilestonePending {
		t.Fatalf("无运行态时 m1 status = %q，希望声明态 pending", got)
	}

	// 整队收口读 TeamworkState.State：空 = 未收口；closed = 已收口（**不是**别的字段）。
	open := BuildProjectBoardRows(scope, 1, boardTestAt, nil, plan, &sessionstore.TeamworkState{})
	openRow, _ := boardRowByKey(open, "milestone:m1")
	if openRow.Payload["team_closed"] != false {
		t.Fatalf("未收口时 team_closed = %v，希望 false", openRow.Payload["team_closed"])
	}
	if _, present := openRow.Payload["team_state"]; present {
		t.Fatalf("未收口时空 State 不该落 team_state（%v）", openRow.Payload["team_state"])
	}
	closed := BuildProjectBoardRows(scope, 1, boardTestAt, nil, plan,
		&sessionstore.TeamworkState{State: sessionstore.TeamworkStateClosed})
	closedRow, _ := boardRowByKey(closed, "milestone:m1")
	if closedRow.Payload["team_closed"] != true {
		t.Fatalf("收口后 team_closed = %v，希望 true", closedRow.Payload["team_closed"])
	}
	if got := boardPayloadText(closedRow, "team_state"); got != sessionstore.TeamworkStateClosed {
		t.Fatalf("收口后 team_state = %q，希望 closed", got)
	}

	// 落点：里程碑行进 teamwork.jsonl；没有 work 行 ⇒ **不建** worktable.jsonl（I6）。
	const idle = 20 * time.Millisecond
	store := newFakeBoardStore()
	deps := boardDeps(store, idle, func(string) []WorkItem { return nil })
	deps.TeamPlanFor = func(string) *sessionstore.TeamworkPlan { return plan }
	deps.TeamStateFor = func(string) *sessionstore.TeamworkState {
		return &sessionstore.TeamworkState{State: sessionstore.TeamworkStateClosed}
	}
	syncer := NewProjectBoardSyncer(deps)
	defer syncer.Close()
	syncer.OnToolComplete("todo_add", nil, "s-1")
	waitForBoardWrites(t, store, sessionstore.ProjectBoardTeamworkFile, 1)
	if got := len(store.folded(boardTestProject, "s-1", sessionstore.ProjectBoardTeamworkFile)); got != 2 {
		t.Fatalf("teamwork 折叠读行数 = %d，希望 2 条里程碑行", got)
	}
	if raw := store.raw(boardTestProject, "s-1", sessionstore.ProjectBoardWorkFile); len(raw) != 0 {
		t.Fatalf("没有 work 行却建了 worktable.jsonl：%d 行（I6：空集合不建文件）", len(raw))
	}
}

// ---------------------------------------------------------------------------
// T2.8 I1：删文件后重建结果相同（指纹去重不得把一次误删固化成永久缺失）
// ---------------------------------------------------------------------------

func TestProjectBoardRebuildAfterDelete(t *testing.T) {
	const idle = 20 * time.Millisecond
	store := newFakeBoardStore()
	live := newBoardLiveRows(boardTodoItem("todo:0", "a", "doing"), boardTaskItem("task:1", "b", "running"))
	syncer := NewProjectBoardSyncer(boardDeps(store, idle, func(string) []WorkItem { return live.rows() }))
	defer syncer.Close()

	syncer.OnToolComplete("todo_add", nil, "s-1")
	waitForBoardWrites(t, store, sessionstore.ProjectBoardWorkFile, 1)
	before := marshalBoardRows(t, store.folded(boardTestProject, "s-1", sessionstore.ProjectBoardWorkFile))

	// 文件被删（人删 / 数据根搬迁 / 掉了一块盘）：下一次触发必须重建。
	store.dropFile(boardTestProject, "s-1", sessionstore.ProjectBoardWorkFile)
	syncer.OnToolComplete("todo_add", nil, "s-1")
	waitForBoardWrites(t, store, sessionstore.ProjectBoardWorkFile, 2)
	after := marshalBoardRows(t, store.folded(boardTestProject, "s-1", sessionstore.ProjectBoardWorkFile))
	if before != after {
		t.Fatalf("重建结果与删除前不同：\n before=%s\n after =%s", before, after)
	}
	// 水位从 0 重来 ⇒ 重建批的 seq 又是 1（水位是**文件内**的事实）。
	for _, row := range store.lastWrite(sessionstore.ProjectBoardWorkFile) {
		if row.Seq != 1 {
			t.Fatalf("重建批 seq = %d，希望 1（文件没了，水位归零）", row.Seq)
		}
	}
}

// ---------------------------------------------------------------------------
// T2.9 反例（I2）：篡改投影文件 ⇒ 活体投影与领域判定不变
// ---------------------------------------------------------------------------

func TestProjectBoardIsNotAnInput(t *testing.T) {
	service, runtime, session := newBoardTestService(t)
	runtime.todoItems = []dto.TodoItem{{Text: "调研", Status: dto.TodoItemDoing}}

	before := service.WorkTableRowsFor(session)
	if len(before) != 1 || before[0].Status != "doing" {
		t.Fatalf("活体投影 = %+v，希望一条 doing", before)
	}

	// 篡改投影文件：把那条 todo 改成 done（外加一行 schema/kind 都认不得的垃圾行）。
	store := newFakeBoardStore()
	if _, err := store.AppendProjectBoardRows(boardTestProject, session, sessionstore.ProjectBoardWorkFile,
		[]sessionstore.ProjectBoardRow{
			{Schema: sessionstore.ProjectBoardSchema, Kind: sessionstore.ProjectBoardKindWork, Key: "work:todo:0", Seq: 99, At: boardTestAt,
				Scope:   sessionstore.ProjectBoardScope{Project: boardTestProject, Session: session},
				Payload: map[string]any{"id": "todo:0", "kind": "todo", "status": "done"}},
			{Schema: 99, Kind: "not-a-kind", Key: "work:todo:1", Seq: 100, At: boardTestAt,
				Payload: map[string]any{"status": "done"}},
		}); err != nil {
		t.Fatalf("造假投影文件: %v", err)
	}

	// 文件的读**确实**被改了（否则这个反例什么都没证明）。
	folded := store.folded(boardTestProject, session, sessionstore.ProjectBoardWorkFile)
	if len(folded) != 1 || boardPayloadText(folded[0], "status") != "done" {
		t.Fatalf("篡改后的文件读 = %+v，希望一条 done", folded)
	}

	// 而活体投影与领域判定一字不变：投影**不是任何东西的输入**。
	after := service.WorkTableRowsFor(session)
	if marshalWorkItems(t, after) != marshalWorkItems(t, before) {
		t.Fatalf("篡改投影文件后活体投影变了：\n before=%s\n after =%s", marshalWorkItems(t, before), marshalWorkItems(t, after))
	}
	records := runtime.TaskSnapshotFor(session)
	if len(records) != 1 || records[0].Status != dto.TaskDoing {
		t.Fatalf("篡改投影文件后 task 注册表变了：%+v", records)
	}
	// 投影输入（活体取数）也不受文件影响：再映射一次仍是 doing。
	rows := BuildProjectBoardRows(sessionstore.ProjectBoardScope{Project: boardTestProject, Session: session},
		1, boardTestAt, service.WorkTableRowsFor(session), nil, nil)
	if len(rows) != 1 || boardPayloadText(rows[0], "status") != "doing" {
		t.Fatalf("活体输入映射出的行 = %+v，希望 status=doing", rows)
	}
}

func marshalWorkItems(t *testing.T, items []WorkItem) string {
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

func TestProjectBoardTriggerTable(t *testing.T) {
	cases := []struct {
		mode string
		name string
		want bool
	}{
		{"off", "todo_add", false},
		{"off", "task_complete", false},
		{"", "todo_add", true},           // 空 = 默认档
		{"todo_list", "todo_init", true}, // 默认档六个名
		{"todo_list", "todo_add", true},
		{"todo_list", "todo_done", true},
		{"todo_list", "todo_status", true},
		{"todo_list", "task_add", true},
		{"todo_list", "task_complete", true},
		{"todo_list", "read_file", false},
		{"task_complete", "task_complete", true},
		{"task_complete", "todo_add", false},
		{"tool_complete", "read_file", true},
		{"tool_complete", "todo_add", true},
		{"all", "anything_at_all", true},
		{"unknown-mode", "todo_add", true}, // 未知档 = 默认档
		{"unknown-mode", "read_file", false},
		{"todo_list", "", false}, // 空工具名恒不命中
		{"tool_complete", "", false},
	}
	for _, testCase := range cases {
		if got := TriggerFor(testCase.mode).Matches(testCase.name); got != testCase.want {
			t.Fatalf("TriggerFor(%q).Matches(%q) = %v，希望 %v", testCase.mode, testCase.name, got, testCase.want)
		}
	}
	if !TriggerFor("off").Off() {
		t.Fatal("TriggerFor(off) 应当是「什么都不触发」")
	}
	if TriggerFor("todo_list").Off() {
		t.Fatal("TriggerFor(todo_list) 不该是空触发面")
	}
}

// ---------------------------------------------------------------------------
// 配套 2：失败调用与未命中调用不落盘（OnToolComplete 只判触发面）
// ---------------------------------------------------------------------------

func TestProjectBoardSyncerIgnoresFailedAndUntriggeredCalls(t *testing.T) {
	const idle = 20 * time.Millisecond
	store := newFakeBoardStore()
	live := newBoardLiveRows(boardTodoItem("todo:0", "a", "doing"))
	deps := boardDeps(store, idle, func(string) []WorkItem { return live.rows() })
	deps.Triggers = TriggerFor("task_complete")
	syncer := NewProjectBoardSyncer(deps)
	defer syncer.Close()

	syncer.OnToolComplete("todo_add", nil, "s-1")                     // 未命中触发面
	syncer.OnToolComplete("task_complete", errors.New("boom"), "s-1") // 工具失败
	syncer.OnToolComplete("task_complete", nil, "")                   // 没有归属会话
	settleBoard(idle)
	if got := store.writeCount(sessionstore.ProjectBoardWorkFile); got != 0 {
		t.Fatalf("不该落盘却落了 %d 次", got)
	}

	syncer.OnToolComplete("task_complete", nil, "s-1") // 命中：正常落盘
	waitForBoardWrites(t, store, sessionstore.ProjectBoardWorkFile, 1)
}

// ---------------------------------------------------------------------------
// 配套 3：消失的 key 补 tombstone（upsert 语义下"删掉的条目要能消失"）
// ---------------------------------------------------------------------------

func TestProjectBoardSyncerTombstonesRemovedKeys(t *testing.T) {
	const idle = 20 * time.Millisecond
	store := newFakeBoardStore()
	live := newBoardLiveRows(boardTodoItem("todo:0", "a", "doing"), boardTaskItem("task:1", "b", "running"))
	syncer := NewProjectBoardSyncer(boardDeps(store, idle, func(string) []WorkItem { return live.rows() }))
	defer syncer.Close()

	syncer.OnToolComplete("todo_add", nil, "s-1")
	waitForBoardWrites(t, store, sessionstore.ProjectBoardWorkFile, 1)
	if got := len(store.folded(boardTestProject, "s-1", sessionstore.ProjectBoardWorkFile)); got != 2 {
		t.Fatalf("折叠读行数 = %d，希望 2", got)
	}

	live.drop(1) // 活没了：task:1 从活体投影里消失
	syncer.OnToolComplete("todo_add", nil, "s-1")
	waitForBoardWrites(t, store, sessionstore.ProjectBoardWorkFile, 2)
	batch := store.lastWrite(sessionstore.ProjectBoardWorkFile)
	removed := false
	for _, row := range batch {
		if row.Key == "work:task:1" {
			if row.Payload["removed"] != true {
				t.Fatalf("消失的 key 补的行 = %+v，希望 payload{removed:true}", row)
			}
			removed = true
		}
	}
	if !removed {
		t.Fatalf("没有为消失的 key work:task:1 补 tombstone：%+v", batch)
	}
	folded := store.folded(boardTestProject, "s-1", sessionstore.ProjectBoardWorkFile)
	if got := boardKeys(folded); len(got) != 1 || got[0] != "work:todo:0" {
		t.Fatalf("折叠读 key 集 = %v，希望只剩 work:todo:0", got)
	}
}

// ---------------------------------------------------------------------------
// 配套 4：Close 幂等且不泄漏 goroutine
// ---------------------------------------------------------------------------

func TestProjectBoardSyncerCloseIsIdempotent(t *testing.T) {
	const idle = 20 * time.Millisecond
	store := newFakeBoardStore()
	syncer := NewProjectBoardSyncer(boardDeps(store, idle, func(string) []WorkItem { return nil }))

	syncer.OnToolComplete("todo_add", nil, "s-1")
	done := make(chan struct{})
	go func() {
		syncer.Close()
		syncer.Close() // 第二次 Close 幂等
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close 没有在限期内返回（goroutine 泄漏）")
	}
	// 关闭之后再触发是无害的空操作（不 panic、不落盘）。
	syncer.OnToolComplete("todo_add", nil, "s-1")
	settleBoard(idle)
	if got := store.writeCount(sessionstore.ProjectBoardWorkFile); got != 0 {
		t.Fatalf("Close 之后仍落盘 %d 次", got)
	}
}

// ---------------------------------------------------------------------------
// 配套 5：M3 装配要用的两个只读取数口
// ---------------------------------------------------------------------------

func TestProjectBoardReadersForAssembly(t *testing.T) {
	// 已装配 teamwork 计划端口（Runtime 额外实现了 TeamworkPlanSource）：装配期注入，不做事后改 Deps。
	planSource := &boardPlanRuntime{
		fakeRuntime: &fakeRuntime{},
		plan: sessionstore.TeamworkPlan{
			TeamID: "team-1",
			State:  sessionstore.TeamworkState{State: sessionstore.TeamworkStateClosed},
		},
	}
	service := newTestService(t, &fakeEngine{},
		withTestRuntime(planSource),
		func(deps *Dependencies) { deps.Workspace = newFakeWorkspace() })
	session := service.Snapshot().Session.ID
	planSource.fakeRuntime.currentTaskSession = session

	if _, err := service.Deps.Workspace.Create("proj", `C:\proj`, ""); err != nil {
		t.Fatalf("建工作区: %v", err)
	}
	if got := service.ProjectIDFor(session); got != "" {
		t.Fatalf("未绑定的会话 ProjectIDFor = %q，希望空串", got)
	}
	service.Deps.Workspace.BindSession(session, "project-1")
	if got := service.ProjectIDFor(session); got != "project-1" {
		t.Fatalf("绑定后 ProjectIDFor = %q，希望 project-1", got)
	}

	// 运行态取的就是计划载荷里那一格（TeamworkPlan.State 就是 TeamworkState）。
	plan := service.ProjectBoardTeamPlanFor(session)
	if plan == nil || plan.TeamID != "team-1" {
		t.Fatalf("装配后计划 = %+v，希望 team-1", plan)
	}
	state := service.ProjectBoardTeamStateFor(session)
	if state == nil || state.State != sessionstore.TeamworkStateClosed {
		t.Fatalf("装配后运行态 = %+v，希望 closed", state)
	}
	if got := len(service.WorkTableRowsFor(session)); got != 0 {
		t.Fatalf("空注册表的工作表格行数 = %d，希望 0", got)
	}

	// 未装配端口（Runtime 不实现 TeamworkPlanSource）⇒ 计划与运行态都是 nil：
	// "没有就是没有"，而不是让 RuntimePort 长出恒返回零值的成员。
	plain := newTestService(t, &fakeEngine{})
	if plan := plain.ProjectBoardTeamPlanFor(plain.Snapshot().Session.ID); plan != nil {
		t.Fatalf("未装配时计划 = %+v，希望 nil", plan)
	}
	if state := plain.ProjectBoardTeamStateFor(plain.Snapshot().Session.ID); state != nil {
		t.Fatalf("未装配时运行态 = %+v，希望 nil", state)
	}
	if _, ok := plain.Deps.Runtime.(TeamworkPlanSource); ok {
		t.Fatal("fakeRuntime 不该实现 TeamworkPlanSource（这个负例才有意义）")
	}
}

// boardPlanRuntime 是"Runtime 额外实现 TeamworkPlanSource"的桩（窄可选端口的正例）。
type boardPlanRuntime struct {
	*fakeRuntime
	plan sessionstore.TeamworkPlan
}

func (runtime *boardPlanRuntime) TeamworkPlanFor(sessionID string) (sessionstore.TeamworkPlan, bool) {
	if strings.TrimSpace(sessionID) == "" {
		return sessionstore.TeamworkPlan{}, false
	}
	return runtime.plan, true
}
