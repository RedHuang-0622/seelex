package teamwork

// job_projection.go 是 teammate 作业表的**只读投影判据**（P21-25「域方法随类型走」：从
// `backend/engine/assemble/runtime_teamwork_jobs.go` 把投影与状态折算搬进本域；plan §2.7 把
// `runtime_teamwork*.go` 列为 `backend/multiagent/teamwork/` 的目标）。
//
// 分工（与 `backend/tool/jobs` 的 `ProjectAsyncRuns` 同形）：投影**只搬运**，不推进游标、
// 不销项；"这条是不是终态、该不该唤醒"是消费方的事。装配层只负责"作业表从哪来"（`jobs.Manager`
// 的读数）与信号口接线。

import (
	"github.com/RedHuang-0622/Seele/jobs"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// ProjectJobCompletions 把作业读数搬成契约 DTO（按读取顺序；空表归一为 nil）。
func ProjectJobCompletions(records []jobs.Record) []dto.TeamworkJobCompletionRecord {
	if len(records) == 0 {
		return nil
	}
	out := make([]dto.TeamworkJobCompletionRecord, 0, len(records))
	for _, record := range records {
		out = append(out, JobCompletionFrom(record))
	}
	return out
}

// JobCompletionFrom 把一条作业读数搬成 DTO。单独成函数是为了让"漏搬一列"
// 能被用例钉住——投影面只做搬运，任何判定都不许长在这里。
func JobCompletionFrom(record jobs.Record) dto.TeamworkJobCompletionRecord {
	subject := record.Scope.Subject
	return dto.TeamworkJobCompletionRecord{
		Handle:      string(record.Handle),
		Kind:        string(record.Kind),
		State:       AsyncStateFromJobs(record.State),
		ExitCode:    record.ExitCode,
		SessionID:   record.Scope.Session,
		Role:        RoleFromSubject(subject),
		WorkItem:    record.Node,
		Description: record.Description,
		Summary:     record.Summary,
		Bytes:       record.Bytes,
	}
}

// AsyncStateFromJobs 把框架 `jobs.Manager` 的状态折成契约枚举。
//
// 词的来源是**框架**（jobs.State*：running|done|failed|killed），与我们那张作业状态词表同形
// 但不由我们定义——所以折一次，而且**只在这里折一次**：投影面从此搬的是类型，不是散字符串。
// 折不动的（框架将来加了新状态、或读到空值）落到 `dto.AsyncStateUnknown`：投影照搬，
// 判定留给消费方——"认不得"不许被折成"还在跑"。
func AsyncStateFromJobs(state jobs.State) dto.AsyncState {
	switch state {
	case jobs.StateRunning:
		return dto.AsyncStateRunning
	case jobs.StateDone:
		return dto.AsyncStateDone
	case jobs.StateFailed:
		return dto.AsyncStateFailed
	case jobs.StateKilled:
		return dto.AsyncStateKilled
	}
	return dto.AsyncStateUnknown
}
