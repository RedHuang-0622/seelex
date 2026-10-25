package board

// HEADER-projection_test.go

import (
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/middle/view/board/boardtest"
	"github.com/RedHuang-0622/seelex/store"
)

func TestBuildProjectBoardRowsNoJudgement(t *testing.T) {
	scope := store.ProjectBoardScope{Project: boardtest.Project, Session: "s-1"}
	items := []model.WorkItem{
		{ID: "todo:0", Kind: "todo", Phase: "tasklist", Task: "a", Status: "x", Dependencies: []string{"task:9"}},
		boardtest.TaskItem("task:1", "b", "y"),
	}
	plan := &store.TeamworkPlan{Milestones: []store.TeamworkMilestone{{
		ID: "m1", Status: "declared-weird", DependsOn: []string{"m0"},
		Items: []store.TeamworkWorkItem{{
			ID: "wi1", Role: "exec", Name: "干活", Status: "item-weird", DependsOn: []string{"wi0"},
		}},
	}}}
	state := &store.TeamworkState{Milestones: map[string]string{"m1": "runtime-weird"}}

	rows := BuildProjectBoardRows(scope, 3, boardtest.At, items, plan, state, 800)

	todo, ok := boardtest.RowByKey(rows, "work:todo:0")
	if !ok {
		t.Fatal("work:todo:0 没有投影")
	}
	if got := boardtest.PayloadText(todo, "status"); got != "x" {
		t.Fatalf("work 行 status = %q，希望原样 x", got)
	}
	dependencies, _ := todo.Payload["dependencies"].([]string)
	if len(dependencies) != 1 || dependencies[0] != "task:9" {
		t.Fatalf("work 行 dependencies = %v，希望原样搬运", todo.Payload["dependencies"])
	}

	milestone, ok := boardtest.RowByKey(rows, "milestone:m1")
	if !ok {
		t.Fatal("milestone:m1 没有投影")
	}
	if got := boardtest.PayloadText(milestone, "status"); got != "runtime-weird" {
		t.Fatalf("里程碑 status = %q，希望运行态原样 runtime-weird", got)
	}
	item, ok := boardtest.RowByKey(rows, "milestone_item:wi1")
	if !ok {
		t.Fatal("milestone_item:wi1 没有投影")
	}
	if got := boardtest.PayloadText(item, "status"); got != "item-weird" {
		t.Fatalf("工作项 status = %q，希望原样 item-weird", got)
	}
	if got := boardtest.PayloadText(item, "milestone"); got != "m1" {
		t.Fatalf("工作项 milestone = %q，希望父节点 id m1", got)
	}

	// 空状态**不补值**（"空 = pending"是存储层的约定，投影不发明第二份默认）。
	emptyPlan := &store.TeamworkPlan{Milestones: []store.TeamworkMilestone{{
		ID: "m2", Items: []store.TeamworkWorkItem{{ID: "wi2", Role: "exec", Status: ""}},
	}}}
	empty := BuildProjectBoardRows(scope, 3, boardtest.At, nil, emptyPlan, nil, 800)
	for _, key := range []string{"milestone:m2", "milestone_item:wi2"} {
		row, found := boardtest.RowByKey(empty, key)
		if !found {
			t.Fatalf("%s 没有投影", key)
		}
		if _, present := row.Payload["status"]; present {
			t.Fatalf("%s 落了空状态（status=%q）：投影不替存储层补默认值", key, boardtest.PayloadText(row, "status"))
		}
	}
}

// ---------------------------------------------------------------------------
// T2.3 kind 映射表：plan/todo/task/subagent/tasklist 每个 kind 显式投影/不投影
// ---------------------------------------------------------------------------

func TestBuildProjectBoardRowsKindMapping(t *testing.T) {
	scope := store.ProjectBoardScope{Project: boardtest.Project, Session: "s-1"}
	items := []model.WorkItem{
		{ID: "plan:n1", Kind: "plan", Phase: "plan", Task: "p", Status: "running"},
		boardtest.TodoItem("todo:0", "t0", "doing"),
		boardtest.TaskItem("task:1", "t1", "running"),
		{ID: "subagent:2", Kind: "subagent", Phase: "subagent", Task: "s", Status: "running"},
		{ID: "weird:3", Kind: "tasklist", Phase: "tasklist", Task: "w", Status: "running"},
	}
	rows := BuildProjectBoardRows(scope, 1, boardtest.At, items, nil, nil, 800)
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
			if row.Kind == store.ProjectBoardKindWork && boardtest.PayloadText(row, "kind") == testCase.kind {
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

func TestProjectBoardTeamworkMilestonePriority(t *testing.T) {
	scope := store.ProjectBoardScope{Project: boardtest.Project, Session: "s-1"}
	plan := &store.TeamworkPlan{Milestones: []store.TeamworkMilestone{
		{ID: "m1", Name: "第一步", Status: store.TeamworkMilestonePending, DependsOn: []string{"m0"}},
		{ID: "m2", Name: "第二步", Status: store.TeamworkMilestoneDone},
	}}

	// 运行态优先：m1 在运行态里是 active（计划里声明的是 pending）；m2 运行态没说 ⇒ 声明兜底。
	state := &store.TeamworkState{Milestones: map[string]string{"m1": store.TeamworkMilestoneActive}}
	rows := BuildProjectBoardRows(scope, 1, boardtest.At, nil, plan, state, 800)
	m1, _ := boardtest.RowByKey(rows, "milestone:m1")
	if got := boardtest.PayloadText(m1, "status"); got != store.TeamworkMilestoneActive {
		t.Fatalf("m1 status = %q，希望运行态 active（运行态优先）", got)
	}
	m2, _ := boardtest.RowByKey(rows, "milestone:m2")
	if got := boardtest.PayloadText(m2, "status"); got != store.TeamworkMilestoneDone {
		t.Fatalf("m2 status = %q，希望声明态 done（运行态兜底）", got)
	}

	// 运行态整份缺席 ⇒ 全用声明态。
	rows = BuildProjectBoardRows(scope, 1, boardtest.At, nil, plan, nil, 800)
	m1, _ = boardtest.RowByKey(rows, "milestone:m1")
	if got := boardtest.PayloadText(m1, "status"); got != store.TeamworkMilestonePending {
		t.Fatalf("无运行态时 m1 status = %q，希望声明态 pending", got)
	}

	// 整队收口读 TeamworkState.State：空 = 未收口；closed = 已收口（**不是**别的字段）。
	open := BuildProjectBoardRows(scope, 1, boardtest.At, nil, plan, &store.TeamworkState{}, 800)
	openRow, _ := boardtest.RowByKey(open, "milestone:m1")
	if openRow.Payload["team_closed"] != false {
		t.Fatalf("未收口时 team_closed = %v，希望 false", openRow.Payload["team_closed"])
	}
	if _, present := openRow.Payload["team_state"]; present {
		t.Fatalf("未收口时空 State 不该落 team_state（%v）", openRow.Payload["team_state"])
	}
	closed := BuildProjectBoardRows(scope, 1, boardtest.At, nil, plan,
		&store.TeamworkState{State: store.TeamworkStateClosed}, 800)
	closedRow, _ := boardtest.RowByKey(closed, "milestone:m1")
	if closedRow.Payload["team_closed"] != true {
		t.Fatalf("收口后 team_closed = %v，希望 true", closedRow.Payload["team_closed"])
	}
	if got := boardtest.PayloadText(closedRow, "team_state"); got != store.TeamworkStateClosed {
		t.Fatalf("收口后 team_state = %q，希望 closed", got)
	}

	// 落点：里程碑行进 teamwork.jsonl；没有 work 行 ⇒ **不建** worktable.jsonl（I6）。
	const idle = 20 * time.Millisecond
	st := boardtest.NewStore()
	deps := boardDeps(st, idle, func(string) []model.WorkItem { return nil })
	deps.TeamPlanFor = func(string) *store.TeamworkPlan { return plan }
	deps.TeamStateFor = func(string) *store.TeamworkState {
		return &store.TeamworkState{State: store.TeamworkStateClosed}
	}
	syncer := NewProjectBoardSyncer(deps)
	defer syncer.Close()
	syncer.OnToolComplete("todo_add", nil, "s-1")
	boardtest.WaitForWrites(t, st, store.ProjectBoardTeamworkFile, 1)
	if got := len(st.Folded(boardtest.ProjectRoot, "s-1", store.ProjectBoardTeamworkFile)); got != 2 {
		t.Fatalf("teamwork 折叠读行数 = %d，希望 2 条里程碑行", got)
	}
	if raw := st.Raw(boardtest.ProjectRoot, "s-1", store.ProjectBoardWorkFile); len(raw) != 0 {
		t.Fatalf("没有 work 行却建了 worktable.jsonl：%d 行（I6：空集合不建文件）", len(raw))
	}
}

// ---------------------------------------------------------------------------
// T2.8 I1：删文件后重建结果相同（指纹去重不得把一次误删固化成永久缺失）
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
