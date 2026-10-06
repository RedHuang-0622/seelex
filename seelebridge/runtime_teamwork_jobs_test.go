package seelebridge

// runtime_teamwork_jobs_test.go — 钉住 **teammate 作业表**（Seele jobs.Manager）的只读
// 投影与信号扇出（契约 contract.TeamworkJobCompletion，2026-10-04）。
//
// 为什么在这一层钉：这条链的输入端在 seelebridge（作业表 → DTO + 信号），"做完自动返回"
// 的判定在 application。投影少搬一列、信号只送到一个读者，都会表现为"teammate 干完了，
// leader 那边什么都没发生"——而后者在类型上完全合法（通道是容量 1 的单接收者）。

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/jobs"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	seeletelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/seelebridge/teamwork"
)

// TestTeamworkJobSignalsFanOutToEverySubscriber：上游 jobs.Manager.Events() 是容量 1 的
// **单接收者**通道：两个读者抢它时，后起的那个一次也收不到。扇出器必须先做这件事——
// 事件投影与"终态触发回合"是两个读者。
func TestTeamworkJobSignalsFanOutToEverySubscriber(t *testing.T) {
	hub := newTeamworkJobSignals()
	upstream := make(chan struct{}, 1)
	hub.start(upstream)
	t.Cleanup(hub.close)

	first := hub.subscribe()
	second := hub.subscribe()

	upstream <- struct{}{}
	for name, channel := range map[string]<-chan struct{}{"事件投影": first, "终态触发": second} {
		select {
		case <-channel:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s 这个订阅者没收到信号（扇出没做到位：单接收者通道被一个读者独占）", name)
		}
	}
}

// TestTeamworkJobSignalsCloseIsIdempotentAndSafeBeforeStart：未 start 过就 close 不得
// 挂住（等价于没装配作业面）；重复 close 幂等。
func TestTeamworkJobSignalsCloseIsIdempotentAndSafeBeforeStart(t *testing.T) {
	unstarted := newTeamworkJobSignals()
	unstarted.close()
	unstarted.close()

	hub := newTeamworkJobSignals()
	hub.start(make(chan struct{}, 1))
	hub.close()
	hub.close()
}

func TestTeamworkJobCompletionsProjectsRecords(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	if err := r.SetTeamworkBackend(teamworkTestBackend(&memPlanStore{}, "s-team")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	if r.TeamworkJobEvents() == nil {
		t.Fatal("装配了 teamwork 就必须给出 teammate 作业的信号口（否则这条链永远不醒）")
	}

	ctx := seeletelemetry.WithSessionID(context.Background(), "s-team")
	// 载荷故意不是 JSON：执行体解码失败即把作业收敛成 failed 终态——本用例要的就是
	// "作业表里真的出现一条终态 teammate 作业"，而不是跑一轮真回合。
	handle, err := r.teamworkJobs.Dispatch(ctx, jobs.Spec{
		Kind:        teamwork.KindWorker,
		Scope:       jobs.Scope{Session: "s-team", Subject: teamwork.SubjectForRole("exec")},
		Node:        "wi-impl",
		Payload:     json.RawMessage("{不是 JSON"),
		Description: "exec/wi-impl: 实现渲染件",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	// 信号必须真的送到 application 侧那条订阅（扇出的第二个读者）。
	select {
	case <-r.TeamworkJobEvents():
	case <-time.After(3 * time.Second):
		t.Fatal("teammate 作业派发后 application 侧那条订阅没收到信号")
	}

	record := waitTeammateRecord(t, r, string(handle))
	if record.State != dto.AsyncStateFailed {
		t.Fatalf("载荷解码失败的作业应落 failed 终态：%+v", record)
	}
	if record.Kind != string(teamwork.KindWorker) {
		t.Fatalf("类别搬运错了：%+v", record)
	}
	if record.SessionID != "s-team" {
		t.Fatalf("会话归属搬运错了（触发回合的落点就是它）：%+v", record)
	}
	if record.Role != "exec" {
		t.Fatalf("角色必须从作用域主体 emp_<role> 反解：%+v", record)
	}
	if record.WorkItem != "wi-impl" {
		t.Fatalf("工作项归属必须搬运（收口按它做）：%+v", record)
	}
	if record.Description == "" {
		t.Fatalf("行标题必须搬运（正文里\"哪件事\"就靠它）：%+v", record)
	}
}

// TestTeamworkJobCompletionsNilWithoutBackend：未装配 teamwork 时两个成员都退场——
// 不是给一个空数组，而是"这条链不存在"（消费方的 select 忽略 nil 通道）。
func TestTeamworkJobCompletionsNilWithoutBackend(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	if got := r.TeamworkJobCompletions(); got != nil {
		t.Fatalf("未装配作业面时不该有投影：%+v", got)
	}
	if r.TeamworkJobEvents() != nil {
		t.Fatal("未装配作业面时不该有信号口")
	}
}

// waitTeammateRecord 轮询到该句柄的 teammate 作业读数（测试里用真实时钟，只等短窗口）。
func waitTeammateRecord(t *testing.T, r *Runtime, handle string) dto.TeamworkJobCompletionRecord {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var found dto.TeamworkJobCompletionRecord
	for time.Now().Before(deadline) {
		for _, record := range r.TeamworkJobCompletions() {
			if record.Handle != handle {
				continue
			}
			found = record
			if record.State != dto.AsyncStateRunning {
				return record
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("teammate 作业 %s 没有在窗口内终态（最后读数：%+v）", handle, found)
	return found
}
