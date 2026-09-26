package tools

import (
	"testing"
)

// 子进程调用系契约（P0）的**已可跑**回归断言。
//
// 本文件只放"当前代码就已成立、且契约落地后必须继续成立"的性质；尚需实现的
// 打点（K-1..K-6）的验收用例在 docs/subprocess_contract_testcases.md 里，按
// 依赖顺序逐条启用。
//
// 落点依据（设计文档 docs/tool_concurrency_design.md §A.3 / §A.4）：
//   - observe 必须只读（不推进游标），fetch 必须消费式（推进游标）——两条性质
//     丢任何一条都会丢输出：前者"看一眼"就吃掉增量，后者每轮重播整份日志。
//   - 工具名 → 路由组 是权限的**单一事实源**：bash 工具族分裂（K-3）之后，
//     分组表是唯一需要更新的地方，这张断言就是它的验收钩子。

// TestObserveDoesNotAdvanceCursor 钉住"观察只读 / 取回消费"这对性质。
//
// 契约里这两个动作是同一个 interface 的两个 op（`Manage(OpObserve)` /
// `Manage(OpFetch)`），所以它们的差别必须在**游标**上可观测：连续观察 N 次之后，
// 取回仍要能拿到全部增量；一旦取回，同样的增量不得再给第二次。
func TestObserveDoesNotAdvanceCursor(t *testing.T) {
	registry := newAsyncRegistryForTest(t)
	run, started, err := registry.begin("sess-c", "make build", "编译", "req-c")
	if err != nil || !started {
		t.Fatalf("begin: started=%v err=%v", started, err)
	}
	const payload = "step 1\nstep 2\n"
	writeLogForTest(t, run.logPath, payload)

	// ① 观察：读多少次都不该消费输出（探针读数里带的是 LogBytes 快照）。
	for i := 0; i < 3; i++ {
		infos := registry.infos()
		if len(infos) != 1 {
			t.Fatalf("第 %d 次观察读数条数 = %d，want 1", i+1, len(infos))
		}
		if infos[0].LogBytes != int64(len(payload)) {
			t.Fatalf("第 %d 次观察的 LogBytes = %d, want %d", i+1, infos[0].LogBytes, len(payload))
		}
	}

	// ② 取回：仍然拿到全部增量（观察没有把游标推走）。
	next, delta, _, ok := registry.advanceTail(run.handle, asyncPollTailBudget)
	if !ok {
		t.Fatal("取回失败：句柄不在登记表里")
	}
	if delta != payload {
		t.Fatalf("观察之后再取回得到 %q, want %q —— 观察推进了游标就等于吞掉了输出", delta, payload)
	}
	registry.markCursor(run.handle, next, false)

	// ③ 消费式：同一段增量不得再给第二次。
	if _, again, _, _ := registry.advanceTail(run.handle, asyncPollTailBudget); again != "" {
		t.Fatalf("第二次取回应为空，却拿到 %q —— 重复取回会把整份日志重播进上下文", again)
	}

	// ④ 新字节仍要能取到（消费式不等于一次取完就失效）。
	writeLogForTest(t, run.logPath, "step 3\n")
	if _, more, _, _ := registry.advanceTail(run.handle, asyncPollTailBudget); more != "step 3\n" {
		t.Fatalf("新字节取回 = %q, want %q", more, "step 3\n")
	}
}

// TestJobContractToolFaceRoutingTable 是"工具名 → 路由组"这张表的验收钩子。
//
// 为什么值得单独立一条：契约把**授权依据**放在路由组上（单一事实源），而
// bash 工具族分裂（打点 K-3）与 async_output 下线（打点 L-2）都只改这张表。
// 表变了这里就该变——这是有意的耦合，不是脆弱的重复。
func TestJobContractToolFaceRoutingTable(t *testing.T) {
	groups := DefaultPermissionGroupList()
	cases := []struct {
		tool  string
		group string
		why   string
	}{
		// 读簇：不改任何共享状态。
		{"read_file", GroupRO, "读文件不改状态"},
		{"grep_search", GroupRO, "只读搜索"},
		{"glob", GroupRO, "只读列举"},
		// bash 工具族（打点 K-3）：只读面免打断，后台面与串行面同组。
		{"bash_read", GroupRO, "只读命令，免打断（handler 侧有服务端守卫 ClassifyCommand）"},
		{"read_batch", GroupRO, "只读扇出作业（Kind=inline），不碰共享状态"},
		// 写簇：跟 bash 同组，sub/员工才能取回/杀掉自己派发的后台作业。
		{"write_file", GroupRW, "写项目文件"},
		{"edit_file", GroupRW, "改项目文件"},
		{"bash", GroupRW, "同步串行命令：保守归类为写"},
		{"bash_bg", GroupRW, "后台受管：实现 §A.1 的 Add，回执不是结果"},
		{"job_manage", GroupRW, "取回/终止/销项管的正是 bash 起的执行体"},
		// 循环控制流 / 能力面。
		{"task_complete", GroupCTL, "结束 loop"},
		{"fork_subagents", GroupCTL, "派生执行结构"},
		{"switch_plugin", GroupADM, "改变能力面本身"},
		{"computer_click", GroupRWDesktop, "共享外设"},
	}
	for _, item := range cases {
		group, ok := RoutePermissionGroup(groups, item.tool)
		if !ok {
			t.Errorf("%s 未归入任何权限组：未分封的工具按框架默认走审批，而它的理由是 %q", item.tool, item.why)
			continue
		}
		if group.Name != item.group {
			t.Errorf("%s 路由组 = %q, want %q（%s）", item.tool, group.Name, item.group, item.why)
		}
	}

	// 这张表已经落在契约上：bash 工具族按名字分流（K-3），两个已下线的旧名字
	// （取回 / 终止）由 job_manage 的 op 取代（L-2 / L-4）。表变了这里就该变——这是有意
	// 的耦合，不是脆弱的重复。
}
