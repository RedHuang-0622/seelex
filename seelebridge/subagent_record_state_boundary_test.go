package seelebridge

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// subagent_record_state_boundary_test.go — **落盘那一格**的未知词取舍。
//
// 记录状态（`sessionstore.NodeSessionRecord.Status`）是唯一落盘的状态格。它的枚举化
// （`dto.SubAgentNodeStatus`，int+iota）带来一个必须显式回答的问题：**旧文件/外来文件里
// 认不得的词怎么办**？
//
// 取舍（写在 nodeStateOfRecord 上）：折成 `dto.SubAgentUnknown`——
//   - **不炸**：一个认不得的词不许让整条记录（乃至整轮恢复）读不出来；
//   - **不静默改写语义**：Unknown 不是终态，"读不懂"绝不被折算成"已完成/已失败"。
//
// 与进程内 wire 的严格口径（`dto.SubAgentNodeStatus.UnmarshalJSON` 认不得就报错）**不矛盾**：
// 进程内的词由同一份代码写出，读不懂就是 bug；落盘的词要跨版本、跨机器读回来。
func TestNodeStateOfRecordKeepsUnknownNonTerminal(t *testing.T) {
	cases := []struct {
		wire string
		want dto.SubAgentNodeStatus
	}{
		{"queued", dto.SubAgentQueued},
		{"running", dto.SubAgentRunning},
		{"done", dto.SubAgentDone},
		{"failed", dto.SubAgentFailed},
		{"interrupted", dto.SubAgentInterrupted},
		{"", dto.SubAgentUnknown},              // 老记录/半成品：没写状态
		{"half-exploded", dto.SubAgentUnknown}, // 认不得的词：折成 Unknown，不炸
	}
	for _, testCase := range cases {
		if got := nodeStateOfRecord(testCase.wire); got != testCase.want {
			t.Errorf("nodeStateOfRecord(%q) = %s，想要 %s", testCase.wire, got, testCase.want)
		}
	}

	// 核心那一条：认不得的词**不是终态**（否则"读不懂"会被下游当成"已完成"）。
	unknown := nodeStateOfRecord("half-exploded")
	if unknown == dto.SubAgentDone || unknown == dto.SubAgentFailed {
		t.Fatalf("认不得的落盘词被折算成了终态 %s——这正是要防的那一类静默改写", unknown)
	}
}
