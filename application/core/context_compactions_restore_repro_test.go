package core

// 冷恢复（进程重启）丢「上下文压缩栈」复现。
//
// 用户现象：应用重启后打开旧会话，右栏「上下文压缩」（压缩栈）整条不见了。
// 渲染数据来自快照的 `task.context_compactions`（= `model.TaskState.
// ContextCompactions`），运行期唯一事实源是 `TaskExecutionState.
// ContextCompactions`；而冷恢复（`resumeSession` 的 hasRecord 分支 →
// `RestoreSessionTaskState`）只装 planStack / transcript / checkpoint /
// toolResults——**会话上下文事实**（压缩记录、保留窗口起点、上下文版本）在这条
// 路上没有从 record 还原回来。
//
// 这是 2026-09-23 修过的「折叠之后的下一轮压缩不见所踪」
// （`context_compact_across_rounds_repro_test.go`）在**重启/冷恢复**上的孪生：
// 会话上下文事实属于会话、不属于回合，进程重启同样不该把它丢掉。
//
// 三条后果分别钉住（任一不成立即红）：
//  1. 驻留读面（`SnapshotOf` → `snapshotOfResident` → `VisibleTaskStateFor`）
//     拿不到压缩记录 → 右栏「上下文压缩」为空；
//  2. 重启后**下一次落盘**（`sessionRecordLocked` → `TaskStateFor`）用空记录
//     覆盖 `record.Execution.Task.ContextCompactions` → 磁盘上的压缩历史被
//     不可逆抹掉；
//  3. 保留窗口起点（`ContextRetainedFrom`）归零 → 下一次装配把已被折出的前缀
//     重新计入上下文预算。
//
// 用例真的跨过一次进程重启：同一份 store 上第一份 Service 跑出压缩记录并按
// 会话落盘 → Shutdown → 同一 store 上新建第二份 Service 冷加载
// （`ResumeSession`）。store 用 `archiveSessions`（record 的读写面），它按
// `SaveSessionRecordWorkspace` / `LoadSessionRecord` 承载 record，与生产
// sessionstore 的 record 通道同形。

import "testing"

const restartCompactionRequestID = "task-restart-compaction"

// compactedSessionStore 造一份「重启前」的持久面并结束进程：第一份 Service 在
// 会话里跑出 4 个已定稿轮次（上下文压力越软阈值）、显式折叠一次（记录进状态、
// 进快照、并按会话落盘），session 的事件流与 record 都留在 store 上。返回的
// retained 是折叠推前后的保留窗口起点（重启前的事实）。
func compactedSessionStore(t *testing.T) (*archiveSessions, string, ContextCompaction, int) {
	t.Helper()
	store := &archiveSessions{}
	first := newTestService(t, &fakeEngine{},
		withTestSessions(store),
		withTestRuntime(runtimeWithContextLimits{fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192}),
	)
	first.ViewMu.Lock()
	first.Core.Snapshot.Chat = ChatState{Running: true, RequestID: restartCompactionRequestID}
	first.components.tasks.BeginTask(restartCompactionRequestID, "inspect", "high", nil, TaskCheckpoint{})
	first.ViewMu.Unlock()
	sessionID := first.Snapshot().Session.ID
	if sessionID == "" {
		sessionID = first.components.tasks.SessionIDForRequest(restartCompactionRequestID)
	}
	appendWindowRounds(t, first, restartCompactionRequestID, 4, 32_000)

	// 折叠一次（`/compact`、`compact_context` 同一落点）。
	compacted := compactNow(t, first, sessionID)
	if compacted.EventTo == 0 {
		t.Fatalf("夹具前提：压缩记录应带被压区间（事件序号）：%+v", compacted)
	}
	first.ViewMu.RLock()
	state := first.components.tasks.CurrentTaskExecutionFor(sessionID)
	var retained int
	var transcript []TranscriptEvent
	if state != nil {
		retained = state.ContextRetainedFrom
		transcript = append([]TranscriptEvent(nil), first.components.tasks.TranscriptFor(sessionID)...)
	}
	first.ViewMu.RUnlock()
	if state == nil || len(state.ContextCompactions) != 1 {
		t.Fatalf("夹具前提：折叠后应有一条压缩记录：%#v", state)
	}
	if retained <= 0 {
		t.Fatalf("夹具前提：折叠应推前保留窗口起点（ContextRetainedFrom>0）")
	}
	if persisted := store.record.Execution.Task; persisted == nil || len(persisted.ContextCompactions) != 1 {
		t.Fatalf("夹具前提：重启前落盘的 record 应带压缩记录：%+v", persisted)
	}
	// 事件流是会话自己的 durable 事实：重启后按同一套事件序号读回（压缩记录里的
	// 区间序号属于这套空间）。
	store.transcript = transcript
	// 进程结束。
	first.Shutdown()
	return store, sessionID, compacted, retained
}

// coldResume 在 store 上新建 Service（进程重启）并冷加载会话。
func coldResume(t *testing.T, store *archiveSessions, sessionID string) *Service {
	t.Helper()
	service := newTestService(t, &fakeEngine{},
		withTestSessions(store),
		withTestRuntime(runtimeWithContextLimits{fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192}),
	)
	if err := service.ResumeSession(sessionID); err != nil {
		t.Fatalf("重启后 ResumeSession(%q): %v", sessionID, err)
	}
	return service
}

// TestReproCompactionStackSurvivesProcessRestart 应用重启后，旧会话的压缩栈
// 必须还在读面上、也必须还在盘上。
func TestReproCompactionStackSurvivesProcessRestart(t *testing.T) {
	store, sessionID, compacted, _ := compactedSessionStore(t)
	service := coldResume(t, store, sessionID)

	// 判据 1：驻留读面的任务面必须带压缩记录（右栏「上下文压缩」的唯一来路）。
	// 两条判据都想在同一轮里看清，所以这一条先报错不中断。
	snapshot, err := service.SnapshotOf(sessionID)
	if err != nil {
		t.Fatalf("SnapshotOf(%q): %v", sessionID, err)
	}
	switch {
	case snapshot.Task == nil || len(snapshot.Task.ContextCompactions) == 0:
		t.Errorf("红灯：重启冷加载后快照任务面丢了压缩记录（task=%+v）——"+
			"运行期唯一事实源 TaskExecutionState.ContextCompactions 在冷恢复时没有从 record.Execution.Task 还原，"+
			"前端右栏「上下文压缩」因此整条为空（用户现象）。", snapshot.Task)
	default:
		restored := snapshot.Task.ContextCompactions[0]
		if restored.Version != compacted.Version || restored.FrameRef != compacted.FrameRef ||
			restored.EventFrom != compacted.EventFrom || restored.EventTo != compacted.EventTo {
			t.Errorf("恢复出来的压缩记录不是落盘那一份：%+v, want %+v", restored, compacted)
		}
	}

	// 判据 2：重启后的下一次落盘不得把 record 里的压缩历史写成空
	// （sessionRecordLocked → TaskStateFor 走的是会话任务状态；状态空就抹盘）。
	location := service.components.sessions.LocateSession(sessionID)
	if err := service.components.sessions.PersistCurrentSession(location, sessionID); err != nil {
		t.Fatalf("重启后落盘: %v", err)
	}
	after, err := store.LoadSessionRecord(sessionID)
	if err != nil {
		t.Fatalf("读回落盘 record: %v", err)
	}
	if after.Execution.Task == nil || len(after.Execution.Task.ContextCompactions) == 0 {
		t.Fatalf("红灯：重启后的一次落盘把 record.Execution.Task.ContextCompactions 抹成空"+
			"（task=%+v）——磁盘上的压缩历史被不可逆覆盖，右栏此后永久为空。", after.Execution.Task)
	}
}

// TestReproCompactionStackVisibleWithoutProjection projection 缺失（老记录、
// 投影没落盘）**不等于**会话上下文事实缺失：压缩栈照旧要落回内存任务面，否则
// 右栏为空、下一次落盘还会把它抹掉。同时不得为冷加载会话伪造一个重启后已不复
// 存在的回合身份（RequestID 留空）。
func TestReproCompactionStackVisibleWithoutProjection(t *testing.T) {
	store, sessionID, compacted, _ := compactedSessionStore(t)
	store.record.Projection = nil
	service := coldResume(t, store, sessionID)

	snapshot, err := service.SnapshotOf(sessionID)
	if err != nil {
		t.Fatalf("SnapshotOf(%q): %v", sessionID, err)
	}
	if snapshot.Task == nil || len(snapshot.Task.ContextCompactions) == 0 {
		t.Fatalf("红灯：没有 projection 时压缩栈没有落回内存任务面（task=%+v）——"+
			"「有 projection 才有任务面」的旧口径把会话上下文事实一起丢了。", snapshot.Task)
	}
	if snapshot.Task.RequestID != "" {
		t.Fatalf("冷恢复不得伪造回合身份：RequestID=%q（want 空）", snapshot.Task.RequestID)
	}
	if restored := snapshot.Task.ContextCompactions[0]; restored.Version != compacted.Version || restored.FrameRef != compacted.FrameRef {
		t.Fatalf("恢复出来的压缩记录不是落盘那一份：%+v, want %+v", restored, compacted)
	}
}

// TestReproCompactionRetainedFromRestoredFromRecord 冷恢复时保留窗口起点
// （`ContextRetainedFrom`）按压缩记录的区间事实还原：它是"已被折出的前缀"的
// 边界，归零会让下一次装配重新计入这段前缀（长会话会稳定越线、每回合重新压
// 一次）。这里用**存储事件流**（seq 空间与折叠当时同一套）作为推导前提。
func TestReproCompactionRetainedFromRestoredFromRecord(t *testing.T) {
	store, sessionID, _, retained := compactedSessionStore(t)
	service := coldResume(t, store, sessionID)

	service.ViewMu.RLock()
	restored := service.components.tasks.CurrentTaskExecutionFor(sessionID)
	service.ViewMu.RUnlock()
	if restored == nil {
		t.Fatalf("冷恢复后应有会话上下文状态")
	}
	if restored.ContextRetainedFrom != retained {
		t.Fatalf("红灯：保留窗口起点没有按 record 的压缩区间还原：%d → %d（want %d）——"+
			"下一次装配会把已被折出的前缀重新计入上下文预算。",
			retained, restored.ContextRetainedFrom, retained)
	}
}
