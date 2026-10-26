package switching

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// 本文件钉后台作业终态触发的判据（M5-R6-async-completion 接收者转移）：
// 终态词表、两条触发正文（用户行文本的字段收窄与工具指向）、以及 teammate 作业表的窄端口读取。

func TestAsyncCompletionTriggersVocabulary(t *testing.T) {
	tri := []dto.AsyncState{dto.AsyncStateDone, dto.AsyncStateFailed}
	for _, state := range tri {
		if !AsyncCompletionTriggers(state) {
			t.Fatalf("%q 应触发", state)
		}
	}
	// killed 不是"作业有了结果"（是被终止）；running 不是终态；空值不触发。
	for _, state := range []dto.AsyncState{
		dto.AsyncStateKilled, dto.AsyncStateRunning, dto.AsyncStateUnknown,
	} {
		if AsyncCompletionTriggers(state) {
			t.Fatalf("%q 不该触发", state)
		}
	}
}

// fakeTeamworkJobs 实现 contract.TeamworkJobCompletion（窄可选端口）。
type fakeTeamworkJobs struct {
	records []dto.TeamworkJobCompletionRecord
	events  chan struct{}
}

func (fake *fakeTeamworkJobs) TeamworkJobCompletions() []dto.TeamworkJobCompletionRecord {
	return fake.records
}

func (fake *fakeTeamworkJobs) TeamworkJobEvents() <-chan struct{} { return fake.events }

var _ teamworkJobPort = (*fakeTeamworkJobs)(nil)

func TestTeamworkJobPortReads(t *testing.T) {
	events := make(chan struct{})
	fake := &fakeTeamworkJobs{records: []dto.TeamworkJobCompletionRecord{{Handle: "bteam-1"}}, events: events}

	if got := TeamworkJobCompletions(fake); len(got) != 1 || got[0].Handle != "bteam-1" {
		t.Fatalf("应转发端口读数，got %+v", got)
	}
	if got := TeamworkJobEvents(fake); got != events {
		t.Fatalf("应转发信号口，got %v", got)
	}
	// 未装配（nil runtime）与未实现该窄口：都退化成"这条链不存在"。
	if got := TeamworkJobCompletions(nil); got != nil {
		t.Fatalf("nil runtime 应为 nil，got %+v", got)
	}
	if got := TeamworkJobEvents(nil); got != nil {
		t.Fatalf("nil runtime 的信号口应为 nil，got %v", got)
	}
}
