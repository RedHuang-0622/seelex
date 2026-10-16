package switching

import (
	"strings"
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

func TestAsyncCompletionPromptFieldNarrowing(t *testing.T) {
	record := dto.AsyncRunRecord{
		Handle:      "bs-7",
		Kind:        "bash_bg",
		State:       dto.AsyncStateDone,
		ExitCode:    3,
		Description: "  构建整个仓库  ",
		Summary:     strings.Repeat("详", 50),
	}
	body := AsyncCompletionPrompt(record, 20)
	for _, want := range []string{
		"后台作业已完成，请取回结果并继续",
		"handle: bs-7",
		"状态: done · exit=3", // 枚举的 String() 才是线上的词（整数值不出本进程）
		"构建整个仓库",
		`bg_manage(op=fetch, handle="bs-7")`,
		`bg_manage(op=done, handle="bs-7")`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("正文应含 %q，got:\n%s", want, body)
		}
	}
	// 摘要按 evidenceChars 有界（这里是 20 rune）。
	if strings.Contains(body, strings.Repeat("详", 21)) {
		t.Fatal("摘要应被 evidenceChars 截断")
	}
}

func TestTeamworkCompletionPromptPointsAtTeamworkTools(t *testing.T) {
	record := dto.TeamworkJobCompletionRecord{
		Handle:   "bteam-1",
		Kind:     "teammate",
		State:    dto.AsyncStateFailed,
		ExitCode: 1,
		Role:     " reviewer ",
		WorkItem: " w-2 ",
		Summary:  "复核未通过",
	}
	body := TeamworkCompletionPrompt(record, 200)
	for _, want := range []string{
		"teammate 作业已完成，请收口",
		"teammate: reviewer（工作项 w-2）",
		"handle: bteam-1",
		"team_context",
		`jobs_manage(op=observe|fetch, handle="bteam-1")`,
		"team_accept / team_fail",
		"复核未通过",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("正文应含 %q，got:\n%s", want, body)
		}
	}
	// 刻意不同：不得指向 bg_manage（两张表的取回工具不是同一个）。
	if strings.Contains(body, "bg_manage") {
		t.Fatalf("teammate 正文不得指向 bg_manage：\n%s", body)
	}
	// 无角色/工作项时的占位。
	if body := TeamworkCompletionPrompt(dto.TeamworkJobCompletionRecord{Handle: "bteam-2"}, 10); !strings.Contains(body, "teammate: —") {
		t.Fatalf("缺归属时应给占位，got:\n%s", body)
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
