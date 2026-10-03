package core

// teamwork_board_session_switch_test.go — 钉住「切换会话 / 重启后打开会话」时**会话运行
// 原件要重建**（2026-10-04 现场：切走再切回来，团队看板整块消失；数据都在磁盘上，
// 是"重建"路径断了）。
//
// 根因：会话运行原件（Snapshot.Runtime，含 teamwork_board）只在**回合尾**（chat.go）与
// **工具边界**（tool_hooks.go）被写进会话槽；而切换会话会换掉槽——冷加载先
// `sessions.Remove(sessionID)`（UnloadSession）再重建单元，新单元的 runtime 槽是空的。
// 于是"切回来第一帧"里 `teamwork_board` 缺键，前端按"没有计划即整块退场"隐藏看板，
// **要等用户再发一句话**才回来。
//
// 为什么在这一层钉：桥侧（seelebridge）的投影组装、模型层的深拷贝、前端渲染件各自都有
// 用例，但如果会话（重）激活不采这一次，"三跳都通、就是没人调"——现象与后端毫无关系的
// 一面空白。

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// sessionSwitchBoardRuntime 是**按会话**回答团队看板的运行时桩：上一个会话说"有"、
// 另一个会话说"没有"——切换语义只有这样才能被验出来（一个恒定返回同一份看板的桩，
// 切换前后看不出差别）。
type sessionSwitchBoardRuntime struct {
	*fakeRuntime
	boards map[string]*dto.TeamworkBoardView
}

func (r *sessionSwitchBoardRuntime) TeamworkBoardSnapshot(sessionID string) *dto.TeamworkBoardView {
	return r.boards[sessionID]
}

func TestSessionReactivationRebuildsTeamBoardProjection(t *testing.T) {
	board := teamBoardFixture()
	runtime := &sessionSwitchBoardRuntime{fakeRuntime: &fakeRuntime{}, boards: map[string]*dto.TeamworkBoardView{}}
	engine := &fakeEngine{}
	service := newTestService(t, engine, withTestRuntime(runtime))

	sessionA := service.Snapshot().Session.ID
	runtime.boards[sessionA] = board
	// 让 A 走**热挂载**（引擎实例在内存）：本用例要钉的就是"换视图指针那一步也要重建槽"。
	if engine.loadedSessions == nil {
		engine.loadedSessions = map[string]bool{}
	}
	engine.loadedSessions[sessionA] = true

	if err := service.BeginNewSession(); err != nil {
		t.Fatalf("BeginNewSession: %v", err)
	}
	sessionB := service.Snapshot().Session.ID
	if sessionB == sessionA {
		t.Fatal("新建会话必须换一个会话号（否则本用例没在测切换）")
	}
	if got := service.Snapshot().Runtime.TeamworkBoard; got != nil {
		t.Fatalf("新会话没有团队计划时槽里不该有看板：%+v", got)
	}

	// 切回 A：**第一帧**快照就必须带看板（不能等下一轮回合尾）。
	if err := service.ActivateSession(sessionA); err != nil {
		t.Fatalf("ActivateSession: %v", err)
	}
	got := service.Snapshot().Runtime.TeamworkBoard
	if got == nil {
		t.Fatal("切回会话时没有重建会话运行原件（团队看板缺失）：数据没丢，是重建路径断了")
	}
	if got.TeamID != board.TeamID || len(got.Milestones) != len(board.Milestones) || len(got.WorkItems) != len(board.WorkItems) {
		t.Fatalf("重建出来的看板与原投影不一致：%+v", got)
	}

	// 切到 B 再切回 A：B 的槽里不该留着 A 的看板（过渡帧不许说谎）。
	if err := service.ActivateSession(sessionB); err != nil {
		t.Fatalf("ActivateSession(B): %v", err)
	}
	if got := service.Snapshot().Runtime.TeamworkBoard; got != nil {
		t.Fatalf("切到没有计划的会话后不得留着上一个会话的看板：%+v", got)
	}
	if err := service.ActivateSession(sessionA); err != nil {
		t.Fatalf("ActivateSession(A): %v", err)
	}
	if service.Snapshot().Runtime.TeamworkBoard == nil {
		t.Fatal("再切回来仍必须带看板（重建是每一步切换都要做的，不是一次性的）")
	}
}
