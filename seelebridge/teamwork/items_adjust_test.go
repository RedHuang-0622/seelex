package teamwork

// items_adjust_test.go — **未开始的工作可调整**（`AdjustItem`）的**正向路径**读数用例
// （审计 §6 #8 的空白：既有只有负向 `TestAdjustItemRefusesStartedAndFinishedWork`）。
//
// 口径（`items.go` 的 AdjustItem 契约注释）：
//   - 铁律只针对**已经发生的**事：running / review / done / failed 是既定事实，改它们
//     等于改历史（负向，见 items_test.go 那条）；**pending** 的一切都还能改；
//   - 可改的是五项：role / name / description / goal / depends_on；留空 = 不动；
//   - 改完必须经 `team_items`（= `Coordinator.Items`）读回与改动一致；
//   - 依赖是**硬闸门**：改成「另一件未完成的工作」之后，派发要被**显式拒收**（不是静默
//     排队、不是静默放行），依赖 done 之后才放行。
//
// 本文件只新增用例，不改既有断言（`items_test.go` 逐字未动）。

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// TestAdjustItemRewritesPendingWorkAndReadsBack 覆盖正向路径的前半：未开始的工作项
// 五项字段都能改，改后 `team_items` 读回与改动一致，没提到的字段一个都不动。
func TestAdjustItemRewritesPendingWorkAndReadsBack(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()

	// 前置：wi-test 起初是 test_case / 「用例跟进」，依赖 wi-impl。
	before := itemState(t, fixture, "wi-test")
	if before.Role != "test_case" || before.Name != "用例跟进" ||
		before.Description != "" || before.Goal != "" ||
		len(before.DependsOn) != 1 || before.DependsOn[0] != "wi-impl" {
		t.Fatalf("前置不符（排活口径变了？）：%+v", before)
	}

	// 未开始（pending）：换人 / 改名 / 加描述 / 改目标 / 换依赖，一次改齐。
	if err := fixture.coordinator.AdjustItem(ctx, "wi-test", WorkItemSpec{
		Role:        "exec",
		Name:        "用例跟进（改派）",
		Description: "补边界用例，含中断恢复那一条",
		Goal:        "覆盖率达标 + 边界有回归",
		DependsOn:   []string{"wi-req"},
	}); err != nil {
		t.Fatalf("未开始的工作项应当可调整: %v", err)
	}

	// team_items（= Coordinator.Items）读回必须与改动一致。
	after := itemState(t, fixture, "wi-test")
	if after.Role != "exec" {
		t.Fatalf("改派没生效：role = %q，want exec", after.Role)
	}
	if after.Name != "用例跟进（改派）" {
		t.Fatalf("改名没生效：name = %q", after.Name)
	}
	if after.Description != "补边界用例，含中断恢复那一条" {
		t.Fatalf("改描述没生效：description = %q", after.Description)
	}
	if after.Goal != "覆盖率达标 + 边界有回归" {
		t.Fatalf("改目标没生效：goal = %q", after.Goal)
	}
	if len(after.DependsOn) != 1 || after.DependsOn[0] != "wi-req" {
		t.Fatalf("改依赖没生效：depends_on = %v，want [wi-req]", after.DependsOn)
	}

	// 没提到的字段一个都不许动：id / 里程碑 / 状态 / 归属仍是原样。
	if after.ID != "wi-test" || after.Milestone != "m-build" {
		t.Fatalf("调整动了不该动的格（id/里程碑）：%+v", after)
	}
	if after.StatusOrPending() != sessionstore.TeamworkItemPending {
		t.Fatalf("调整不是派发：状态仍是 pending，得到 %q", after.StatusOrPending())
	}
	// 未派发的工作项没有自己的会话/现场（那是派发才发生的）。
	if after.SessionID != "" || after.Worktree != "" || after.Handle != "" {
		t.Fatalf("调整不得凭空造出会话/现场/句柄：%+v", after)
	}

	// 只改一项也要能改：把目标改回去，其余字段照旧（留空 = 不动）。
	if err := fixture.coordinator.AdjustItem(ctx, "wi-test", WorkItemSpec{Goal: "只改目标"}); err != nil {
		t.Fatalf("只改一项也应当可调整: %v", err)
	}
	single := itemState(t, fixture, "wi-test")
	if single.Goal != "只改目标" || single.Name != "用例跟进（改派）" || single.Role != "exec" ||
		len(single.DependsOn) != 1 || single.DependsOn[0] != "wi-req" {
		t.Fatalf("留空 = 不动：只改 goal，其余字段应当原封不动：%+v", single)
	}

	// 调整会给未在编的角色：显式拒收（角色是编排事实，不能改成计划外的人）。
	if err := fixture.coordinator.AdjustItem(ctx, "wi-test", WorkItemSpec{Role: "ghost"}); err == nil ||
		!strings.Contains(err.Error(), "不在编") {
		t.Fatalf("改成不在编的角色必须被拒，得到 %v", err)
	}
	// 不存在的 id：显式报错（不是静默 no-op）。
	if err := fixture.coordinator.AdjustItem(ctx, "wi-nope", WorkItemSpec{Name: "x"}); err == nil ||
		!strings.Contains(err.Error(), "没有工作项") {
		t.Fatalf("调整不存在的工作项必须报错，得到 %v", err)
	}
}

// TestAdjustItemDependencyIsAHardGate 覆盖正向路径的后半：把某未开始项的 depends_on
// 改成另一件**未完成**的工作之后，派发被显式拒收；依赖做完并验收通过才放行。
func TestAdjustItemDependencyIsAHardGate(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()

	// 起初 wi-test 依赖 wi-impl；把它的依赖改成另一件未完成的 wi-req。
	if err := fixture.coordinator.AdjustItem(ctx, "wi-test", WorkItemSpec{DependsOn: []string{"wi-req"}}); err != nil {
		t.Fatalf("AdjustItem: %v", err)
	}

	// 新依赖没完成：派发必须**显式拒收**，且正文说清卡在哪一件。
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-test"); err == nil {
		t.Fatal("依赖还没完成就派发，必须被拒（硬闸门，不是静默排队）")
	} else if !strings.Contains(err.Error(), "还没完成") || !strings.Contains(err.Error(), "wi-req") {
		t.Fatalf("拒收正文必须说清是哪件依赖没完成，得到 %v", err)
	}
	// 被拒之后不得留下任何派发痕迹（没有会话/现场/绑定）。
	blocked := itemState(t, fixture, "wi-test")
	if blocked.StatusOrPending() != sessionstore.TeamworkItemPending || blocked.SessionID != "" ||
		blocked.Worktree != "" || blocked.StartedAt != 0 {
		t.Fatalf("派发被拒不得改动工作项：%+v", blocked)
	}

	// 依赖做完并验收通过 → 放行。
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-req"); err != nil {
		t.Fatalf("DispatchItem(wi-req): %v", err)
	}
	waitItemStatus(t, fixture, "wi-req", sessionstore.TeamworkItemReview)
	if err := fixture.coordinator.AcceptItem(ctx, "wi-req", "通过"); err != nil {
		t.Fatalf("AcceptItem: %v", err)
	}
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-test"); err != nil {
		t.Fatalf("依赖完成后必须放行：%v", err)
	}
	dispatched := itemState(t, fixture, "wi-test")
	if dispatched.StatusOrPending() != sessionstore.TeamworkItemRunning {
		t.Fatalf("放行后工作项应在跑，得到 %q", dispatched.StatusOrPending())
	}
	if dispatched.SessionID == "" || dispatched.Worktree == "" {
		t.Fatalf("派发必须落上「这件事自己的会话 + 现场」：%+v", dispatched)
	}
}

// TestAdjustItemStillRefusesRunningReviewAndDone 是负向边界**不回归**：已 running /
// review / done 一律被拒（调整不该在正向用例里被放宽）。
//
// 与 items_test.go:593 那条同名判据互补：那条覆盖 running + done，这里把**待验收（review）**
// 这一格也钉上（待验收是"结论已经有了、leader 还没给结论"，同样属于既定事实）。
func TestAdjustItemStillRefusesRunningReviewAndDone(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()

	// 已开始（running）：派发返回时状态就是 running。
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-req"); err != nil {
		t.Fatalf("DispatchItem: %v", err)
	}
	if err := fixture.coordinator.AdjustItem(ctx, "wi-req", WorkItemSpec{Name: "偷改"}); err == nil ||
		!strings.Contains(err.Error(), "既定的") {
		t.Fatalf("已经开始的工作项必须拒绝调整，得到 %v", err)
	}

	// 待验收（review）：teammate 跑完了，但结论还没落 —— 同样是既定事实。
	waitItemStatus(t, fixture, "wi-req", sessionstore.TeamworkItemReview)
	if err := fixture.coordinator.AdjustItem(ctx, "wi-req", WorkItemSpec{Goal: "改目标"}); err == nil ||
		!strings.Contains(err.Error(), "既定的") {
		t.Fatalf("待验收的工作项必须拒绝调整，得到 %v", err)
	}

	// 已结束（done）：改它等于改历史。
	if err := fixture.coordinator.AcceptItem(ctx, "wi-req", "通过"); err != nil {
		t.Fatalf("AcceptItem: %v", err)
	}
	if err := fixture.coordinator.AdjustItem(ctx, "wi-req", WorkItemSpec{Name: "改历史"}); err == nil ||
		!strings.Contains(err.Error(), "既定的") {
		t.Fatalf("已经完成的工作项必须拒绝调整，得到 %v", err)
	}
}
