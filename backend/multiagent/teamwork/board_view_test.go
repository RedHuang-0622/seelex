package teamwork

// board_view_test.go 是团队看板投影判据的**域级**用例（P21-20：随实现从
// `backend/engine/assemble/runtime_teamwork_board.go` 搬进本域）。宿主级用例（快照缓存、存档兜底、
// 失效点）仍留在装配层，走同身份别名覆盖同一份判据。

import (
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/jobs"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/store"
)

func boardPlanFixture() store.TeamworkPlan {
	return store.TeamworkPlan{
		TeamID: "team-1", Version: 3,
		State: store.TeamworkState{State: "active"},
		Members: []store.TeamworkMember{
			{Role: "exec", RoleSessionID: "role-exec", ToolsPolicy: "readwrite", Plugins: []string{"cad"}},
			{Role: "pm", RoleSessionID: "role-pm", ToolsPolicy: "readonly"},
		},
		Milestones: []store.TeamworkMilestone{{
			ID: "m1", Name: "实现", Required: []string{"exec"},
			Items: []store.TeamworkWorkItem{
				{ID: "wi-1", Milestone: "m1", Role: "exec", Name: "写解析", SessionID: "sess-wi-1", Status: store.TeamworkItemRunning, Handle: "bteam-1"},
				{ID: "wi-2", Milestone: "m1", Role: "exec", Name: "写测试", SessionID: "sess-wi-2", Status: store.TeamworkItemDone},
				{ID: "wi-3", Milestone: "m1", Role: "pm", Name: "需求", SessionID: "sess-wi-3", Status: store.TeamworkItemReview},
			},
		}},
	}
}

// TestBuildBoardViewCarriesFactsNotJudgments：看板只搬事实——闭板三字段从**计划**搬、
// 成员/里程碑/工作项/作业/审计五份行齐、以及出图与存档用**同一条**路径（同一函数）。
func TestBuildBoardViewCarriesFactsNotJudgments(t *testing.T) {
	plan := boardPlanFixture()
	plan.State.ClosedAt = 1700000000
	events := []store.TeamworkEvent{{
		At: time.Unix(1700000000, 0), Kind: store.TeamworkEventMessage, Role: "exec",
		WorkItem: "wi-1", Detail: "回执正文",
	}}
	bindings := []store.TeamworkBinding{{WorkItem: "wi-1"}}
	records := []jobs.Record{{Handle: "bteam-1", Node: "wi-1", State: jobs.StateRunning, Scope: jobs.Scope{Subject: SubjectForRole("exec")}}}

	view := BuildBoardView(plan, events, bindings, records, 6, nil)
	if view.TeamID != "team-1" || view.Version != 3 || view.MaxMembers != 6 {
		t.Fatalf("头部字段 = %+v", view)
	}
	if view.State != "active" || view.ClosedAt != 1700000000 {
		t.Fatalf("闭板三字段从计划搬：%+v", view)
	}
	if len(view.Members) != 2 || len(view.Milestones) != 1 || len(view.WorkItems) != 3 ||
		len(view.Jobs) != 1 || len(view.Events) != 1 {
		t.Fatalf("五份行必须齐：members=%d milestones=%d items=%d jobs=%d events=%d",
			len(view.Members), len(view.Milestones), len(view.WorkItems), len(view.Jobs), len(view.Events))
	}
	if view.Jobs[0].Role != "exec" || view.Jobs[0].Node != "wi-1" {
		t.Fatalf("作业行的权威归属（Node + 主体反解）：%+v", view.Jobs[0])
	}
	if view.Members[0].Messages == nil || len(view.Members[0].Messages) != 1 ||
		view.Members[0].Messages[0].Text != "回执正文" {
		t.Fatalf("成员消息来自审计流水：%+v", view.Members[0].Messages)
	}
	if view.WorkItems[0].Live != true {
		t.Fatalf("绑定在册 ⇒ Live：%+v", view.WorkItems[0])
	}
}

// TestMemberStatusHasOnlyTwoValues：人的状态只有 running / free 两值（看板回答"此刻在不在干活"）。
func TestMemberStatusHasOnlyTwoValues(t *testing.T) {
	plan := boardPlanFixture()
	if got := MemberStatus(plan, store.TeamworkMember{Role: "exec"}); got != MemberRunning {
		t.Fatalf("名下有在跑的工作项 ⇒ running，得到 %q", got)
	}
	if got := MemberStatus(plan, store.TeamworkMember{Role: "pm"}); got != MemberFree {
		t.Fatalf("只有等验收的工作项 ⇒ free（没有 review 这个值），得到 %q", got)
	}
	if got := MemberStatus(plan, store.TeamworkMember{Role: "ghost"}); got != MemberFree {
		t.Fatalf("没派过活 ⇒ free，得到 %q", got)
	}
}

// TestMemberStatusSkipsDoneItemsInQueue：队列是"还没完成的工作项名称"（销项即出队）。
func TestMemberStatusSkipsDoneItemsInQueue(t *testing.T) {
	views := MemberViews(boardPlanFixture(), nil, nil, nil)
	if len(views) != 2 {
		t.Fatalf("成员行数 = %d", len(views))
	}
	if len(views[0].Queue) != 1 || views[0].Queue[0] != "写解析" {
		t.Fatalf("已销项不占队列：%+v", views[0].Queue)
	}
}

// TestMemberCurrentPrefersRunningThenReviewThenLast："这个人此刻那件事的会话"三段优先，
// 且**不用** RoleSessionID 顶替（那是员工的长期会话，不是"一 Work Item 一个 Session"的会话号）。
func TestMemberCurrentPrefersRunningThenReviewThenLast(t *testing.T) {
	plan := boardPlanFixture()
	if sessionID, item := MemberCurrent(plan, "exec"); sessionID != "sess-wi-1" || item != "wi-1" {
		t.Fatalf("在跑优先：%q %q", sessionID, item)
	}
	if sessionID, item := MemberCurrent(plan, "pm"); sessionID != "sess-wi-3" || item != "wi-3" {
		t.Fatalf("其次等验收：%q %q", sessionID, item)
	}
	// 只剩终态时退到"最近开过工的那件事"，而不是回到 RoleSessionID。
	done := boardPlanFixture()
	done.Milestones[0].Items[0].Status = store.TeamworkItemDone
	done.Milestones[0].Items[2].Role = "exec"
	if sessionID, _ := MemberCurrent(done, "exec"); sessionID != "sess-wi-3" {
		t.Fatalf("兜底取最后一个带会话号的工作项：%q", sessionID)
	}
	if sessionID, item := MemberCurrent(plan, "ghost"); sessionID != "" || item != "" {
		t.Fatalf("没这个角色 ⇒ 空：%q %q", sessionID, item)
	}
}

// TestJobViewsReverseSubjectBackToRole：作业行的角色由作用域主体反解（emp_<role>）；
// 不是这个前缀就原样返回（不猜）。
func TestJobViewsReverseSubjectBackToRole(t *testing.T) {
	views := JobViews([]jobs.Record{
		{Handle: "h1", Scope: jobs.Scope{Subject: SubjectForRole("exec")}},
		{Handle: "h2", Scope: jobs.Scope{Subject: "someone-else"}},
		{Handle: "h3"},
	})
	if views[0].Role != "exec" || views[0].Scope.Subject != "emp_exec" {
		t.Fatalf("反解 emp_<role>：%+v", views[0])
	}
	if views[1].Role != "someone-else" {
		t.Fatalf("非本前缀原样返回：%+v", views[1])
	}
	if views[2].Role != "" {
		t.Fatalf("没有主体 ⇒ 空角色：%+v", views[2])
	}
	if JobViews(nil) != nil {
		t.Fatal("没有作业行 ⇒ nil（前端不画空表）")
	}
}

// TestEventViewsKeepsOnlyRecent 钉住审计投影的有界性：只取最近 BoardEventLimit 条，
// 且保持文件顺序（发生顺序），不重排。
func TestEventViewsKeepsOnlyRecent(t *testing.T) {
	events := make([]store.TeamworkEvent, 0, BoardEventLimit+5)
	for index := 0; index < BoardEventLimit+5; index++ {
		events = append(events, store.TeamworkEvent{At: time.Unix(int64(index), 0), Kind: "settle"})
	}
	views := EventViews(events)
	if len(views) != BoardEventLimit {
		t.Fatalf("条数 = %d, want %d", len(views), BoardEventLimit)
	}
	if views[0].At != 5 || views[len(views)-1].At != int64(BoardEventLimit+4) {
		t.Fatalf("必须取**最近**若干条且保序：first=%d last=%d", views[0].At, views[len(views)-1].At)
	}
	if EventViews(nil) != nil {
		t.Fatal("没有审计流水 ⇒ nil")
	}
}

// TestProjectionStaleIsOnlyAHint：句柄过期提示位——计划里记着句柄、本进程作业表一个都查不到
// ⇒ true；计划里没记句柄、或至少有一个还活着 ⇒ false。它只是提示位，不阻止渲染。
func TestProjectionStaleIsOnlyAHint(t *testing.T) {
	state := store.TeamworkState{Jobs: map[string]string{"wi-1": "h1", "wi-2": "h2"}}
	if ProjectionStale(state, nil) != true {
		t.Fatal("全部查不到 ⇒ 提示过期")
	}
	if ProjectionStale(state, []dto.TeamworkJobView{{Handle: "h2"}}) != false {
		t.Fatal("至少一个还活着 ⇒ 不过期")
	}
	if ProjectionStale(store.TeamworkState{}, nil) != false {
		t.Fatal("计划里没记句柄 ⇒ 不过期")
	}
}

// TestPlanHasOrchestrationRequiresMilestones：可看的编排 = 有里程碑（阶段口径已退场）。
func TestPlanHasOrchestrationRequiresMilestones(t *testing.T) {
	if !PlanHasOrchestration(boardPlanFixture()) {
		t.Fatal("有里程碑 ⇒ 有编排")
	}
	if PlanHasOrchestration(store.TeamworkPlan{TeamID: "t"}) {
		t.Fatal("没有里程碑 ⇒ 没有编排")
	}
}
