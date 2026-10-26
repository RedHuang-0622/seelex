package table

import (
	"fmt"
	"sort"
	"strings"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/view/worktable"
)

const (
	// TraceMarkerOpen starts the request-tail work trace block.
	TraceMarkerOpen = "<!-- seelex:worktable:v1 -->"
	// TraceMarkerClose ends the request-tail work trace block.
	TraceMarkerClose = "<!-- /seelex:worktable:v1 -->"
	// TraceMaxLines is the whole-block line budget, including markers and hints.
	TraceMaxLines = 30
)

// TraceInputs is one immutable request-tail projection input.
type TraceInputs struct {
	SessionID     string
	ViewSessionID string
	Tasks         []dto.TaskRecord
	AsyncRuns     []dto.AsyncRunRecord
	TeamworkRuns  []dto.TeamworkJobCompletionRecord
	Limits        worktable.Limits
}

// BuildTraceBlock renders the session-scoped request-tail work table.
func BuildTraceBlock(input TraceInputs) string {
	effectiveSession := input.SessionID
	if effectiveSession == "" || effectiveSession == input.ViewSessionID {
		effectiveSession = input.ViewSessionID
	}
	active := make([]dto.TaskRecord, 0, len(input.Tasks))
	for _, record := range input.Tasks {
		if record.Status != dto.TaskCompleted && record.Status != dto.TaskFailed {
			active = append(active, record)
		}
	}
	sort.SliceStable(active, func(left, right int) bool {
		return active[left].ID < active[right].ID
	})
	lines := make([]string, 0, len(active)+1)
	for _, record := range active {
		retry := ""
		if record.RetryCount > 0 {
			retry = fmt.Sprintf(" retry=%d", record.RetryCount)
		}
		lines = append(lines, fmt.Sprintf("- %s %s%s %s",
			record.ID, record.Status, retry, worktable.TruncateWorkEvidence(record.Task, 80)))
	}
	asyncLines := worktable.AsyncTraceLines(input.AsyncRuns, effectiveSession, input.Limits)
	lines = append(lines, asyncLines...)
	teamworkLines := worktable.TeamworkTraceLines(input.TeamworkRuns, effectiveSession, input.Limits)
	lines = append(lines, teamworkLines...)
	if len(lines) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString(TraceMarkerOpen + "\n# 工作打点表（系统维护，只读；任务状态与打点以工作表格为准）\n")
	budget := TraceMaxLines - 3
	hint := ""
	if len(asyncLines) > 0 {
		hint = "async:<句柄> 行是作业（bash_bg 后台命令 / read_batch 扇出读 / subagent）：" +
			"字节数=已产出输出量；在途行带标题，完成行带摘要（exit/行数/末行采样）。" +
			"句柄自带来源（工具作业 bt-3 / 子代理作业 bs-7）。" +
			"取结果用 bg_manage(op=fetch, handle)、只读看进展用 op=observe、终止用 op=kill、" +
			"销项用 op=done。"
		budget--
	}
	if len(teamworkLines) > 0 {
		hint += "teamwork:<句柄> 行是 teammate 作业（leader 派出去的活）：" +
			"句柄形如 bteam-7（团队作业表；拿错工具会被显式拒绝，不会静默命中别的作业）。" +
			"结论用 team_context（非消费读）、产物正文用 jobs_manage(op=observe|fetch, handle)，" +
			"复核完用 team_accept / team_fail 收口（验收通过即销掉那条作业行，不必自己销项）。"
		if len(asyncLines) == 0 {
			budget--
		}
	}
	truncated := false
	if len(lines) > budget {
		lines = lines[:budget-1]
		truncated = true
	}
	for _, line := range lines {
		builder.WriteString(line + "\n")
	}
	if truncated {
		builder.WriteString("- …（打点表已达上限，详情见工作表格）\n")
	}
	if hint != "" {
		builder.WriteString(hint + "\n")
	}
	builder.WriteString(TraceMarkerClose)
	return builder.String()
}
