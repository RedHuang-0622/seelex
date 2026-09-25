package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// ── 后台命令（bash background=true）的工作表格投影 ─────────────────────
//
// 事实源是 seelebridge 的后台执行登记表，这里只做**只读投影**：投影行不进 task
// 注册表、不随会话落盘（不变量 I-21）。为什么不走注册表——注册表 record.Tasks 会
// 持久化并在恢复时回灌，而句柄表在内存，真进去就会在重启后留下一条永远 running 的
// 假行；要避免就得给驱逐/关停补终态回执，那等于把"不做持久化"重新买回来。
//
// 因此后台行的生命完全跟着登记表：出现（派发）、更新（探针）、消失（终态被驱逐）。

const (
	// asyncWorkRowPrefix 是投影行的 ID 前缀：async:a3。行身份沿用 owner 裁定的
	// kind=task + source_id=async:<handle>。
	asyncWorkRowPrefix = "async:"
	// asyncProbeOperation 是探针打点的操作名（GUI 打点表里一眼能看出这不是任务打点，
	// 是一次现场采样）。
	asyncProbeOperation = "async_probe"
	// asyncWorkMaxRows 是后台行在工作表格里占的行数上限。登记表总槽 256，全部投影
	// 会把真实任务挤出 WorkTableRows 截断线——这里按"在途优先 + 最新终态补齐"封顶，
	// 是**展示边界**，不改变执行域里的任何状态。
	asyncWorkMaxRows = 32
)

// asyncRunsForTable 读后台执行投影。三处建表入口（实时重投影、会话快照、冷读面）
// 都走它，保证同一张表不管从哪个读面看都有同一批后台行。
//
// 登记表锁是叶子锁（tools 侧从不回调进 application），所以持 ViewMu 时调用不会成环。
func (state *serviceState) asyncRunsForTable() []dto.AsyncRunRecord {
	if state == nil || state.Deps.Runtime == nil {
		return nil
	}
	return state.Deps.Runtime.AsyncRunsSnapshot()
}

// asyncWorkItems 把后台执行全量投影成工作表格行（跨会话：工作表格是全局台账，
// 行自带 SessionID 供前端按会话筛选）。在途行永不丢，终态行只按最新补齐到上限。
func asyncWorkItems(records []dto.AsyncRunRecord) []WorkItem {
	var running, finished []dto.AsyncRunRecord
	for _, record := range records {
		if record.State == dto.AsyncStateRunning {
			running = append(running, record)
			continue
		}
		finished = append(finished, record)
	}
	rows := make([]WorkItem, 0, len(running)+asyncWorkMaxRows)
	for _, record := range running {
		rows = append(rows, asyncRunToWorkItem(record))
	}
	for index := len(finished) - 1; index >= 0 && len(rows) < asyncWorkMaxRows; index-- {
		rows = append(rows, asyncRunToWorkItem(finished[index]))
	}
	return rows
}

// asyncRunToWorkItem 映射一条后台执行到工作表格行。
//
// 列位分配按"界面上要看什么"：Task=模型写的任务描述、Description=运行的命令行、
// Trace=探针读数（字节数/末行/耗时）、Attachments=输出文件路径（只上 GUI）。
func asyncRunToWorkItem(record dto.AsyncRunRecord) WorkItem {
	task := record.Description
	if strings.TrimSpace(task) == "" {
		// 描述是 background=true 的必填项，正常拿不到空；回退命令行是防御，不是第二判据。
		task = record.Command
	}
	elapsed := record.EndedAt.Sub(record.StartedAt)
	if record.State == dto.AsyncStateRunning {
		elapsed = time.Since(record.StartedAt)
	}
	rowID := asyncWorkRowPrefix + record.Handle
	return WorkItem{
		ID:          rowID,
		SessionID:   record.SessionID,
		Phase:       "task",
		Kind:        "task",
		SourceID:    rowID,
		Task:        truncateWorkEvidence(task, 200),
		Description: truncateWorkEvidence("后台命令: "+record.Command+"（handle="+record.Handle+"）", Limits().EvidenceChars),
		Status:      asyncWorkStatus(record.State),
		Attachments: []string{record.LogPath},
		BatchID:     record.BatchID,
		CreatedAt:   record.StartedAt,
		StartedAt:   record.StartedAt,
		EndedAt:     record.EndedAt,
		Elapsed:     formatWorkDuration(elapsed),
		Trace:       []WorkTracePoint{asyncProbePoint(record, elapsed)},
	}
}

// asyncProbePoint 是一次探针采样的打点行：状态、字节数、末行、耗时。
func asyncProbePoint(record dto.AsyncRunRecord, elapsed time.Duration) WorkTracePoint {
	at := record.StartedAt
	if record.State == dto.AsyncStateRunning {
		if !record.LastByteAt.IsZero() {
			at = record.LastByteAt
		}
	} else if !record.EndedAt.IsZero() {
		at = record.EndedAt
	}
	evidence := fmt.Sprintf("%s · 输出 %s", record.State, formatAsyncBytes(record.LogBytes))
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
	return WorkTracePoint{
		At: at, Status: record.State, Operation: asyncProbeOperation,
		Evidence: truncateWorkEvidence(evidence, Limits().EvidenceChars),
		Duration: formatWorkDuration(elapsed),
	}
}

// asyncWorkStatus 把执行域状态映射到工作表格的权威状态字面量。
// killed 归 failed：表格状态机没有"被杀"这一档，被杀本来就是没跑完。
func asyncWorkStatus(state string) string {
	switch state {
	case dto.AsyncStateRunning:
		return string(dto.TaskRunning)
	case "done":
		return string(dto.TaskCompleted)
	default:
		return string(dto.TaskFailed)
	}
}

// asyncTraceLines 生成打点块里的后台行：**只在跑的那些**（终态行不进块，与"无活动
// 任务块自动消失"同语义），且只取本会话——打点块注入在组装请求的那个会话尾部。
//
// 进上下文的字段是刻意收窄的：句柄、状态、输出字节数、任务描述（或命令行短截断）。
// 不含日志路径、不含末行原文、不含时间戳：日志正文由 async_output 按游标增量交付，
// 探针读数重复进上下文只会每轮白烧 token。
func asyncTraceLines(records []dto.AsyncRunRecord, sessionID string) []string {
	lines := make([]string, 0, len(records))
	for _, record := range records {
		if record.State != dto.AsyncStateRunning {
			continue
		}
		if sessionID != "" && record.SessionID != sessionID {
			continue
		}
		if len(lines) >= asyncWorkMaxRows {
			break
		}
		fields := []string{asyncWorkRowPrefix + record.Handle, record.State, formatAsyncBytes(record.LogBytes)}
		if detail := truncateWorkEvidence(record.Description, 40); detail != "" {
			fields = append(fields, detail)
		} else if command := truncateWorkEvidence(record.Command, 40); command != "" {
			fields = append(fields, command)
		}
		lines = append(lines, "- "+strings.Join(fields, " "))
	}
	return lines
}

// formatAsyncBytes 把字节数写成便于扫读的量级（界面与打点块共用一个口径）。
func formatAsyncBytes(bytes int64) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%dB", bytes)
	case bytes < 1024*1024:
		return fmt.Sprintf("%.1fKiB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%.1fMiB", float64(bytes)/1024/1024)
	}
}
