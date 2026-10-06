package seelebridge

// runtime_teamwork_work_item_progress_test.go — 钉住「teammate 某件工作的**进度详情**」
// 这条读面（审计 §6 #7 的空白：teammate「每 Session 进度详情」的**非真机档**读数用例）。
//
// 勘定（本轮，只读）：teammate 侧有**两条**读面，各自只带一半——
//
//	(a) 会话详情面   Service.TeammateSessionLiveFor(sessionID) → dto.TeammateSessionLiveView
//	    带 session_id / role / live / running / messages / truncated；
//	    **不带 worktree、不带状态词/阶段**（application/contract/dto/teammate_session_live.go:21）。
//	    它是"这件事此刻在说什么"，与子代理详情（SubagentDetail 一份载荷就带 worktree）不同口径。
//
//	(b) 看板投影面   Service.TeamworkBoardViewFor(sessionID) → dto.TeamworkBoardView.WorkItems[]
//	    每个工作项带 SessionID / Worktree / Status / Milestone / Handle / Live / Interrupted
//	    （application/contract/dto/teamwork_board.go:167；搬运点 seelebridge/runtime_teamwork_board.go:354
//	    teamworkWorkItemViews）。**session id + worktree + 状态词 + 所属里程碑（阶段）这一组
//	    ("这件事跑到哪") 只在 (b) 读得到。**
//
// 因此本用例钉 (b)：一条**不依赖真机额度**的读数，证明 teammate 某件工作（按它自己的会话号
// 定位）的进度详情确实能从投影读出。既有用例（runtime_teamwork_board_test.go:TestTeamworkBoardCarriesWorkItemsAndTeammateQueue
// 与 TestTeamworkBoardCarriesMemberCurrentSession）搬了 status / current session，但**没有**
// 一条把「session id + worktree + 状态词 + 阶段」当**同一件工作的进度详情**一起钉住。
//
// 而「让**一条**读面同时给出会话正文 + worktree + 阶段」（子代理详情那种单载荷口径）需要动产品
// （给 TeammateSessionLiveView 加 worktree/状态词，或给看板行加 stage/preview）——**本轮不改产品**，
// 该半仍开放，见交付说明。

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// workItemProgressFixture 造一份"某件工作正在跑"的计划：工作项自带它自己的会话号与现场。
func workItemProgressFixture() sessionstore.TeamworkPlan {
	return sessionstore.TeamworkPlan{
		TeamID:  "t-progress",
		Version: 1,
		Members: []sessionstore.TeamworkMember{
			{Role: "exec", RoleSessionID: "s-team-exec", ToolsPolicy: "readwrite"},
		},
		Milestones: []sessionstore.TeamworkMilestone{
			{
				ID: "m-impl", Name: "实现", Content: "把渲染件写完",
				Items: []sessionstore.TeamworkWorkItem{
					{
						ID: "wi-impl", Milestone: "m-impl", Role: "exec", Name: "实现 UI",
						Status: sessionstore.TeamworkItemRunning,
						// 这两格就是「一 Work Item 一个 Session + 一个 worktree」的事实。
						SessionID: "s-team-exec-wi-wi-impl",
						Worktree:  "seelex/exec-wi-impl",
						Handle:    "a1",
					},
				},
			},
		},
	}
}

// TestTeamworkBoardReadsWorkItemProgressDetail 钉住：teammate 某件工作的进度详情
// （session id + worktree + 状态词 + 阶段/里程碑）能从看板投影读出，且能**按它自己的
// 会话号定位**——这正是"每 Session 进度详情"的读面口径。
func TestTeamworkBoardReadsWorkItemProgressDetail(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	store := &memPlanStore{}
	backend := teamworkTestBackend(store, "s-team")
	if err := r.SetTeamworkBackend(backend); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	ctx := context.Background()
	key := sessionstore.Key{ProjectID: "p-team", SessionID: "s-team"}
	if err := store.WritePlan(ctx, key, workItemProgressFixture(), backend.MaxTeammates); err != nil {
		t.Fatalf("WritePlan: %v", err)
	}
	// 绑定账本一行：这件事有一份**未释放**的现场（Live = 读出来的事实）。
	if err := store.AppendBinding(ctx, key, sessionstore.TeamworkBinding{
		WorkItem: "wi-impl", Milestone: "m-impl", Role: "exec",
		SessionID: "s-team-exec-wi-wi-impl", Worktree: "seelex/exec-wi-impl",
	}); err != nil {
		t.Fatalf("AppendBinding: %v", err)
	}

	board := r.TeamworkBoardSnapshot("s-team")
	if board == nil {
		t.Fatal("有计划就必须给出看板投影")
	}

	// 「每 Session」的定位口径：用**这件事自己的会话号**在投影里找到那件工作。
	// 这条会话号不是角色会话号（s-team-exec）——一 Work Item 一套 Session。
	detail, found := boardWorkItemBySession(board, "s-team-exec-wi-wi-impl")
	if !found {
		t.Fatalf("按会话号定位不到那件工作（每 Session 进度详情读不出来）：%+v", board.WorkItems)
	}

	// 四件一起钉：会话号 + 现场 + 状态词 + 阶段（所属里程碑）。
	if detail.SessionID != "s-team-exec-wi-wi-impl" {
		t.Fatalf("进度详情必须带**这件事自己的会话号**：%q", detail.SessionID)
	}
	if detail.Worktree != "seelex/exec-wi-impl" {
		t.Fatalf("进度详情必须带现场（worktree）：%q", detail.Worktree)
	}
	if detail.Status != sessionstore.TeamworkItemRunning {
		t.Fatalf("进度详情必须带状态词：%q，want %q", detail.Status, sessionstore.TeamworkItemRunning)
	}
	if detail.Milestone != "m-impl" {
		t.Fatalf("进度详情必须带阶段（所属里程碑）：%q", detail.Milestone)
	}
	// 阶段名要能从同一份载荷解析出来（只看 id 的"阶段"读不出人话）。
	if name := boardMilestoneName(board, detail.Milestone); name != "实现" {
		t.Fatalf("阶段 id 必须在同一份投影里能解析成名字，得到 %q（里程碑表：%+v）", name, board.Milestones)
	}
	// 归属（谁在干）也要在：这件工作的执行者是 exec。
	if detail.Role != "exec" {
		t.Fatalf("进度详情必须带执行者：%q", detail.Role)
	}
	// 两条"读出来的事实"：现场在册（Live）+ 句柄不在本进程（Interrupted，可重派信号）。
	if !detail.Live {
		t.Fatal("有一份未释放的绑定 → Live 必须为真（现场在册是读出来的事实）")
	}
	if !detail.Interrupted {
		t.Fatal("状态说在跑、本进程作业表里查不到该句柄 → Interrupted 必须为真（jobs I-4 的显式化）")
	}

	// 同一件事在自己的会话号下读得到——换一个**角色会话号**不该命中（那是长期会话，不是这件事的）。
	if _, wrong := boardWorkItemBySession(board, "s-team-exec"); wrong {
		t.Fatal("角色会话号（长期会话）不该被当成某件工作的会话号")
	}
}

// boardWorkItemBySession 在投影里按**会话号**找那件工作（"每 Session 进度详情"的定位口径）。
func boardWorkItemBySession(board *dto.TeamworkBoardView, sessionID string) (dto.TeamworkWorkItemView, bool) {
	for _, item := range board.WorkItems {
		if item.SessionID == sessionID {
			return item, true
		}
	}
	return dto.TeamworkWorkItemView{}, false
}

// boardMilestoneName 把里程碑 id 解析成名字（空串 = 投影里没有这个里程碑）。
func boardMilestoneName(board *dto.TeamworkBoardView, milestoneID string) string {
	for _, milestone := range board.Milestones {
		if milestone.ID == milestoneID {
			return milestone.Name
		}
	}
	return ""
}
