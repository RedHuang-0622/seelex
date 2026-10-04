package core

// 重启恢复会话 → 上下文再次到达压缩额度 → 压缩失败 → 现场实际发生了什么？
//
// 用户口径（2026-10-02）：压缩失败 → 留下失败记录 → **原始上下文继续存在** →
// 模型仍然直接看到原来的上下文，不中断继续工作。
//
// 本用例把这条旅程放回**重启恢复**这条路上：会话重启后继续跑、上下文再次越线、
// 这次压缩仍然拿不到模型读后感（失败）。判据只有一条——**尚未被任何压缩覆盖的
// 轮次，必须仍然在 provider 历史里**：压缩失败什么都不该动，更不能在失败的同时
// 把模型还看得见的轮次悄悄丢掉（那正是"保留了失败痕、同时把上下文截断"）。

import (
	"os"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
)

// restartRefillRounds 是重启后新追加的轮数（每轮约 8000 tokens）。26 轮 = 实测
// 已经越出 provider 窗口（200k）的量：那一档下最早 2 轮被截掉。
const restartRefillRounds = 26

// restartRefillWithinWindowRounds 是"越线但装得进 provider 窗口"的轮数
// （22 轮 ≈ 176k < 窗口 200k，> 安全线 ≈ 166.8k）：这是「重启 → 越线 → 压缩」
// 这条旅程里**该有的**那一档——压缩失败、失败痕留下、上下文原样一轮不少。
const restartRefillWithinWindowRounds = 22

// restartRefillProbeEnv 打开"越出窗口"那一档的探针实跑：默认跳过（它记的是"要修的
// 那个现象"，不是"应有的行为"——口径待裁决，见
// docs/devlog/2026-10-04-compaction-facts-durable-channel-and-restart-restore.md §4），
// 设成非空即实跑复现现场。
const restartRefillProbeEnv = "SEELEX_REPRO_RESTART_REFILL"

// startRestartRound 在重启恢复后的会话上开新回合（自动路径）：**把上一份状态传下去**
// ——生产就是这条（chat.go 的 `previousTask := CurrentTaskExecutionFor(sessionID)`），
// 回合续接继承的是"会话上下文事实"（上下文版本 / 保留窗口起点 / 压缩记录）。
//
// 传 nil 会造出一个保留窗口起点归零的新状态，让这次装配把**已被压出的前缀**重新计入
// 预算：那是夹具造出来的假现场（更容易越线、更容易截断），不是重启后的样子。
func startRestartRound(t *testing.T, service *Service, sessionID, requestID string) {
	t.Helper()
	service.ViewMu.Lock()
	defer service.ViewMu.Unlock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: requestID}
	previous := service.components.tasks.CurrentTaskExecutionFor(sessionID)
	service.components.tasks.BeginTaskFor(sessionID, requestID, "inspect", "high", previous, TaskCheckpoint{})
}

// TestProbeCompactionFailureAfterRestartEncounter（**红灯复现，等待口径裁决**）
//
// 实测（本轮真跑）：
//   - 22 轮（合计 ≈ 176k < 窗口 200k）→ 全部保留，装配照常发出；
//   - 26 轮（合计 ≈ 208k > 窗口 200k）→ 只保留最新 24 轮，最早的 2 轮从
//     provider 历史里消失，而这条失败痕写的是「no_model_summary estimated=…
//     budget=… window=… overhead=…」——没有一个字说上下文被截断了。
//
// 也就是说：**压缩失败时"上下文原样"这条口径，在请求装不进 provider 窗口时
// 是假的**。截断本身在那种尺寸下无法避免（请求不能超过物理窗口），可选的修法
// 互斥（拒绝发送 / 如实告知 / 回退有界 checkpoint 压缩），需要先定口径。
// 本用例因此先跳过：它记录的是"要修的那个现象"，不是"应有的行为"。
func TestProbeCompactionFailureAfterRestartEncounter(t *testing.T) {
	if os.Getenv(restartRefillProbeEnv) == "" {
		t.Skip("等待口径裁决：压缩失败 + 装不进 provider 窗口时该拒绝、该如实告知、还是该回退有界 checkpoint 压缩（实跑复现：SEELEX_REPRO_RESTART_REFILL=1 go test ./application/core/ -run TestProbeCompactionFailureAfterRestartEncounter -v）")
	}
	const (
		firstRequestID  = "task-restart-refill-1"
		secondRequestID = "task-restart-refill-2"
	)
	store := &retiredRecordChannelSessions{}
	// ── 重启之前：压过一次（有模型读后感 → 真的压成了）──────────────────
	first, sessionID := startRestartSession(t, store,
		restartCompactionRuntime(&compactionIndexRecorder{receipt: context_runtime.CompactionIndexReceipt{
			SegmentID:     "compact-restart-refill",
			Summary:       "## 压缩内容 (Compacted Context)\n### 目标 (Goal)\n模型读后感",
			SummarySource: context_runtime.CompactionSummarySourceReplay,
		}}, context_runtime.CompactionSummarySourceReplay),
		firstRequestID, 30)
	if compacted := compactNow(t, first, sessionID); compacted.EventTo == 0 {
		t.Fatalf("夹具前提：首次压缩应带区间：%+v", compacted)
	}
	persistRestartSession(t, first, store, sessionID)
	first.Shutdown()

	// ── 进程重启：宿主这次拿不到模型读后感，之后的压缩必然失败 ──────────
	engine := &fakeEngine{}
	second := newTestService(t, engine,
		withTestSessions(store),
		withTestRuntime(restartCompactionRuntime(&compactionIndexRecorder{}, context_runtime.CompactionSummarySourceLocal)),
	)
	if err := second.ResumeSession(sessionID); err != nil {
		t.Fatalf("重启后 ResumeSession(%q): %v", sessionID, err)
	}
	// 会话继续跑：新一轮（自动路径，不是 /compact），上下文再次长到阈值之上。
	startRestartRound(t, second, sessionID, secondRequestID)
	marks := appendWindowRounds(t, second, secondRequestID, restartRefillRounds, 32_000)

	if _, err := second.components.context.PrepareExecutionContext(secondRequestID, "continue"); err != nil {
		t.Fatalf("装配上下文: %v", err)
	}

	// 失败痕必须留下（这部分行为是对的）。
	records := compactionRecordsOf(t, second)
	failures := 0
	for _, record := range records {
		if record.Failed {
			failures++
		}
	}
	if failures != 1 {
		t.Fatalf("这次压缩拿不到模型读后感，应恰好留一条失败痕，实际 %d 条：%+v", failures, records)
	}

	// 判据：失败痕留下了，但**尚未被压缩覆盖**的轮次不能跟着消失。
	history := engine.History()
	kept := retainedRounds(history, marks)
	if kept != len(marks) {
		t.Fatalf("红灯：压缩失败的同时把上下文截断了——重启后新长出来的 %d 轮里只有 %d 轮还在"+
			"provider 历史里（最早的一轮 %s 已消失），而这次压缩什么都没压成："+
			"失败痕说「上下文原样」，实际却把模型还看得见的轮次丢了。",
			len(marks), kept, marks[0])
	}
}

// TestRestartRefillCompactionFailureKeepsTraceWithReadbackError——「重启 → 越线 →
// 压缩」这条旅程的可跑复现（用户口径 2026-10-04）。
//
// 与上面那条探针的分工：探针（26 轮）钉的是"请求装不进 provider 窗口时**截断无法
// 避免**、该选哪条路"这个待裁决现象；本条钉的是同一条旅程在**装得进窗口**那一档
// （22 轮 ≈ 176k > 安全线 166.8k）的应有行为，四件事一起断：
//
//	① 重启之后上下文再次越线，这次压缩压不下去（读数闸拿不到模型读后感）；
//	② 失败痕留下**并带上读数闸的报错原文**（` error=…`）——此前只写
//	   `no_model_summary estimated=… budget=… window=… overhead=…`，"这次为什么
//	   读不到读后感"一个字没有，读痕的人不知道该去查配置、查素材还是查那次调用；
//	③ 失败痕进快照可见面（右栏「上下文压缩」的唯一来路），且跨重启仍在（前一条
//	   用例已钉通道，这里钉的是这一轮新写的那条痕也走同一条通道）；
//	④ 上下文原样：尚未被任何压缩覆盖的轮次一轮不少（"压缩失败不该覆盖 agent 已经
//	   看见的上下文"）。
func TestRestartRefillCompactionFailureKeepsTraceWithReadbackError(t *testing.T) {
	const (
		firstRequestID  = "task-restart-refill-trace-1"
		secondRequestID = "task-restart-refill-trace-2"
		// 生产形态的读数闸注记：重放调用在运行时失败，落回本地确定性压缩；末尾那句
		// 底层报错是现场（connect: connection refused）留下的原文。
		readbackError = "compact-local:replay-failed 前缀重放两次调用均失败，已回退本地确定性压缩：connect: connection refused"
	)
	store := &retiredRecordChannelSessions{}
	// ── 重启之前：压过一次（有模型读后感 → 真的压成了）──────────────────
	first, sessionID := startRestartSession(t, store,
		restartCompactionRuntime(&compactionIndexRecorder{receipt: context_runtime.CompactionIndexReceipt{
			SegmentID:     "compact-restart-refill-trace",
			Summary:       "## 压缩内容 (Compacted Context)\n### 目标 (Goal)\n模型读后感",
			SummarySource: context_runtime.CompactionSummarySourceReplay,
		}}, context_runtime.CompactionSummarySourceReplay),
		firstRequestID, 30)
	if compacted := compactNow(t, first, sessionID); compacted.EventTo == 0 {
		t.Fatalf("夹具前提：首次压缩应带区间：%+v", compacted)
	}
	persistRestartSession(t, first, store, sessionID)
	first.Shutdown()

	// ── 进程重启：宿主这次拿不到模型读后感，之后的压缩必然失败 ──────────
	readback := restartCompactionRuntime(&compactionIndexRecorder{}, context_runtime.CompactionSummarySourceLocal)
	readback.readback.SummaryNote = readbackError
	engine := &fakeEngine{}
	second := newTestService(t, engine,
		withTestSessions(store),
		withTestRuntime(readback),
	)
	if err := second.ResumeSession(sessionID); err != nil {
		t.Fatalf("重启后 ResumeSession(%q): %v", sessionID, err)
	}
	// ① 重启前的成功记录必须已经读回来（否则下面的"新痕"与"旧记录"无从区分）。
	before := compactionRecordsOf(t, second)
	if len(before) != 1 || before[0].Failed {
		t.Fatalf("夹具前提：重启后应先读回那条成功记录：%+v", before)
	}
	// 会话继续跑：新一轮（自动路径，不是 /compact），上下文再次长到阈值之上。
	startRestartRound(t, second, sessionID, secondRequestID)
	marks := appendWindowRounds(t, second, secondRequestID, restartRefillWithinWindowRounds, 32_000)
	if _, err := second.components.context.PrepareExecutionContext(secondRequestID, "continue"); err != nil {
		t.Fatalf("装配上下文：%v（压缩失败不得中断会话）", err)
	}
	// 读数闸实测输入（重启到底剥夺了哪一步，靠这个实测而不是靠猜）：这次读数拿到的
	// 重放素材是"装配出来的那份历史"，不是空的——即失败不是"没素材"，是那次调用没成。
	t.Logf("读数闸输入：overflow=%d 条 replay=%d 条",
		len(readback.lastReadback.Overflow), len(readback.lastReadback.ReplayHistory))
	if len(readback.lastReadback.ReplayHistory) == 0 {
		t.Logf("注意：读数闸这次拿到的是**空**重放素材（重启后的现场形态之一）")
	}

	// ② 失败痕留下，且带读数闸的报错原文。
	records := compactionRecordsOf(t, second)
	failures := 0
	var failure ContextCompaction
	for _, record := range records {
		if record.Failed {
			failures++
			failure = record
		}
	}
	if failures != 1 {
		t.Fatalf("重启后越线的那次压缩应恰好留一条失败痕，实际 %d 条：%+v", failures, records)
	}
	if !strings.Contains(failure.Note, " error=") || !strings.Contains(failure.Note, "connect: connection refused") {
		t.Fatalf("失败痕的 note 必须带上读数闸的报错原文（err 也写进 note）：%q", failure.Note)
	}

	// ③ 同一条痕进快照可见面（右栏「上下文压缩」的唯一来路）。
	snapshot, err := second.SnapshotOf(sessionID)
	if err != nil {
		t.Fatalf("SnapshotOf(%q): %v", sessionID, err)
	}
	if snapshot.Task == nil || len(snapshot.Task.ContextCompactions) != 2 {
		t.Fatalf("快照应带 1 条成功记录 + 1 条失败痕：%+v", snapshot.Task)
	}
	visible := snapshot.Task.ContextCompactions[1]
	if !visible.Failed || !strings.Contains(visible.Note, " error=") {
		t.Fatalf("快照里的失败痕没带上报错原文（前端因此渲染不出这一行）：%+v", visible)
	}

	// ④ 上下文原样：尚未被任何压缩覆盖的轮次一轮不少。
	kept := retainedRounds(engine.History(), marks)
	if kept != len(marks) {
		t.Fatalf("压缩失败把上下文截断了：重启后新长出来的 %d 轮里只有 %d 轮还在 provider 历史里："+
			"这次什么都没压成，模型却已经看不见最早的一轮（%s）。", len(marks), kept, marks[0])
	}
}
