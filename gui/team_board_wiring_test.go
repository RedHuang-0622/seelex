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
}
