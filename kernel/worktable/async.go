package worktable

// async.go — 作业（bash_bg / read_batch / subagent / teammate）的工作表格行与打点行投影。
//
// 事实源是引擎侧的后台执行登记表（经 `contract` 端口读出）：投影行不进 task 注册表、
// 不随会话落盘（不变量 I-21）。因此后台行的生命完全跟着登记表：出现（派发）、
// 更新（探针）、消失（终态被驱逐）。

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
	model "github.com/RedHuang-0622/seelex/contract/protocol"
)

const (
	// AsyncWorkRowPrefix 是投影行的 ID 前缀：async:a3。行身份沿用 owner 裁定的
	// kind=task + source_id=async:<handle>。
	AsyncWorkRowPrefix = "async:"
	// AsyncProbeOperation 是探针打点的操作名（GUI 打点表里一眼能看出这不是任务打点，
	// 是一次现场采样）。
	AsyncProbeOperation = "async_probe"
	// AsyncWorkMaxRows 是后台行在工作表格里占的行数上限。登记表总槽 256，全部投影
	// 会把真实任务挤出 WorkTableRows 截断线——这里按"在途优先 + 最新终态补齐"封顶，
	// 是**展示边界**，不改变执行域里的任何状态。
	AsyncWorkMaxRows = 32
)

// AsyncWorkItems 把后台执行全量投影成工作表格行（跨会话：工作表格是全局台账，
// 行自带 SessionID 供前端按会话筛选）。在途行永不丢，终态行只按最新补齐到上限。
func AsyncWorkItems(records []dto.AsyncRunRecord, limits Limits) []model.WorkItem {
	var running, finished []dto.AsyncRunRecord
	for _, record := range records {
		if record.State == dto.AsyncStateRunning {
			running = append(running, record)
			continue
		}
		finished = append(finished, record)
	}
	rows := make([]model.WorkItem, 0, len(running)+AsyncWorkMaxRows)
	for _, record := range running {
		rows = append(rows, AsyncRunToWorkItem(record, limits))
	}
	for index := len(finished) - 1; index >= 0 && len(rows) < AsyncWorkMaxRows; index-- {
		rows = append(rows, AsyncRunToWorkItem(finished[index], limits))
	}
	return rows
}

// AsyncRunToWorkItem 映射一条后台执行到工作表格行。
//
// 列位分配按"界面上要看什么"：Task=模型写的任务描述、Description=运行的命令行、
// Trace=探针读数（字节数/末行/耗时）、Attachments=输出文件路径（只上 GUI）。
func AsyncRunToWorkItem(record dto.AsyncRunRecord, limits Limits) model.WorkItem {
	task := record.Description
	if strings.TrimSpace(task) == "" {
		// 描述是 background=true 的必填项，正常拿不到空；回退命令行是防御，不是第二判据。
		task = record.Command
	}
	elapsed := record.EndedAt.Sub(record.StartedAt)
	if record.State == dto.AsyncStateRunning {
		elapsed = time.Since(record.StartedAt)
	}
	rowID := AsyncWorkRowPrefix + record.Handle
	return model.WorkItem{
		ID:          rowID,
		SessionID:   record.SessionID,
		Phase:       "task",
		Kind:        "task",
		SourceID:    rowID,
		Task:        TruncateWorkEvidence(task, 200),
		Description: TruncateWorkEvidence("后台命令: "+record.Command+"（handle="+record.Handle+"）", limits.EvidenceChars),
		Status:      AsyncWorkStatus(record.State),
		Attachments: []string{record.LogPath},
		BatchID:     record.BatchID,
		CreatedAt:   record.StartedAt,
		StartedAt:   record.StartedAt,
		EndedAt:     record.EndedAt,
		Elapsed:     FormatWorkDuration(elapsed),
		Trace:       []model.WorkTracePoint{AsyncProbePoint(record, elapsed, limits)},
	}
}

// AsyncProbePoint 是一次探针采样的打点行：状态、字节数、末行、耗时。
func AsyncProbePoint(record dto.AsyncRunRecord, elapsed time.Duration, limits Limits) model.WorkTracePoint {
	at := record.StartedAt
	if record.State == dto.AsyncStateRunning {
		if !record.LastByteAt.IsZero() {
			at = record.LastByteAt
		}
	} else if !record.EndedAt.IsZero() {
		at = record.EndedAt
	}
	evidence := fmt.Sprintf("%s · 输出 %s", record.State, FormatAsyncBytes(record.LogBytes))
	if record.State != dto.AsyncStateRunning {
		evidence += fmt.Sprintf(" · exit %d", record.ExitCode)
	}
	if record.Tail != "" {
		evidence += " · 末行: " + record.Tail
	}
	if record.Truncated {
		evidence += " · 输出已截断（完整内容见 attachments 里的日志路径）"
	}
	if record.Degraded {
		evidence += " · 进程树挂不上，终止只及直接子进程"
	}
	return model.WorkTracePoint{
		At: at, Status: record.State.String(), Operation: AsyncProbeOperation,
		Evidence: TruncateWorkEvidence(evidence, limits.EvidenceChars),
		Duration: FormatWorkDuration(elapsed),
	}
}

// AsyncWorkStatus 把执行域状态映射到工作表格的权威状态字面量。
// killed 归 failed：表格状态机没有"被杀"这一档，被杀本来就是没跑完。
func AsyncWorkStatus(state dto.AsyncState) string {
	switch state {
	case dto.AsyncStateRunning:
		return dto.TaskRunning.String()
	case dto.AsyncStateDone:
		return dto.TaskCompleted.String()
	default:
		return dto.TaskFailed.String()
	}
}

// AsyncTraceLines 生成打点块里的作业行，且只取本会话——打点块注入在组装请求的那个
// 会话尾部。
//
// 两类行（设计文档 §A.3 的回填规范，打点 K-5）：
//   - **在途行**：句柄、类别、状态、已产出字节数、行标题 —— 与环境里"任务打点"同语义；
//   - **完成行**（待取回）：句柄、类别、状态与**有界摘要**（退出码 + 行数 + 字节数 +
//     有界末行）。这就是"回填的内容 = 表格的内容"：行还在，但它现在带着结果。
//
// 进上下文的字段仍然刻意收窄：不含日志路径、不含末行原文（摘要里的末行是**压成一行、
// 限长**的采样）、不含时间戳。全文只走 bg_manage(op=fetch) 或 attachments 里的日志路径
// ——完成行随打点块每轮重播，塞全文就是按轮数线性烧 token。
//
// 有界：单行 ≤AsyncWorkMaxRows 条，块行数由调用方（workTableTraceBlockFor）按
// workTableTraceMaxLines 截断；被截掉的完成行**补一行汇总**，而不是静默消失——
// 静默会让模型以为"没有待取回的作业"，而事实是"太多"。
func AsyncTraceLines(records []dto.AsyncRunRecord, sessionID string, limits Limits) []string {
	lines := make([]string, 0, len(records))
	var running, finished []dto.AsyncRunRecord
	for _, record := range records {
		if sessionID != "" && record.SessionID != sessionID {
			continue
		}
		if record.State == dto.AsyncStateRunning {
			running = append(running, record)
			continue
		}
		// 终态但**还没回填过**的记录不进块：那是状态机与投影之间的不一致（finish 会在
		// 迁移终态的同一步置上 notified），宁可漏一行也不要把一条没有摘要的行当成结果。
		if record.Notified {
			finished = append(finished, record)
		}
	}
	// 在途优先（它们在动），完成行按完成时间**倒序**（最新完成的先被看见）。
	sort.SliceStable(finished, func(left, right int) bool {
		return finished[left].EndedAt.After(finished[right].EndedAt)
	})
	ordered := append(append([]dto.AsyncRunRecord(nil), running...), finished...)
	dropped := 0
	// 汇总行也要占一格：上限是"这一块里最多几条"，不是"明细几条 + 汇总另算"。
	limit := AsyncWorkMaxRows
	if len(ordered) > limit {
		limit--
	}
	for _, record := range ordered {
		if len(lines) >= limit {
			dropped++
			continue
		}
		lines = append(lines, AsyncTraceLine(record, limits))
	}
	if dropped > 0 {
		lines = append(lines, fmt.Sprintf("- …另有 %d 个作业未在块内列出（详情见工作表格）", dropped))
	}
	return lines
}

// AsyncTraceLine 渲染一条作业行：句柄、类别、状态、（完成行）摘要或（在途行）标题。
func AsyncTraceLine(record dto.AsyncRunRecord, limits Limits) string {
	kind := strings.TrimSpace(record.Kind)
	if kind == "" {
		kind = "process"
	}
	fields := []string{AsyncWorkRowPrefix + record.Handle, kind, record.State.String(), FormatAsyncBytes(record.LogBytes)}
	if record.State != dto.AsyncStateRunning {
		// 完成行带**有界摘要**（回填的全部内容）。
		if summary := TruncateWorkEvidence(record.Summary, limits.EvidenceChars); summary != "" {
			fields = append(fields, summary)
		}
		return "- " + strings.Join(fields, " ")
	}
	if detail := TruncateWorkEvidence(record.Description, 40); detail != "" {
		fields = append(fields, detail)
	} else if command := TruncateWorkEvidence(record.Command, 40); command != "" {
		fields = append(fields, command)
	}
	return "- " + strings.Join(fields, " ")
}

// TeamworkTraceLines 生成打点块里的 **teammate 作业行**（与 AsyncTraceLines 同一块、
// 同一目的：让 leader 在回合边界读到"谁跑完了"）。
//
// 为什么 teammate 作业也要进这块（2026-10-04）：它们的终态触发（triggerTeamworkJobCompletions）
// **绝不唤醒忙会话**——leader 正在跑的时候，那条回执只能靠这一块被看见。少了它，忙碌期的
// 完成回执就只能等 leader 自己想起来去 team_items / jobs_manage 问一遍。
//
// 与 async 行刻意分开两件事：
//   - 行首打 `teamwork:<handle>`：句柄空间与 tools 那张表独立，取回工具也不同
//     （jobs_manage vs bg_manage）——合并成同一种行，模型就会拿错工具；
//   - 只取**本会话**的行（与 AsyncTraceLines 同口径：打点块注入在组装请求的那个会话尾部）。
func TeamworkTraceLines(records []dto.TeamworkJobCompletionRecord, sessionID string, limits Limits) []string {
	lines := make([]string, 0, len(records))
	var running, finished []dto.TeamworkJobCompletionRecord
	for _, record := range records {
		if sessionID != "" && record.SessionID != sessionID {
			continue
		}
		if record.State == dto.AsyncStateRunning {
			running = append(running, record)
			continue
		}
		finished = append(finished, record)
	}
	ordered := append(append([]dto.TeamworkJobCompletionRecord(nil), running...), finished...)
	for _, record := range ordered {
		lines = append(lines, TeamworkTraceLine(record, limits))
	}
	return lines
}

// TeamworkTraceLine 渲染一条 teammate 作业行：句柄、状态、归属（teammate/工作项）、
// 摘要（完成行）或标题（在途行）。
func TeamworkTraceLine(record dto.TeamworkJobCompletionRecord, limits Limits) string {
	fields := []string{"teamwork:" + record.Handle, PromptKind(record.Kind), record.State.String()}
	if owner := TeamworkOwnerText(record); owner != "" {
		fields = append(fields, owner)
	}
	if record.State != dto.AsyncStateRunning {
		if summary := TruncateWorkEvidence(record.Summary, limits.EvidenceChars); summary != "" {
			fields = append(fields, summary)
		}
		return "- " + strings.Join(fields, " ")
	}
	if detail := TruncateWorkEvidence(record.Description, 40); detail != "" {
		fields = append(fields, detail)
	}
	return "- " + strings.Join(fields, " ")
}

// TeamworkOwnerText 是 teammate 行的归属文本（`<role>/<work item>`）。
func TeamworkOwnerText(record dto.TeamworkJobCompletionRecord) string {
	role := strings.TrimSpace(record.Role)
	item := strings.TrimSpace(record.WorkItem)
	switch {
	case role != "" && item != "":
		return role + "/" + item
	case role != "":
		return role
	default:
		return item
	}
}

// FormatAsyncBytes 把字节数写成便于扫读的量级（界面与打点块共用一个口径）。
func FormatAsyncBytes(bytes int64) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%dB", bytes)
	case bytes < 1024*1024:
		return fmt.Sprintf("%.1fKiB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%.1fMiB", float64(bytes)/1024/1024)
	}
}

// PromptKind 是进正文前的类别兜底：类别缺失时按 process 读，与打点块同口径。
func PromptKind(kind string) string {
	if trimmed := strings.TrimSpace(kind); trimmed != "" {
		return trimmed
	}
	return "process"
}
