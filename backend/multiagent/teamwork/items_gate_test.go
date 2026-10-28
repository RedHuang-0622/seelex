package teamwork

import (
	"context"
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
	"strings"
	"testing"
)

// 卡点（D7）与收口拒绝文案（D8）的判据：**一处判断，两处渲染**。
//
// 缺陷本体（2026-10-15 审计）：leader 只能靠状态/依赖/绑定自己反推"卡在哪"（真实会话里同一批
// 取回工具被轮询几十次）；而 team_close 的拒绝文案把几种情形一起列出来，却不说当前是哪一种。

// TestItemGateNamesWhereEveryItemIsStuck 把每一种卡点逐个钉住：判据是纯函数，词表就是它的输出。
func TestItemGateNamesWhereEveryItemIsStuck(t *testing.T) {
	plan := storeteamwork.Plan{
		TeamID:  "v-model",
		Version: 1,
		Milestones: []storeteamwork.Milestone{{
			ID: "m-build",
			Items: []storeteamwork.WorkItem{
				{ID: "wi-done", Status: storeteamwork.ItemDone},
				{ID: "wi-running", Status: storeteamwork.ItemRunning, Handle: "bteam-1"},
				{ID: "wi-interrupted", Status: storeteamwork.ItemRunning, Handle: "bteam-2"},
				{ID: "wi-review", Status: storeteamwork.ItemReview},
				{ID: "wi-merge", Status: storeteamwork.ItemReview},
				{ID: "wi-failed", Status: storeteamwork.ItemFailed},
				{ID: "wi-blocked", DependsOn: []string{"wi-running"}},
				{ID: "wi-ready", DependsOn: []string{"wi-done"}},
			},
		}},
	}
	plan.State.Unmerged = map[string]string{"wi-merge": "主工作区挡路"}
	live := map[string]JobReading{
		"bteam-1": {Alive: true, Running: true},
		"bteam-2": {}, // 句柄不在册（重启 / 已销项 / 被驱逐）
	}

	cases := []struct {
		item string
		want ItemGate
	}{
		{"wi-done", ItemGateDone},
		{"wi-running", ItemGateJobRunning},
		{"wi-interrupted", ItemGateJobInterrupted},
		{"wi-review", ItemGateReview},
		{"wi-merge", ItemGateMerge},
		{"wi-failed", ItemGateFailed},
		{"wi-blocked", ItemGateBlocked},
		{"wi-ready", ItemGateReady},
	}
	for _, testCase := range cases {
		_, item, ok := findItem(&plan, testCase.item)
		if !ok {
			t.Fatalf("用例的夹具缺工作项 %q", testCase.item)
		}
		got := ItemGateOf(plan, *item, live[item.Handle])
		if got != testCase.want {
			t.Errorf("%s 的卡点 = %q，want %q", testCase.item, got, testCase.want)
		}
		if got.Next() == "" {
			t.Errorf("卡点 %q 没有下一步动作：说得出卡在哪就必须说得出怎么做（审计 D8）", got)
		}
	}
}

// TestOnlySandboxHoldingGatesCountAsUnsettled 钉住"没落定"这一格的边界：只有收口会拆掉现场的那几类
// 卡点算没落定；review（尾插已走完、合并已落地）与句柄已作废的 running 都不算。
func TestOnlySandboxHoldingGatesCountAsUnsettled(t *testing.T) {
	unsettled := []ItemGate{ItemGateJobRunning, ItemGateMerge, ItemGateFailed}
	for _, gate := range unsettled {
		if !unsettledBySandboxGate(gate) {
			t.Errorf("卡点 %q 应当算没落定", gate)
		}
	}
	settled := []ItemGate{
		ItemGateDone, ItemGateJobInterrupted, ItemGateReview, ItemGateBlocked, ItemGateReady,
	}
	for _, gate := range settled {
		if unsettledBySandboxGate(gate) {
			t.Errorf("卡点 %q 不该算没落定", gate)
		}
	}
}

// TestItemsCarryGateAndNext：team_items 的每一项都带着"卡在哪 + 下一步该做什么"两格。
func TestItemsCarryGateAndNext(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()

	block := make(chan struct{})
	fixture.runner.block = block
	t.Cleanup(func() { close(block) })
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-req"); err != nil {
		t.Fatalf("DispatchItem: %v", err)
	}
	waitRequests(t, fixture, 1)

	views, err := fixture.coordinator.Items(ctx)
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	byID := map[string]ItemView{}
	for _, view := range views {
		byID[view.Item.ID] = view
	}
	if got := byID["wi-req"]; got.Gate != ItemGateJobRunning || !strings.Contains(got.Next, "jobs_manage(op=kill") {
		t.Fatalf("在跑的工作项卡点 = %q / 下一步 = %q，want job:running + 可照做的停止动作", got.Gate, got.Next)
	}
	if got := byID["wi-impl"]; got.Gate != ItemGateBlocked {
		t.Fatalf("依赖未 done 的工作项卡点 = %q，want %q", got.Gate, ItemGateBlocked)
	}
	if got := byID["wi-test"]; got.Gate != ItemGateBlocked {
		t.Fatalf("下游工作项同样卡在依赖上：卡点 = %q，want %q", got.Gate, ItemGateBlocked)
	}
}

// TestTeamCloseRefusalNamesTheCurrentGate：收口被拒时，文案要**逐件**说清卡点与下一步，
// 而不是把几种情形一起列出来让 leader 自己翻译。
func TestTeamCloseRefusalNamesTheCurrentGate(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()
	block := blockWorker(fixture)
	t.Cleanup(func() { close(block) })
	runningItem(t, fixture, "wi-req")

	if _, err := fixture.coordinator.Close(ctx); err == nil {
		t.Fatal("还有在跑的作业时收口必须被拒")
	} else {
		message := err.Error()
		for _, want := range []string{"job:running", "jobs_manage(op=kill"} {
			if !strings.Contains(message, want) {
				t.Fatalf("收口拒绝文案缺 %q（必须说清当前卡点与下一步）：%v", want, err)
			}
		}
		if !strings.Contains(message, "wi-req") {
			t.Fatalf("收口拒绝文案没有点名是哪一件：%v", err)
		}
	}
}
