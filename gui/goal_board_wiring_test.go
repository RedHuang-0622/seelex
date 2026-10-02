package gui

import (
	"strings"
	"testing"
)

// TestEmbeddedGoalBoardWiring：「目标」子页的面板必须在嵌入前端里真的接起来。
//
// 用户口径（2026-10-03）：看板出来到工作台的「目标」子页——上面是大的 active seq，
// 下面是我发出的最近一次任务（小字），点开出一份内容详情（资源管理器「内容详情」
// 口径）；目标结束（栈上没有 active 帧）就没有看板。
// 用户口径（2026-10-02）：标签的生命周期也归 goal 状态机——目标结束后面板与它的
// 标签（GOAL 徽标 / goal 域 skill chips）一起退场，不靠 skill 激活态（`$goal`
// 一召回就长期为真，用它当判据就是"goal 已经结束了、标签还贴着"）。
//
// 三处缺一，用户看到的就分别是"没有看板"（模块没接）/ "点了没反应"（点击落点没接）/
// "弹窗是空的"（弹窗元素没进 DOM）。这里逐条钉住。
func TestEmbeddedGoalBoardWiring(t *testing.T) {
	t.Parallel()
	script, err := embeddedFrontend.ReadFile("frontend/dist/app.js")
	if err != nil {
		t.Fatalf("embedded frontend app.js: %v", err)
	}
	index, err := embeddedFrontend.ReadFile("frontend/dist/index.html")
	if err != nil {
		t.Fatalf("embedded frontend index.html: %v", err)
	}
	board, err := embeddedFrontend.ReadFile("frontend/dist/goal-board-view.js")
	if err != nil {
		t.Fatalf("embedded frontend goal-board-view.js: %v", err)
	}
	app := string(script)
	boardSource := string(board)

	if !strings.Contains(app, `from "./goal-board-view.js"`) ||
		!strings.Contains(app, "renderGoalPanel(") ||
		!strings.Contains(app, "renderGoalDetail(") {
		t.Fatal("面板/看板/详情必须由 ./goal-board-view.js 的纯渲染件承担（app.js 只做接线）")
	}
	if !strings.Contains(app, `[data-goal-board-open]`) || !strings.Contains(app, "openGoalDetail()") {
		t.Fatal("看板卡片的点击落点必须接上详情弹窗（否则点了没反应）")
	}
	for _, id := range []string{"goal-detail-modal", "goal-detail-close", "goal-detail-title", "goal-detail-view"} {
		if !strings.Contains(string(index), `id="`+id+`"`) {
			t.Fatalf("缺少目标详情弹窗元素 %s", id)
		}
	}
	// 看板的两行与详情面：大 active seq / 小字最近输入 / 完整打点流水。
	// 「序号徽标」2026-10-02 抽成组件库件（components.js renderSeqBadge）：goal 看板与
	// 右栏压缩条目共用同一件，所以这里同时钉"看板复用它"与"徽标本体在组件库里"——
	// 只钉 boardSource 里的类名会让"两处各写一份"重新长回来。
	components, err := embeddedFrontend.ReadFile("frontend/dist/components.js")
	if err != nil {
		t.Fatalf("embedded frontend components.js: %v", err)
	}
	for _, want := range []string{"active seq", "renderSeqBadge(", "goal-board-task", "goal-detail-marks", "progress_all"} {
		if !strings.Contains(boardSource, want) {
			t.Fatalf("看板渲染件缺少 %q", want)
		}
	}
	for _, want := range []string{"export function renderSeqBadge", "seq-badge-num", "seq-badge-unit"} {
		if !strings.Contains(string(components), want) {
			t.Fatalf("序号徽标组件（components.js）缺少 %q", want)
		}
	}
	// 席位轮转的三个概念（轮次/座次/断环）不得从门外再爬回来：面板既不渲染它们，
	// 也没有任何写 goal 状态的入口（前端只读，真值在后端）。
	for _, retired := range []string{"current_seat", "round_error", "break_reason", "goal-gov-broken", "goal-gov-error"} {
		if strings.Contains(app, retired) {
			t.Fatalf("席位轮转已退场，app.js 不得再引用 %q", retired)
		}
	}
	// 标签的生命周期归 goal 状态机（2026-10-02 现场）：面板不得拿 skill 激活态当
	// 标签判据——`$goal`/`$teamwork` 一召回就长期为真，目标收口归档后 GOAL 徽标与
	// chips 会一直贴着。判据只能是治理视图里"栈上还有 active 帧"。
	if strings.Contains(app, "goal_skill_active") {
		t.Fatal("「目标」面板的标签必须由 goal 状态机（治理视图的 active 帧）驱动，不得由 skill 激活态驱动")
	}
}
