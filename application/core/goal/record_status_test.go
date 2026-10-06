package goal

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// record_status_test.go — goal 状态格的判据：**存档面读回 + 终态判定**。
//
// 判据三条：
//  1. 存档里认得的词读回同一个状态（存档面 `sessionstore.GoalFrame.Status` 是
//     契约之下的 wire，store 存的是词）；
//  2. 认不得的词 / 空串 → `dto.GoalStatusUnknown`：**不折成 active/paused**——
//     折了就是替存档编一个它没说过的事实（随后 Reload 的**位置语义**才是修正
//     active/paused 的权威判据，那是"栈这一位置该是什么"，不是"读不懂就猜一个"）；
//  3. 终态判定只认 completed|failed|aborted——unknown 不是终态（不能因为读不懂
//     就把一个目标从栈上摘掉）。
func TestGoalStatusOfRecordReadsTheWireAndNeverFoldsToAKnownState(t *testing.T) {
	known := []struct {
		word string
		want Status
	}{
		{"active", StatusActive},
		{"paused", StatusPaused},
		{"reviewing", StatusReviewing},
		{"completed", StatusCompleted},
		{"failed", StatusFailed},
		{"aborted", StatusAborted},
		{"waiting_human", StatusWaitingHuman},
	}
	for _, entry := range known {
		if got := StatusOfRecord(entry.word); got != entry.want {
			t.Errorf("StatusOfRecord(%q) = %v，期望 %v", entry.word, got, entry.want)
		}
	}
	for _, wire := range []string{"", "half-exploded", "Active", "waiting human"} {
		got := StatusOfRecord(wire)
		if got != dto.GoalStatusUnknown {
			t.Errorf("认不得的存档词 %q 读回成了 %v，必须落 unknown", wire, got)
		}
	}

	if !StatusCompleted.Terminal() || !StatusFailed.Terminal() || !StatusAborted.Terminal() {
		t.Fatal("completed|failed|aborted 必须是终态")
	}
	for _, alive := range []Status{StatusActive, StatusPaused, StatusReviewing, StatusWaitingHuman, StatusOfRecord("")} {
		if alive.Terminal() {
			t.Errorf("%v 不是终态（读不懂也不许被当成终态）", alive)
		}
	}
	if !IsTerminal(StatusAborted) || IsTerminal(StatusReviewing) {
		t.Fatal("IsTerminal 必须与 Terminal 同源")
	}
}
