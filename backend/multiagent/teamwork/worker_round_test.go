package teamwork

// worker_round_test.go 是 teammate 回合面三条纯判据的域级用例（P21-29：随实现从
// `backend/engine/assemble/runtime_teamwork.go` 搬进本域）。

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/jobs"
)

// TestJobRecordCeilingOnlyRaisesAboveFrameworkDefault：记录槽上限**只抬不降**——
// 团队规模换算出的值低于框架缺省时取缺省；计划侧"不限制"（<=0）按一名成员算。
func TestJobRecordCeilingOnlyRaisesAboveFrameworkDefault(t *testing.T) {
	floor := jobs.DefaultLimits().Records
	if floor <= 0 {
		t.Fatalf("框架缺省上限应当是正数：%d", floor)
	}
	if got := JobRecordCeiling(0); got != floor {
		t.Fatalf("计划侧不限制时按一名成员算，仍不低于缺省 %d：%d", floor, got)
	}
	if got := JobRecordCeiling(-3); got != floor {
		t.Fatalf("负值同解：%d", got)
	}
	// 换算值低于缺省时取缺省（这就是"只抬不降"这一半）。
	if got := JobRecordCeiling(2); got != floor {
		t.Fatalf("换算值（2 × %d）低于缺省时应取缺省 %d：%d", RoundsPerMember, floor, got)
	}
	// 大到足以超过缺省：换算值直接生效（另一半）。
	members := floor/RoundsPerMember + 2
	big := JobRecordCeiling(members)
	if big != members*RoundsPerMember || big <= floor {
		t.Fatalf("%d 名成员的上限 = %d（缺省 %d）", members, big, floor)
	}
}

// TestTurnLimitPrefersRequested：成员级 max_turns 给了正数就照用，否则回退宿主给的默认。
func TestTurnLimitPrefersRequested(t *testing.T) {
	if got := TurnLimit(20, 12); got != 20 {
		t.Fatalf("给了正数应当照用：%d", got)
	}
	for _, unset := range []int{0, -1} {
		if got := TurnLimit(unset, 12); got != 12 {
			t.Fatalf("未给（%d）应当回退默认：%d", unset, got)
		}
	}
}

// TestRoundInputCarriesMilestoneAndGoal：回合输入正文带里程碑标签与正文；没有正文时如实说明
// （不假装有输入）；没有里程碑时不出标签（阶段口径已退场）。
func TestRoundInputCarriesMilestoneAndGoal(t *testing.T) {
	got := RoundInput(WorkerRequest{Role: "exec", Milestone: "m1", Goal: "  实现渲染件  "})
	if !strings.HasPrefix(got, "<milestone>m1</milestone>\n") {
		t.Fatalf("里程碑标签应当在最前：%q", got)
	}
	if !strings.Contains(got, "<round_input>\n实现渲染件\n</round_input>") {
		t.Fatalf("正文应当 trim 后进 round_input：%q", got)
	}
	if !strings.Contains(got, "以 exec 的身份完成这一轮") {
		t.Fatalf("task 行应当点名角色：%q", got)
	}

	plain := RoundInput(WorkerRequest{Role: "pm"})
	if strings.Contains(plain, "<milestone>") {
		t.Fatalf("没有里程碑时不该出标签：%q", plain)
	}
	if !strings.Contains(plain, "（本轮没有新的工作正文）") {
		t.Fatalf("没有正文时应当如实说明：%q", plain)
	}
}
