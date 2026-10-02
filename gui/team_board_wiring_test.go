package gui

import (
	"strings"
	"testing"
)

// TestEmbeddedTeamBoardWiring：团队看板必须在嵌入前端里真的接起来（契约
// docs/arch/team-board-gui-tui-contract.md §5/§8 第 7 条）。
//
// 为什么要有这条守卫：「团队看板」渲染件（frontend/dist/team-board-view.js）此前一直存在，
// 但只有静态预览页与它自己的单测在消费——活体 GUI 里没有 import、没有落点、快照里也没有
// 那份投影。缺任何一处，用户看到的就是这三种之一：
//   - 模块没接          → 面板永远不出现；
//   - index.html 没有落点 → app.js 拿到 null 元素，静默返回；
//   - runtime 键没登记   → 快照分型把它当未知键（甚至被当成进程字段）。
//
// 顺带反向钉住用户裁决（2026-10-02）：**评审过程**不再挂在「goal 是否存活」下面——
// goal 面板里不得再出现 round_steps。
func TestEmbeddedTeamBoardWiring(t *testing.T) {
	t.Parallel()
	read := func(path string) string {
		t.Helper()
		content, err := embeddedFrontend.ReadFile(path)
		if err != nil {
			t.Fatalf("embedded frontend %s: %v", path, err)
		}
		return string(content)
	}
	app := read("frontend/dist/app.js")
	index := read("frontend/dist/index.html")
	shape := read("frontend/dist/snapshot-shape.js")
	boardView := read("frontend/dist/team-board-view.js")
	goalBoardView := read("frontend/dist/goal-board-view.js")

	// ① 渲染件被真的 import，并被真的调用（不是抄一份渲染逻辑进 app.js）。
	if !strings.Contains(app, `from "./team-board-view.js"`) {
		t.Fatal("app.js 必须从 ./team-board-view.js 引入团队看板渲染件（app.js 只做接线）")
	}
	if !strings.Contains(app, "renderTeamBoard(") {
		t.Fatal("app.js 必须调用 renderTeamBoard（引入但不调用 = 面板永远不出现）")
	}
	if !strings.Contains(boardView, "export function renderTeamBoard(") {
		t.Fatal("渲染件必须导出 renderTeamBoard")
	}
	// 同一份样式：来源是渲染件导出的 TEAM_BOARD_CSS，不在 styles.css 里抄第二份。
	if !strings.Contains(app, "TEAM_BOARD_CSS") || !strings.Contains(app, "ensureTeamBoardStyles(") {
		t.Fatal("看板样式必须由 app.js 注入渲染件导出的 TEAM_BOARD_CSS（唯一来源）")
	}
	appStyles := read("frontend/dist/styles.css")
	if strings.Contains(appStyles, ".team-stage {") || strings.Contains(appStyles, ".team-board-head") {
		t.Fatal("styles.css 不得再抄一份团队看板样式（两份色值 = 改一处漏一处）")
	}

	// ② 快照键登记：会话运行原件必须认领 teamwork_board，并真的被消费。
	if !strings.Contains(shape, `"teamwork_board"`) {
		t.Fatal("snapshot-shape.js 的 SESSION_RUNTIME_KEYS 必须登记 teamwork_board")
	}
	if !strings.Contains(app, "teamwork_board") {
		t.Fatal("app.js 必须从快照里取 runtime.teamwork_board（登记了不消费 = 面板永远是空的）")
	}

	// ③ index.html 落点：section / badge / view 三件都在，且 id 不与「状态」页的
	//    Agent Team 块（team-section / team-view / team-count）撞名。
	for _, id := range []string{"team-board-section", "team-board-badge", "team-board-view"} {
		if !strings.Contains(index, `id="`+id+`"`) {
			t.Fatalf("index.html 缺少团队看板挂载点 %s", id)
		}
	}
	// ④ 退场语义：没有计划时不留空壳（section 默认 hidden，正文清空）。
	if !strings.Contains(index, `id="team-board-section" class="hidden"`) {
		t.Fatal("团队看板 section 必须默认 hidden（无计划时不留空壳）")
	}
	// ⑤ 接线落在整份渲染与 runtime.changed 两条路径上（否则改了团队计划面板不刷新）。
	if count := strings.Count(app, "renderTeam(snapshot)"); count < 2 {
		t.Fatalf("renderTeam 必须接在整份渲染与 runtime.changed 两条路径上（实际 %d 处）", count)
	}

	// ⑥ 反向：评审过程不再挂在「goal 是否存活」下面（用户裁决 2026-10-02）。
	if strings.Contains(app, "round_steps") {
		t.Fatal("评审过程已退场：app.js 不得再引用 round_steps")
	}
	if strings.Contains(goalBoardView, "renderGoalSteps") {
		t.Fatal("评审过程已退场：「目标」面板不得再渲染它")
	}

	// ⑦ 成员入口（S7）：看板「在编」行的角色名 = 打开这位员工工作上下文的入口。
	//    两处缺一即死：渲染件不写钩子 = 没有入口；app.js 不绑委托 = 点不动的入口。
	//    委托必须挂在 `#team-board-view` 上：Agent Team 面板的那条挂在 `#team-view`，
	//    是另一块 section，收不到看板子树里的事件。
	if !strings.Contains(boardView, "data-team-role-open") || !strings.Contains(boardView, "data-team-role-session") {
		t.Fatal("看板在编行必须带成员入口钩子（data-team-role-open / data-team-role-session）")
	}
	if !strings.Contains(boardView, "is-openable") {
		t.Fatal("成员入口必须与纯文本区分（is-openable，否则用户看不出这一行可以点）")
	}
	if !strings.Contains(app, "bindTeamBoardActions(") || !strings.Contains(app, `elements["team-board-view"]`) {
		t.Fatal("app.js 必须把成员入口的委托绑在 #team-board-view 上（不绑 = 渲染出来的按钮点不动）")
	}
	if !strings.Contains(app, "openRoleSessionDetail(openRole.dataset.teamRoleOpen") {
		t.Fatal("看板的成员入口必须复用到同一个 openRoleSessionDetail（不另造一个员工会话概念）")
	}
}

// TestEmbeddedBoardArchiveVisibilityWiring 是**存档可见面**的接线守卫（设计契约
// docs/arch/session-board-metadata-lifecycle.md §6 第 3 条 / §3.1 / §10 第 4 条）。
//
// 存档（metadata/board_goal.json / metadata/board_team.json）本身是存储侧的事，但
// "从存档兜底恢复出来的看板必须能被看出来"是**前端的事**：缺了这几行，用户看到的恢复
// 结果与活体投影一模一样——他会把上一轮的快照当成现在的事实（这是本设计最不能出的错）。
// 同理，收口账本必须在展示面与"当前目标"**分开**（§3.1 的硬要求），不许混进看板卡片。
func TestEmbeddedBoardArchiveVisibilityWiring(t *testing.T) {
	t.Parallel()
	read := func(path string) string {
		t.Helper()
		content, err := embeddedFrontend.ReadFile(path)
		if err != nil {
			t.Fatalf("embedded frontend %s: %v", path, err)
		}
		return string(content)
	}
	app := read("frontend/dist/app.js")
	teamView := read("frontend/dist/team-board-view.js")
	goalView := read("frontend/dist/goal-board-view.js")
	styles := read("frontend/dist/styles.css")

	// 团队看板：recovered 必须从快照一路走到渲染件（搬运 + 渲染两处都要在）。
	if !strings.Contains(app, "recovered: board.recovered === true") {
		t.Fatal("app.js 必须把 runtime.teamwork_board.recovered 搬给渲染件（登记了不消费 = 这个痕迹永远不显示）")
	}
	if !strings.Contains(teamView, "team-recovered") || !strings.Contains(teamView, "自快照恢复") {
		t.Fatal("团队看板渲染件必须显形 recovered（与 stale 同形的一句话）")
	}

	// goal 看板：recovered 标记 + 「历史目标」一节，且账本与看板主体分开渲染。
	if !strings.Contains(goalView, "goal-recovered") || !strings.Contains(goalView, "自快照恢复") {
		t.Fatal("「目标」看板必须显形 recovered（这一帧来自存档快照）")
	}
	if !strings.Contains(goalView, "export function renderGoalHistory(") {
		t.Fatal("「目标」面板必须把 history 单成一节渲染（§3.1：不得混进看板卡片）")
	}
	if !strings.Contains(goalView, "${renderGoalHistory(governance)}") {
		t.Fatal("renderGoalPanel 必须真的把「历史目标」一节放进面板（导出了不调 = 永远不显示）")
	}
	if !strings.Contains(styles, ".goal-history-list") || !strings.Contains(styles, ".goal-recovered") {
		t.Fatal("styles.css 缺少「历史目标」/ recovered 的样式（渲染出来却没有版式）")
	}
}
