package adapters

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// session_status_record_test.go — 存储面的会话可见状态读回：**跨包同一份词**的互锁。
//
// 判据三条：
//  1. store 的每一个持久词都必须被可见面这一格认得，且读回**同一个词**（两包各存
//     一份词表时的老毛病：store 加一个词，可见面这边静默变成"别的状态"）；
//  2. 认不得的词、空串 → `SessionStatusUnknown`，**不折成** idle/running 这些已知态
//     （折了就是替存储层编事实，驱逐/归档/待批判据会照着错的读）；
//  3. 越界的枚举值读不出"某个已知状态"（词表是闭合的）。
func TestSessionStatusOfRecordLocksTheStoreVocabulary(t *testing.T) {
	durable := []sessionstore.Status{
		sessionstore.StatusDraft,
		sessionstore.StatusIdle,
		sessionstore.StatusRunning,
		sessionstore.StatusQueued,
		sessionstore.StatusAwaitingApproval,
		sessionstore.StatusArchived,
	}
	for _, word := range durable {
		got := sessionStatusOfRecord(word)
		if got.String() != string(word) {
			t.Errorf("store 词 %q 读回成了 %q（两包词表分叉）", word, got)
		}
	}

	for _, wire := range []sessionstore.Status{"", "half-exploded", "Restoring", "IDLE"} {
		if got := sessionStatusOfRecord(wire); got != dto.SessionStatusUnknown {
			t.Errorf("认不得的存储词 %q 读回成了 %q，必须落 unknown（不是任何已知态）", wire, got)
		}
	}
}

// TestSessionStatusUnknownIsNotAKnownState：unknown 不是 idle/running 的别名——
// 消费方（目录行徽标/驱逐守卫）据它拿到的必须是"读不懂"这个事实本身。
func TestSessionStatusUnknownIsNotAKnownState(t *testing.T) {
	unknown := sessionStatusOfRecord("")
	for _, known := range []dto.SessionStatus{
		dto.SessionStatusDraft, dto.SessionStatusIdle, dto.SessionStatusRunning,
		dto.SessionStatusQueued, dto.SessionStatusAwaitingApproval,
		dto.SessionStatusArchived, dto.SessionStatusRestoring,
	} {
		if unknown == known {
			t.Fatalf("认不得的存储词被折成了已知状态 %q", known)
		}
	}
}
