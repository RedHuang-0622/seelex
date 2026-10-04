package core

// 压缩记录跨重启：从「只活在 record 通道」改到「住本会话自己的压缩记录通道」。
//
// 现场（docs/research/2026-10-02-compaction-record-and-failure-trace-survey.md §4.3
// 的真布局实测）：现行存储布局（v8/S20）下 `record.Execution.Task.ContextCompactions`
// **写不进也读不回** —— `SaveRecordRaw` 只把 status/title 写穿，`LoadRecordRaw` 交回
// 按 message head 派生的最小 record。于是重启后「压缩帧在（compact 通道）、压缩记录
// 不在」：右栏「上下文压缩」整条为空、保留窗口起点归零。
//
// 本文件的替身把这条实测事实写进形状里：record 通道停写 `execution`（置 nil），压缩
// 记录改由 `LoadCompactionRecordsWorkspace`（= 生产 sessionstore 的
// compaction-records.jsonl 通道）承载。改掉读侧之前，下面两条用例都是红的。

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
)

// retiredRecordChannelSessions 是现行存储布局（v8/S20）下的会话端口替身。
type retiredRecordChannelSessions struct {
	archiveSessions
	// facts 是压缩记录通道（按写入顺序；生产为 append-only 的
	// compaction-records.jsonl + metadata/compaction.json 水位）。
	facts []ContextCompaction
}

func (sessions *retiredRecordChannelSessions) SaveSessionRecord(_ string, record SessionRecord) error {
	return sessions.persist(record)
}

func (sessions *retiredRecordChannelSessions) SaveSessionRecordWorkspace(_, _ string, record SessionRecord) error {
	return sessions.persist(record)
}

// persist 模拟 v8 的落盘：record 通道收下 status/title/对话，`execution` 子树丢弃
// （这正是"往 SessionRecord 加字段既写不进去也读不回来"的那一半），压缩记录改走
// 压缩记录通道。
func (sessions *retiredRecordChannelSessions) persist(record SessionRecord) error {
	if record.Execution.Task != nil {
		sessions.facts = append([]ContextCompaction(nil), record.Execution.Task.ContextCompactions...)
		record.Execution.Task = nil
	}
	sessions.record = record
	return nil
}

func (sessions *retiredRecordChannelSessions) LoadCompactionRecordsWorkspace(_, _ string) ([]ContextCompaction, error) {
	return append([]ContextCompaction(nil), sessions.facts...), nil
}

// restartCompactionRuntime 造一个 200k 窗口的宿主；readback 决定这次压缩能不能
// 拿到模型读后感（replay = 有；local = 没有 → 压缩失败）。
func restartCompactionRuntime(recorder *compactionIndexRecorder, source string) *compactionReadbackRuntime {
	return &compactionReadbackRuntime{
		compactionIndexRuntime: compactionIndexRuntime{
			runtimeWithContextLimits: runtimeWithContextLimits{fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192},
			recorder:                 recorder,
		},
		readback: context_runtime.CompactionIndexReceipt{
			Summary:       "## 压缩内容 (Compacted Context)\n### 目标 (Goal)\n模型读后感",
			SummarySource: source,
		},
	}
}

// startRestartSession 起一个任务执行中的会话并追加 rounds 个已定稿轮次
// （每轮约 8000 tokens：32,000 ASCII 字符 ≈ 8,000 tokens）。
func startRestartSession(t *testing.T, store *retiredRecordChannelSessions, runtime RuntimePort, requestID string, rounds int) (*Service, string) {
	t.Helper()
	service := newTestService(t, &fakeEngine{}, withTestSessions(store), withTestRuntime(runtime))
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: requestID}
	service.components.tasks.BeginTask(requestID, "inspect", "high", nil, TaskCheckpoint{})
	service.ViewMu.Unlock()
	sessionID := service.Snapshot().Session.ID
	appendWindowRounds(t, service, requestID, rounds, 32_000)
	return service, sessionID
}

// persistRestartSession 按会话落盘（事件流是会话自己的 durable 事实，重启后按同一
// 套事件序号读回——压缩记录里的区间序号属于那套空间）。
func persistRestartSession(t *testing.T, service *Service, store *retiredRecordChannelSessions, sessionID string) {
	t.Helper()
	service.ViewMu.RLock()
	transcript := append([]TranscriptEvent(nil), service.components.tasks.TranscriptFor(sessionID)...)
	service.ViewMu.RUnlock()
	store.transcript = transcript
	if err := service.components.sessions.PersistCurrentSession(
		service.components.sessions.LocateSession(sessionID), sessionID); err != nil {
		t.Fatalf("落盘会话: %v", err)
	}
}

// TestReproCompactionFactsSurviveProcessRestart 重启后压缩栈必须从存储读回来：
// 记录进快照可见面（右栏唯一来路），保留窗口起点按记录里的区间还原。
func TestReproCompactionFactsSurviveProcessRestart(t *testing.T) {
	const requestID = "task-restart-compaction-facts"
	store := &retiredRecordChannelSessions{}
	recorder := &compactionIndexRecorder{receipt: context_runtime.CompactionIndexReceipt{
		SegmentID:     "compact-restart",
		Summary:       "## 压缩内容 (Compacted Context)\n### 目标 (Goal)\n模型读后感",
		SummarySource: context_runtime.CompactionSummarySourceReplay,
	}}
	first, sessionID := startRestartSession(t, store,
		restartCompactionRuntime(recorder, context_runtime.CompactionSummarySourceReplay),
		requestID, 30)
	compacted := compactNow(t, first, sessionID)
	if compacted.EventTo == 0 {
		t.Fatalf("夹具前提：压缩记录应带被压区间（事件序号）：%+v", compacted)
	}
	first.ViewMu.RLock()
	state := first.components.tasks.CurrentTaskExecutionFor(sessionID)
	retained := 0
	if state != nil {
		retained = state.ContextRetainedFrom
	}
	first.ViewMu.RUnlock()
	if retained <= 0 {
		t.Fatalf("夹具前提：压缩应推前保留窗口起点（ContextRetainedFrom>0）")
	}
	persistRestartSession(t, first, store, sessionID)
	if store.record.Execution.Task != nil {
		t.Fatalf("夹具前提：v8 的 record 通道必须停写 execution（否则这条用例测的不是真布局）")
	}
	if len(store.facts) != 1 {
		t.Fatalf("夹具前提：压缩记录应落进压缩记录通道：%+v", store.facts)
	}
	first.Shutdown()

	// ── 进程重启 ─────────────────────────────────────────────────────────
	second := newTestService(t, &fakeEngine{},
		withTestSessions(store),
		withTestRuntime(restartCompactionRuntime(&compactionIndexRecorder{}, context_runtime.CompactionSummarySourceLocal)),
	)
	if err := second.ResumeSession(sessionID); err != nil {
		t.Fatalf("重启后 ResumeSession(%q): %v", sessionID, err)
	}

	snapshot, err := second.SnapshotOf(sessionID)
	if err != nil {
		t.Fatalf("SnapshotOf(%q): %v", sessionID, err)
	}
	if snapshot.Task == nil || len(snapshot.Task.ContextCompactions) == 0 {
		t.Fatalf("红灯：重启后快照任务面丢了压缩记录（task=%+v）——"+
			"前端右栏「上下文压缩」整条为空、轨迹「压缩」轨没有刻度、对话区不画压缩分界。", snapshot.Task)
	}
	restored := snapshot.Task.ContextCompactions[0]
	if restored.Version != compacted.Version || restored.EventFrom != compacted.EventFrom ||
		restored.EventTo != compacted.EventTo || restored.MessageFrom != compacted.MessageFrom {
		t.Fatalf("恢复出来的压缩记录不是落盘那一份：%+v, want %+v", restored, compacted)
	}
	second.ViewMu.RLock()
	restoredState := second.components.tasks.CurrentTaskExecutionFor(sessionID)
	second.ViewMu.RUnlock()
	if restoredState == nil {
		t.Fatalf("冷恢复后应有会话上下文状态")
	}
	if restoredState.ContextRetainedFrom != retained {
		t.Fatalf("红灯：保留窗口起点没有还原：%d → %d（want %d）——"+
			"下一次装配会把已被压出的前缀重新计入上下文预算。",
			retained, restoredState.ContextRetainedFrom, retained)
	}

	// 重启后的一次落盘不得把压缩记录抹掉，也不得重复追加同一条。
	persistRestartSession(t, second, store, sessionID)
	if len(store.facts) != 1 {
		t.Fatalf("重启后的一次落盘把压缩记录写成 %d 条（want 1）：%+v", len(store.facts), store.facts)
	}
}

// TestReproCompactionFailureTraceSurvivesProcessRestart 压缩失败痕同样要跨重启：
// 它是「某次没压成」的唯一持久证据（瞬态进度条 6 秒后自动撤条）。
func TestReproCompactionFailureTraceSurvivesProcessRestart(t *testing.T) {
	const requestID = "task-restart-compaction-failure"
	store := &retiredRecordChannelSessions{}
	// readback=local → 拿不到模型读后感 → 这次压缩失败，只留一条失败痕。
	first, sessionID := startRestartSession(t, store,
		restartCompactionRuntime(&compactionIndexRecorder{}, context_runtime.CompactionSummarySourceLocal),
		requestID, 4)
	result, err := first.components.context.CompactContextNow(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("压缩失败不得中断会话: %v", err)
	}
	if result.Recorded || result.Failure == "" {
		t.Fatalf("夹具前提：这次压缩应当是失败的（只留痕、不落压缩记录）：%+v", result)
	}
	persistRestartSession(t, first, store, sessionID)
	if len(store.facts) != 1 || !store.facts[0].Failed {
		t.Fatalf("夹具前提：失败痕应落进压缩记录通道：%+v", store.facts)
	}
	first.Shutdown()

	second := newTestService(t, &fakeEngine{},
		withTestSessions(store),
		withTestRuntime(restartCompactionRuntime(&compactionIndexRecorder{}, context_runtime.CompactionSummarySourceLocal)),
	)
	if err := second.ResumeSession(sessionID); err != nil {
		t.Fatalf("重启后 ResumeSession(%q): %v", sessionID, err)
	}
	snapshot, err := second.SnapshotOf(sessionID)
	if err != nil {
		t.Fatalf("SnapshotOf(%q): %v", sessionID, err)
	}
	if snapshot.Task == nil || len(snapshot.Task.ContextCompactions) != 1 {
		t.Fatalf("红灯：重启后失败痕没从存储读回来（task=%+v）——"+
			"用户再想确认「刚才那次到底压没压」就查无实据。", snapshot.Task)
	}
	failure := snapshot.Task.ContextCompactions[0]
	if !failure.Failed || failure.Note == "" {
		t.Fatalf("恢复出来的失败痕必须仍是失败痕（Failed=true + 带原因）：%+v", failure)
	}
	if failure.EventFrom != 0 || failure.EventTo != 0 || failure.FrameRef != "" {
		t.Fatalf("失败痕不得带区间/帧（带上了就会被读成「已压出窗口」）：%+v", failure)
	}
}
