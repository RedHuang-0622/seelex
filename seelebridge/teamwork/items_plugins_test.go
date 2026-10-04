package teamwork

// items_plugins_test.go — 派发三跳里的**前两跳补钉子**：装配声明必须进**作业载荷**。
//
// 背景（对抗复核）：`coordinator.go` 的 Dispatch 与 `items.go` 的 DispatchItem 各自
// 构造一份 WorkerRequest，两处都写了 `Plugins: member.Plugins`，而**两处都没有用例**——
// 于是"派发时带了、执行时丢了"这条本特性自定的失败模式在两个入口上都没被钉住（权限在
// 相同位置有对应测试，能力轴反而没有）。载荷是执行体唯一的输入（WorkerRequest 同时是
// jobs.Spec.Payload 与 RunWorker 的入参），所以这里断言的是**载荷本身**，不是回执。
//
// 判别力：把成员的声明换成另一个名字，载荷必须跟着变——若实现退化成"常量/空值"，
// 这条用例失败；而只断言"字段存在"是抓不住退化实现的。

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

func pluginsEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

// TestDispatchCarriesPluginsInWorkerPayload 钉住两个派发口的载荷构造：
//
//	coordinator.go 的 Dispatch（teammate 级老口径）
//	items.go 的 DispatchItem（Work Item 级口径）
//
// 两条路都必须把成员声明的插件清单放进 WorkerRequest，且是**计划里那一份**。
func TestDispatchCarriesPluginsInWorkerPayload(t *testing.T) {
	ctx := context.Background()
	fixture := newItemFixture(t, 6)
	plan := itemPlan()
	plan.Members[1].Plugins = []string{"docs"} // exec 这一位声明了装配
	plan.Members[2].Plugins = []string{"test"} // test_case 另一位声明**不同**的装配
	if err := fixture.coordinator.SetPlan(ctx, plan); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}

	// ① teammate 级派发：exec 的载荷必须带 ["docs"]。
	handle, err := fixture.coordinator.Dispatch(ctx, "exec", "跑一轮")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	waitTerminal(t, fixture.jobs, handle)

	// ② Work Item 级派发：同一个成员的同一个声明，走另一条载荷构造。
	if err := fixture.coordinator.PlanMilestone(ctx, "m-build", []WorkItemSpec{
		{ID: "wi-plugins", Role: "test_case", Name: "装配链路", Goal: "把声明带进载荷"},
	}); err != nil {
		t.Fatalf("PlanMilestone: %v", err)
	}
	itemHandle, err := fixture.coordinator.DispatchItem(ctx, "wi-plugins")
	if err != nil {
		t.Fatalf("DispatchItem: %v", err)
	}
	waitTerminal(t, fixture.jobs, itemHandle)

	requests := fixture.runner.requestsSnapshot()
	if len(requests) != 2 {
		t.Fatalf("两次派发应各送一份载荷，得 %d 份", len(requests))
	}
	byRole := map[string]WorkerRequest{}
	for _, request := range requests {
		byRole[request.Role] = request
	}
	if got := byRole["exec"].Plugins; !pluginsEqual(got, []string{"docs"}) {
		t.Fatalf("Dispatch 的载荷丢了装配声明：Plugins=%v，want [docs]（派发时带了、执行时丢了）", got)
	}
	if got := byRole["test_case"].Plugins; !pluginsEqual(got, []string{"test"}) {
		t.Fatalf("DispatchItem 的载荷丢了装配声明：Plugins=%v，want [test]（派发时带了、执行时丢了）", got)
	}
	// 两条路都不许把别人的装配串过来：这不是"只要有值就行"。
	if pluginsEqual(byRole["exec"].Plugins, byRole["test_case"].Plugins) {
		t.Fatalf("两个成员的载荷必须是各自的声明，得 %v / %v", byRole["exec"].Plugins, byRole["test_case"].Plugins)
	}
	// Work Item 口径的载荷还要带上归属（同一跳里一起丢的另一类字段）。
	if byRole["test_case"].WorkItemID != "wi-plugins" || byRole["test_case"].Milestone != "m-build" {
		t.Fatalf("工作项载荷的归属字段丢了：%#v", byRole["test_case"])
	}
}

// TestDispatchPayloadOmitsUndeclaredPlugins：没声明 = 缺省（空），不是"装配了空集"。
//
// 这一条与上一条互为阴性对照：载荷里的 Plugins 一律来自计划成员条目，而"没声明"的
// 形态是 nil/空——行为上落到"不覆盖"（继承宿主当前装配），不是"零个插件"。
func TestDispatchPayloadOmitsUndeclaredPlugins(t *testing.T) {
	ctx := context.Background()
	fixture := newItemFixture(t, 6)
	if err := fixture.coordinator.SetPlan(ctx, itemPlan()); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	handle, err := fixture.coordinator.Dispatch(ctx, "pm", "跑一轮")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	waitTerminal(t, fixture.jobs, handle)

	requests := fixture.runner.requestsSnapshot()
	if len(requests) != 1 || len(requests[0].Plugins) != 0 {
		t.Fatalf("没声明装配的成员，载荷里必须是空（不覆盖），得 %#v", requests)
	}
}

// TestDispatchItemCarriesPluginsAfterPlanRewrite 是一次**回归钉子**：SetPlan 会整份
// 替换成员条目，改名/改声明之后的派发必须用**新**声明（而不是旧计划里的那份）。
func TestDispatchItemCarriesPluginsAfterPlanRewrite(t *testing.T) {
	ctx := context.Background()
	fixture := newItemFixture(t, 6)
	plan := itemPlan()
	plan.Members[1].Plugins = []string{"old"}
	if err := fixture.coordinator.SetPlan(ctx, plan); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	rewritten := itemPlan()
	rewritten.Members[1].Plugins = []string{"new"}
	if err := fixture.coordinator.SetPlan(ctx, rewritten); err != nil {
		t.Fatalf("SetPlan(改写): %v", err)
	}
	handle, err := fixture.coordinator.Dispatch(ctx, "exec", "跑一轮")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	waitTerminal(t, fixture.jobs, handle)

	requests := fixture.runner.requestsSnapshot()
	if len(requests) != 1 || !pluginsEqual(requests[0].Plugins, []string{"new"}) {
		t.Fatalf("载荷必须取计划里的当前声明（new），得 %#v", requests)
	}
	// 顺带钉住"声明进了计划"这一层：读回来的成员条目就是派发用的那一份。
	stored, err := fixture.store.ReadPlan(ctx, sessionstore.Key{ProjectID: "p", SessionID: "s"})
	if err != nil {
		t.Fatalf("ReadPlan: %v", err)
	}
	if !pluginsEqual(stored.Members[1].Plugins, []string{"new"}) {
		t.Fatalf("计划里存的声明 = %v，want [new]", stored.Members[1].Plugins)
	}
}
