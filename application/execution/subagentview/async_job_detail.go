package subagentview

// async_job_detail.go — 后台作业行（工作表格里 `async:<句柄>`）的详情数据面。
//
// 为什么单开一份：作业行在执行域里**不是一个会话**（没有 History、没有上下文
// 快照），它的"记录"就是输出文件。硬塞进会话分支会让「会话记录」撒谎；不给它任何
// 读数，详情弹窗就只能永远停在占位（2026-10-29 用户实测：后台测试跑起来后点开
// 那一行，面板只有「加载会话记录…」）。
//
// 边界：本文件不碰执行域状态，只搬运 `AsyncRuns` 只读投影 + 有界输出尾部；写侧
// （派发 / 取回 / 终止）仍归 `backend/tool/registry`，判据（字节数/末行/耗时）
// 仍归 `kernel/worktable`——这里只做"行 → 详情"的搬运与状态映射。

import (
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
	model "github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/worktable"
)

// maxAsyncDetailOutputBytes 是详情弹窗一次带回的输出窗口上限。
//
// 弹窗打开期间每 1.5s 拉一次：窗口放大就是按秒烧 IPC 带宽、按秒重绘 DOM。16KiB
// 足够看住一条在跑的测试的"最后几屏"，完整日志仍走 LogPath 或 bg_manage(op=fetch)。
const maxAsyncDetailOutputBytes = 16 << 10

// asyncJobHandle 解析作业行 ID（`async:<句柄>`）：不是作业行返回 false。
//
// 前缀字面量只取 `worktable.AsyncWorkRowPrefix` 一处：行身份跨层必须同一份，
// 在这里重写一个字面量就会让"点开详情"与"表格认行"各说一套。
func asyncJobHandle(nodeID string) (string, bool) {
	if !strings.HasPrefix(nodeID, worktable.AsyncWorkRowPrefix) {
		return "", false
	}
	handle := strings.TrimSpace(strings.TrimPrefix(nodeID, worktable.AsyncWorkRowPrefix))
	if handle == "" {
		return "", false
	}
	return handle, true
}

// asyncJobDetail 组装作业行详情：读数来自执行域只读投影，输出正文来自有界尾部读口。
//
// 句柄不在册（已结束被驱逐 / 已销项 / 进程重启后登记表清空）时**仍然返回详情**：
// "不在册"是有用的答案，不是打开弹窗的失败——失败只会把面板重新钉在占位。
func (c *Coordinator) asyncJobDetail(handle string) *model.SubagentDetail {
	record, found := c.asyncRunRecord(handle)
	job := &model.SubagentAsyncRun{Handle: handle, Found: found}
	detail := &model.SubagentDetail{Async: job}
	if !found {
		return detail
	}
	elapsed := worktable.FormatWorkDuration(asyncJobElapsed(record))
	job.Kind = record.Kind
	job.State = record.State.String()
	job.Command = c.truncateSubagentEvidence(record.Command)
	job.Description = c.truncateSubagentEvidence(record.Description)
	job.LogPath = record.LogPath
	job.LogBytes = record.LogBytes
	job.Lines = record.Lines
	job.ExitCode = record.ExitCode
	job.Summary = c.truncateSubagentEvidence(record.Summary)
	job.Tail = c.truncateSubagentEvidence(record.Tail)
	job.Truncated = record.Truncated
	job.Degraded = record.Degraded
	job.StartedAt = record.StartedAt
	job.EndedAt = record.EndedAt
	if c.asyncOutputTail != nil {
		window := c.asyncOutputTail(handle, maxAsyncDetailOutputBytes)
		job.Output = window.Output
		job.OutputWindowed = window.Windowed
	}
	detail.Running = record.State == dto.AsyncStateRunning
	detail.Status = asyncJobNodeStatus(record.State)
	detail.Elapsed = elapsed
	detail.Goal = job.Description
	detail.Summary = job.Summary
	// 失败/被杀进程的原文就是它的终态摘要（退出码 + 行数 + 末行）：详情错误面
	// 不能空着，否则界面只能显示一个 FAILED 徽标却不说为什么。
	if detail.Status == model.NodeFailed {
		detail.Error = job.Summary
	}
	// 功能打点复用工作表格的探针点（同一份判据，不在这里写第二份字节/末行口径）。
	detail.Trace = []model.WorkTracePoint{worktable.AsyncProbePoint(
		record, asyncJobElapsed(record),
		worktable.Limits{EvidenceChars: c.limits().EvidenceChars},
	)}
	return detail
}

// asyncRunRecord 在后台执行表只读投影里按句柄取一条记录。
//
// 句柄自带命名空间（`bt-` / `bs-`）且进程内单调，因此只按句柄匹配、不按会话过滤：
// 工作表格是跨会话台账，详情要能打开别的会话派出去的作业行。
func (c *Coordinator) asyncRunRecord(handle string) (dto.AsyncRunRecord, bool) {
	if c.asyncRuns == nil || handle == "" {
		return dto.AsyncRunRecord{}, false
	}
	for _, record := range c.asyncRuns() {
		if record.Handle == handle {
			return record, true
		}
	}
	return dto.AsyncRunRecord{}, false
}

// asyncJobElapsed 算作业耗时：在途按"派发到现在"，终态按"派发到结束"。
//
// 为什么不用墙钟猜状态：耗时只是展示，状态一律取 `record.State`（执行体判定）。
func asyncJobElapsed(record dto.AsyncRunRecord) time.Duration {
	if record.StartedAt.IsZero() {
		return 0
	}
	if record.State == dto.AsyncStateRunning || record.EndedAt.IsZero() {
		return time.Since(record.StartedAt)
	}
	elapsed := record.EndedAt.Sub(record.StartedAt)
	if elapsed < 0 {
		return 0
	}
	return elapsed
}

// asyncJobNodeStatus 把执行域状态映射到详情状态词。
//
// killed 归 failed：作业被杀就是没跑完（与工作表格 `AsyncWorkStatus` 同一口径）。
func asyncJobNodeStatus(state dto.AsyncState) model.NodeStatus {
	switch state {
	case dto.AsyncStateRunning:
		return model.NodeRunning
	case dto.AsyncStateDone:
		return model.NodeCompleted
	case dto.AsyncStateFailed, dto.AsyncStateKilled:
		return model.NodeFailed
	default:
		return model.NodeUnknown
	}
}
