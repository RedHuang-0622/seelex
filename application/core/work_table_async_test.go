package core

// 后台命令（bash background=true）投影到工作表格与请求尾部打点块的判据
// （台账 §10 S3；规格 docs/2026-09-24-async-tool-deferred-ack §10）。
//
// 钉住四件事：
//  1. 界面上要看的一眼全在列里：描述（行标题）、指令（描述列）、实时读数（打点行）；
//  2. 进上下文的那一份**只给最小事实**——不含日志路径、不含末行原文、不含时间戳；
//  3. 登记表是唯一事实源：行随状态出现、随终态/驱逐消失，不进 task 注册表、不落盘；
//  4. 第 4 个生命周期消费者真的接上了变化信号（不需要人工刷新）。

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

func runningAsyncRecord(handle string) dto.AsyncRunRecord {
	now := time.Now()
	return dto.AsyncRunRecord{
		SessionID: "session-a", Handle: handle, Description: "跑一整轮集成测试",
		Command: "go test ./... -count=1", State: dto.AsyncStateRunning, ExitCode: -1,
		LogBytes: 1300, Tail: "ok seelebridge/tools 6.5s",
		LastByteAt: now, LogPath: `C:\Temp\seelex-async-1\a3.log`,
		BatchID: "req-1", StartedAt: now.Add(-2 * time.Second),
	}
}

func workItemByID(rows []WorkItem, id string) (WorkItem, bool) {
	for _, row := range rows {
		if row.ID == id {
			return row, true
		}
	}
	return WorkItem{}, false
}

// 列位分配：描述→行标题、指令→描述列、探针读数→打点行、日志路径→附件列。
func TestAsyncRunProjectsEveryVisibleColumn(t *testing.T) {
	record := runningAsyncRecord("a3")
	rows := buildWorkTable(nil, nil, nil, []dto.AsyncRunRecord{record})
	row, ok := workItemByID(rows, "async:a3")
	if !ok {
		t.Fatalf("后台行没进工作表格: %+v", rows)
	}
	if row.Kind != "task" || row.SourceID != "async:a3" || row.Phase != "task" {
		t.Fatalf("行身份不符（裁定 2：kind=task + source_id=async:<handle>）: %+v", row)
	}
	if row.Status != string(dto.TaskRunning) {
		t.Fatalf("在跑的后台执行状态 = %q", row.Status)
	}
	if row.Task != "跑一整轮集成测试" {
		t.Fatalf("任务描述没成行标题: %q", row.Task)
	}
	if !strings.Contains(row.Description, "go test ./... -count=1") {
		t.Fatalf("运行指令没进描述列: %q", row.Description)
	}
	if len(row.Attachments) != 1 || row.Attachments[0] != record.LogPath {
		t.Fatalf("日志路径没进附件列（GUI 要看完整输出只能靠它）: %v", row.Attachments)
	}
	if len(row.Trace) != 1 {
		t.Fatalf("探针打点条数 = %d", len(row.Trace))
	}
	point := row.Trace[0]
	if point.Operation != asyncProbeOperation || point.Status != dto.AsyncStateRunning {
		t.Fatalf("探针打点身份不符: %+v", point)
	}
	for _, want := range []string{"1.3KiB", "ok seelebridge/tools 6.5s"} {
		if !strings.Contains(point.Evidence, want) {
			t.Fatalf("探针证据缺 %q: %q", want, point.Evidence)
		}
	}
	if point.Duration == "" || row.Elapsed == "" {
		t.Fatalf("耗时没算出来: duration=%q elapsed=%q", point.Duration, row.Elapsed)
	}
	if row.BatchID != "req-1" {
		t.Fatalf("批次归属丢失: %q", row.BatchID)
	}
	// 批次头必须出现，否则前端按批次渲染时这行无处安放。
	batches := buildWorkTableBatches(rows)
	found := false
	for _, batch := range batches {
		if batch.ID == "req-1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("后台行没归进批次头: %+v", batches)
	}
}

// 终态映射：done→completed，failed/killed→failed（表格状态机没有"被杀"这一档）。
func TestAsyncTerminalStatesMapToWorkStatus(t *testing.T) {
	base := runningAsyncRecord("a9")
	base.State = "done"
	base.ExitCode = 0
	failed := base
	failed.Handle, failed.State, failed.ExitCode = "a8", "failed", 2
	killed := base
	killed.Handle, killed.State, killed.ExitCode = "a7", "killed", 137

	rows := buildWorkTable(nil, nil, nil, []dto.AsyncRunRecord{base, failed, killed})
	for id, want := range map[string]string{
		"async:a9": string(dto.TaskCompleted),
		"async:a8": string(dto.TaskFailed),
		"async:a7": string(dto.TaskFailed),
	} {
		row, ok := workItemByID(rows, id)
		if !ok {
			t.Fatalf("%s 没进表格", id)
		}
		if row.Status != want {
			t.Fatalf("%s 状态 = %q，want %q", id, row.Status, want)
		}
	}
}

// 打点块列**在途行 + 待取回的完成行**（打点 K-5 的回填规范），且进上下文的字段必须
// 收窄：路径、日志名、时间戳都不许出现在这里（它们是探针给 GUI 的）。
func TestAsyncTraceLinesCarryNoPathsOrLogContent(t *testing.T) {
	record := runningAsyncRecord("a3")
	lines := asyncTraceLines([]dto.AsyncRunRecord{record}, "session-a")
	if len(lines) != 1 {
		t.Fatalf("打点行数 = %d: %v", len(lines), lines)
	}
	line := lines[0]
	for _, want := range []string{"async:a3", "process", "running", "1.3KiB", "跑一整轮集成测试"} {
		if !strings.Contains(line, want) {
			t.Fatalf("打点行缺 %q: %q", want, line)
		}
	}
	for _, forbidden := range []string{record.LogPath, record.Tail, "a3.log"} {
		if strings.Contains(line, forbidden) {
			t.Fatalf("打点行漏出上下文不该有的内容 %q: %q", forbidden, line)
		}
	}

	// 终态 + 已回填（Notified）⇒ 进块，带**有界摘要**。
	terminal := record
	terminal.State = "done"
	terminal.Notified = true
	terminal.Summary = "done · exit=0 · 12 行 · 1.3KiB · 末行: ok seelebridge/tools"
	terminalLines := asyncTraceLines([]dto.AsyncRunRecord{terminal}, "session-a")
	if len(terminalLines) != 1 {
		t.Fatalf("完成行（已回填）必须进打点块: %v", terminalLines)
	}
	for _, want := range []string{"async:a3", "done", "exit=0", "12 行"} {
		if !strings.Contains(terminalLines[0], want) {
			t.Fatalf("完成行缺 %q: %q", want, terminalLines[0])
		}
	}
	if strings.Contains(terminalLines[0], record.LogPath) || strings.Contains(terminalLines[0], "a3.log") {
		t.Fatalf("完成行漏出日志路径: %q", terminalLines[0])
	}

	// 终态但**没回填过**（Notified=false）= 状态机与投影不一致，不得当成结果进块。
	unnotified := record
	unnotified.State = "done"
	if got := asyncTraceLines([]dto.AsyncRunRecord{unnotified}, "session-a"); len(got) != 0 {
		t.Fatalf("没回填过的终态行进了块: %v", got)
	}
	if got := asyncTraceLines([]dto.AsyncRunRecord{record}, "session-b"); len(got) != 0 {
		t.Fatalf("别的会话的后台命令漏进本会话打点块: %v", got)
	}
}

// 整块语义：没有活动任务、只有一条在跑的后台命令时，打点块必须出现（这是
// "少花一轮往返确认进展"的全部依据）；命令终态后整块消失。
func TestAsyncRunAloneMaterializesTraceBlock(t *testing.T) {
	runtime := &fakeRuntime{}
	service := newTestService(t, &fakeEngine{sessionID: "session-a"}, withTestRuntime(runtime))
	defer service.Shutdown()
	runtime.currentTaskSession = "session-a"

	runtime.asyncRuns = []dto.AsyncRunRecord{runningAsyncRecord("a3")}
	block := service.workTableTraceBlockFor("")
	if !strings.Contains(block, workTableTraceMarkerOpen) || !strings.Contains(block, "- async:a3 process running") {
		t.Fatalf("只有后台命令时打点块没出现: %q", block)
	}
	if !strings.Contains(block, "job_manage") {
		t.Fatalf("打点块缺 async:<句柄> 的读法说明: %q", block)
	}

	// 完成后：行**带着摘要留在块里**（这正是"回填的内容 = 表格的内容"）。
	done := runningAsyncRecord("a3")
	done.State = "done"
	done.Notified = true
	done.Summary = "done · exit=0 · 3 行 · 12B · 末行: ok"
	runtime.asyncRuns = []dto.AsyncRunRecord{done}
	finishedBlock := service.workTableTraceBlockFor("")
	if !strings.Contains(finishedBlock, "async:a3") || !strings.Contains(finishedBlock, "exit=0") {
		t.Fatalf("完成行没有带着摘要回填: %q", finishedBlock)
	}

	// 取回之后行消失（登记表不再报它）⇒ 块自动消失。
	runtime.asyncRuns = nil
	if got := service.workTableTraceBlockFor(""); got != "" {
		t.Fatalf("取回/销项之后打点块应自动消失: %q", got)
	}
}

// 登记表是唯一事实源：它不再报这条记录（终态被驱逐），投影里就没有这行。
// 走的是"重建"而不是"回写"，所以不需要任何补记链路。
func TestAsyncRowDisappearsWhenRegistryDropsIt(t *testing.T) {
	runtime := &fakeRuntime{}
	service := newTestService(t, &fakeEngine{sessionID: "session-a"}, withTestRuntime(runtime))
	defer service.Shutdown()

	runtime.asyncRuns = []dto.AsyncRunRecord{runningAsyncRecord("a3")}
	service.refreshWorkTableFromSources()
	if _, ok := workItemByID(serviceWorkTableRows(service), "async:a3"); !ok {
		t.Fatal("后台行没出现在工作表格里")
	}

	runtime.asyncRuns = []dto.AsyncRunRecord{runningAsyncRecord("a4")}
	service.refreshWorkTableFromSources()
	rows := serviceWorkTableRows(service)
	if _, ok := workItemByID(rows, "async:a3"); ok {
		t.Fatalf("登记表已经没有 a3，投影里还留着（事实源不唯一）: %+v", rows)
	}
	if _, ok := workItemByID(rows, "async:a4"); !ok {
		t.Fatalf("新句柄 a4 没投影出来: %+v", rows)
	}
}

// 第 4 个生命周期消费者：执行域一发声，表格就重投影——不靠模型再调一次工具，
// 也不靠心跳轮询。
func TestAsyncChangeSignalReprojectsWorkTable(t *testing.T) {
	runtime := &fakeRuntime{asyncEvents: make(chan struct{}, 1)}
	service := newTestService(t, &fakeEngine{sessionID: "session-a"}, withTestRuntime(runtime))
	defer service.Shutdown()

	// 服务已装配完（构造期的刷新看到的是空表），此时新增一行还不该出现在投影里。
	runtime.asyncRuns = []dto.AsyncRunRecord{runningAsyncRecord("a3")}
	if _, ok := workItemByID(serviceWorkTableRows(service), "async:a3"); ok {
		t.Fatal("还没发信号就有行：这个用例就证明不了消费者")
	}

	runtime.asyncEvents <- struct{}{}
	deadline := time.After(5 * time.Second)
	for {
		if _, ok := workItemByID(serviceWorkTableRows(service), "async:a3"); ok {
			return
		}
		select {
		case <-deadline:
			t.Fatal("后台执行变化信号没驱动工作表格重投影（第 4 消费者没接上？）")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// 后台行按上限封顶，且不挤掉真实任务行（在途优先，终态按最新补齐）。
func TestAsyncRowsAreCappedAndNeverEvictTasks(t *testing.T) {
	records := make([]dto.AsyncRunRecord, 0, asyncWorkMaxRows+20)
	for index := range asyncWorkMaxRows + 20 {
		record := runningAsyncRecord(fmt.Sprintf("t%d", index))
		record.State = "done"
		records = append(records, record)
	}
	records = append(records, runningAsyncRecord("a999"))
	tasks := []dto.TaskRecord{{ID: "task:1", Phase: "task", Kind: "task", Task: "真任务", Status: dto.TaskRunning}}

	rows := buildWorkTable(nil, tasks, nil, records)
	asyncCount := 0
	for _, row := range rows {
		if strings.HasPrefix(row.ID, asyncWorkRowPrefix) {
			asyncCount++
		}
	}
	if asyncCount != asyncWorkMaxRows {
		t.Fatalf("后台行数 = %d，want 上限 %d", asyncCount, asyncWorkMaxRows)
	}
	if _, ok := workItemByID(rows, "async:a999"); !ok {
		t.Fatal("在途的后台行被终态行挤掉了（必须先保在途）")
	}
	if _, ok := workItemByID(rows, "task:1"); !ok {
		t.Fatal("真实任务行被后台行挤掉")
	}
}

// TC-K5-1（打点 K-5 的"有界"判据）：注入 100 个已完成作业 →
//   - 工作表格里的作业行 ≤ asyncWorkMaxRows；
//   - 打点块里的行 ≤ workTableTraceMaxLines（含块头块尾）；
//   - 在途行优先于完成行（它们在动）；
//   - 被截掉的完成行**补一行汇总**，不静默消失（静默会让模型以为"没有待取回的作业"）。
func TestAsyncBackfillStaysBounded(t *testing.T) {
	records := make([]dto.AsyncRunRecord, 0, 120)
	for index := range 100 {
		record := runningAsyncRecord(fmt.Sprintf("f%d", index))
		record.State = "done"
		record.Notified = true
		record.Summary = fmt.Sprintf("done · exit=0 · %d 行 · 1.3KiB · 末行: ok", index)
		record.EndedAt = time.Now().Add(time.Duration(index) * time.Millisecond)
		records = append(records, record)
	}
	for index := range 4 {
		records = append(records, runningAsyncRecord(fmt.Sprintf("r%d", index)))
	}

	rows := asyncWorkItems(records)
	asyncRows := 0
	for _, row := range rows {
		if strings.HasPrefix(row.ID, asyncWorkRowPrefix) {
			asyncRows++
		}
	}
	if asyncRows > asyncWorkMaxRows {
		t.Fatalf("工作表格作业行 = %d，超过上限 %d", asyncRows, asyncWorkMaxRows)
	}

	lines := asyncTraceLines(records, "session-a")
	if len(lines) > asyncWorkMaxRows {
		t.Fatalf("打点行 = %d，超过上限 %d", len(lines), asyncWorkMaxRows)
	}
	// 在途行必须全部在（4 条），且排在完成行之前。
	for index := range 4 {
		want := fmt.Sprintf("async:r%d", index)
		found := false
		for position, line := range lines {
			if strings.Contains(line, want) {
				found = true
				if position >= 4 {
					t.Fatalf("在途行 %s 没有排在完成行之前: %v", want, lines)
				}
			}
		}
		if !found {
			t.Fatalf("在途行 %s 被完成行挤掉了: %v", want, lines)
		}
	}
	// 被截掉的完成行要有汇总行，而不是无声无息。
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "另有") {
		t.Fatalf("被截断的完成行没有汇总行: %v", lines)
	}

	// 整块（含块头块尾与读法说明）不得超过 workTableTraceMaxLines。
	runtime := &fakeRuntime{}
	service := newTestService(t, &fakeEngine{sessionID: "session-a"}, withTestRuntime(runtime))
	defer service.Shutdown()
	runtime.currentTaskSession = "session-a"
	runtime.asyncRuns = records
	block := service.workTableTraceBlockFor("")
	if got := strings.Count(strings.TrimSpace(block), "\n") + 1; got > workTableTraceMaxLines {
		t.Fatalf("打点块行数 = %d，超过上限 %d:\n%s", got, workTableTraceMaxLines, block)
	}
}

func serviceWorkTableRows(service *Service) []WorkItem {
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	return CloneWorkItems(service.Core.Snapshot.Runtime.WorkTable)
}
