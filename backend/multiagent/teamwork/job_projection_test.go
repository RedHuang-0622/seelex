package teamwork

// job_projection_test.go 是作业完成投影的**域级**用例（P21-25：随实现从
// `backend/engine/assemble/runtime_teamwork_jobs.go` 搬进本域）。宿主级用例（作业表读数、信号口、
// 缺装配语义）留在装配层，走同身份别名覆盖同一份判据。

import (
	"testing"

	"github.com/RedHuang-0622/Seele/jobs"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// TestJobCompletionFromCarriesEveryColumn 钉住"投影只搬、不漏列"：一条作业读数的每一列都要到
// DTO 上，且角色由作用域主体反解、工作项取 Node。
func TestJobCompletionFromCarriesEveryColumn(t *testing.T) {
	record := jobs.Record{
		Handle: "bteam-3", Kind: KindWorker, State: jobs.StateFailed, ExitCode: 2,
		Node: "wi-impl", Description: "实现渲染件", Summary: "半途而废", Bytes: 2048,
		Scope: jobs.Scope{Session: "sess-main", Subject: SubjectForRole("exec")},
	}
	got := JobCompletionFrom(record)
	if got.Handle != "bteam-3" || got.Kind != string(KindWorker) || got.State != dto.AsyncStateFailed ||
		got.ExitCode != 2 || got.SessionID != "sess-main" || got.Role != "exec" || got.WorkItem != "wi-impl" ||
		got.Description != "实现渲染件" || got.Summary != "半途而废" || got.Bytes != 2048 {
		t.Fatalf("投影漏列或搬错：%+v", got)
	}
	plain := JobCompletionFrom(jobs.Record{Handle: "h1"})
	if plain.Role != "" || plain.State != dto.AsyncStateUnknown {
		t.Fatalf("缺主体/缺状态时的投影 = %+v", plain)
	}
}

// TestAsyncStateFromJobsFoldsOnlyKnownWords：状态词由**框架**定义，折一次；
// 认不得的词（含空值）落 Unknown——"认不得"不许被折成"还在跑"。
func TestAsyncStateFromJobsFoldsOnlyKnownWords(t *testing.T) {
	cases := map[jobs.State]dto.AsyncState{
		jobs.StateRunning: dto.AsyncStateRunning,
		jobs.StateDone:    dto.AsyncStateDone,
		jobs.StateFailed:  dto.AsyncStateFailed,
		jobs.StateKilled:  dto.AsyncStateKilled,
		jobs.State(""):    dto.AsyncStateUnknown,
		jobs.State("?"):   dto.AsyncStateUnknown,
	}
	for state, want := range cases {
		if got := AsyncStateFromJobs(state); got != want {
			t.Fatalf("AsyncStateFromJobs(%q) = %q, want %q", state, got, want)
		}
	}
}

// TestProjectJobCompletionsKeepsOrderAndNormalizesEmpty：顺序保持、空表归一为 nil。
func TestProjectJobCompletionsKeepsOrderAndNormalizesEmpty(t *testing.T) {
	if ProjectJobCompletions(nil) != nil {
		t.Fatal("空表应当归一为 nil")
	}
	records := []jobs.Record{{Handle: "b1"}, {Handle: "b2"}, {Handle: "b3"}}
	out := ProjectJobCompletions(records)
	if len(out) != 3 || out[0].Handle != "b1" || out[2].Handle != "b3" {
		t.Fatalf("顺序必须保持：%+v", out)
	}
}
