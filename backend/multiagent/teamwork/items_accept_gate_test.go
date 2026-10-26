package teamwork

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/jobs"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/store"
)

// 验收闸门的判据（2026-10-15 审计 D2）：**按作业状态**，不按"句柄还在不在册"。
//
// 旧口径用"在册"当"还在飞"的代理，于是终态行留在表里时闸门继续拒——拒绝文案给出的出路
// （`jobs_manage(op=kill)`）对一条已经跑完的作业毫无意义，验收被卡在一句办不到的指示上。
// 三条用例分别钉住：终态即放行、真在跑仍然拒、验收会把那条作业行结清。

// TestAcceptPassesOnceTheJobIsTerminal：作业已终态（哪怕那一行还在册）→ 验收必须放行。
//
// 形状来自真实会话：作业因**执行体起不来 / 载荷解不开**直接落成终态，而尾插那一段根本没跑
// （executor 在这些路径上提前返回，settle 不会执行），于是工作项**仍是 running**、作业却已经
// 不在跑。旧口径此时继续拒（"handle 还在册"），验收被卡在一句办不到的指示上；新口径按作业
// 状态放行。这里用框架的**外部终态入口**（`Complete`）确定性地构造同一形状。
func TestAcceptPassesOnceTheJobIsTerminal(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()

	block := make(chan struct{})
	fixture.runner.block = block
	t.Cleanup(func() { close(block) })
	handle, err := fixture.coordinator.DispatchItem(ctx, "wi-req")
	if err != nil {
		t.Fatalf("DispatchItem: %v", err)
	}
	waitRequests(t, fixture, 1)
	if err := fixture.jobs.Complete(ctx, jobs.Handle(handle), jobs.StateFailed, 1, "执行体起不来"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	record, ok := fixture.jobs.Observe(jobs.Handle(handle))
	if !ok || record.Running() {
		t.Fatalf("外部终态之后作业应当是终态但在册，得到 %+v ok=%v", record, ok)
	}
	if item := itemState(t, fixture, "wi-req"); store.TeamworkWorkItemStatusOrPending(item) != store.TeamworkItemRunning {
		t.Fatalf("这条路径上工作项应当仍是 running（尾插没跑，没人写状态），得到 %s", store.TeamworkWorkItemStatusOrPending(item))
	}

	if err := fixture.coordinator.AcceptItem(ctx, "wi-req", "现场已停，人工处置完"); err != nil {
		t.Fatalf("作业已终态时验收必须放行（旧口径按「在册」拒会让验收卡死）：%v", err)
	}
	if item := itemState(t, fixture, "wi-req"); store.TeamworkWorkItemStatusOrPending(item) != store.TeamworkItemDone {
		t.Fatalf("验收之后工作项应 done，得到 %s", store.TeamworkWorkItemStatusOrPending(item))
	}
}

// TestAcceptStillRefusesARunningJob：真在跑就仍然拒——闸门没有被放松成"永远放行"。
func TestAcceptStillRefusesARunningJob(t *testing.T) {
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

	err := fixture.coordinator.AcceptItem(ctx, "wi-req", "验收通过")
	if err == nil || !strings.Contains(err.Error(), "还在跑") {
		t.Fatalf("作业在跑时验收必须被拒，得到 %v", err)
	}
	// 拒绝文案必须给出**可达**的出路：这条作业确实还在跑，kill 是能照做的下一步。
	if !strings.Contains(err.Error(), "jobs_manage(op=kill") {
		t.Fatalf("拒绝文案必须给出可达的下一步（团队作业表用 jobs_manage）：%v", err)
	}
	if item := itemState(t, fixture, "wi-req"); store.TeamworkWorkItemStatusOrPending(item) != store.TeamworkItemRunning {
		t.Fatalf("被拒的验收不得改状态，得到 %s", store.TeamworkWorkItemStatusOrPending(item))
	}
}

// TestAcceptRetiresTheSettledJobRow：验收 = 这件事结清（计划销项 + 作业行销项）。
//
// 销项不再寄存在 leader 的自觉上（旧口径要求谁想起来谁调 `jobs_manage(op=done)`）。
func TestAcceptRetiresTheSettledJobRow(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()

	handle, err := fixture.coordinator.DispatchItem(ctx, "wi-req")
	if err != nil {
		t.Fatalf("DispatchItem: %v", err)
	}
	waitItemStatus(t, fixture, "wi-req", store.TeamworkItemReview)
	if _, ok := fixture.jobs.Observe(jobs.Handle(handle)); !ok {
		t.Fatalf("尾插走完到验收之间，作业行必须还在册：它是看板与「做完自动返回」读的证据")
	}
	if err := fixture.coordinator.AcceptItem(ctx, "wi-req", "验收通过"); err != nil {
		t.Fatalf("AcceptItem: %v", err)
	}
	if record, ok := fixture.jobs.Observe(jobs.Handle(handle)); ok {
		t.Fatalf("验收之后那条作业行应当已销项，仍在册：%+v", record)
	}
	// 幂等：重复验收不报错（done 已是幂等动作）。
	if err := fixture.coordinator.AcceptItem(ctx, "wi-req", "再验一次"); err != nil {
		t.Fatalf("重复验收必须幂等：%v", err)
	}
}

// TestTeamworkHandlesCarryTheirNamespace：团队作业表的句柄必须带 `bteam-` token——
// 这是"两张表不可能撞名"的前置（后台表是 bt- / bs-，见 dto.JobNamespace）。
func TestTeamworkHandlesCarryTheirNamespace(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	handle, err := fixture.coordinator.DispatchItem(context.Background(), "wi-req")
	if err != nil {
		t.Fatalf("DispatchItem: %v", err)
	}
	namespace, known := dto.JobNamespaceOf(string(handle))
	if !known || namespace != dto.JobNamespaceTeamwork {
		t.Fatalf("团队作业句柄 %q 的命名空间 = (%q, %v)，want (%q, true)",
			handle, namespace, known, dto.JobNamespaceTeamwork)
	}
}
