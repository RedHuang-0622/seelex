package core

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/task_context"
)

// 红灯复现（用户现象）：**压缩执行之后的下一轮对话，压缩记录已经不见所踪**。
//
// 用户形状（现场）：
//  1. 会话冷加载（打开一个早先的会话）后执行了一次压缩（`/compact`、自动达峰或
//     冷加载维护身份下的显式压缩）——状态页「上下文压缩」里有条目、对话区有
//     那条压缩分界；
//  2. 用户接着发下一条消息；
//  3. 条目与分界一起消失：`snapshot.task.context_compactions` 空了，保留窗口
//     起点（`ContextRetainedFrom`）退回 0，上下文版本退回 1。
//
// 根因：`continuationTaskExecutionState` 把**回合续接**判据
// （`IsContinuableStatus`）当成了**会话上下文事实**的门。回合收尾后状态是
// `completed`、冷加载维护身份结束后状态是 `idle`——两者都不在
// `IsContinuableStatus` 里，于是新回合直接返回一份全新状态：压缩记录、保留
// 窗口起点、上下文版本、checkpoint 全部丢弃。
//
// 危害不止「列表空白」：保留窗口起点归零意味着下一次装配把**已被折出的前缀**
// 重新计入上下文（`TaskExecutionState.ContextRetainedFrom` 的注释写明这条：
// 「否则每次装配都把已被折出的前缀重新计入，长会话会稳定越线、每回合重新压
// 一次」），于是出现「一发消息一条压缩记录」的抖动。
//
// 会话上下文事实属于**会话**（`session_context_maintenance.go` 的维护身份注释：
// 「下一次 BeginTask 照常开新回合，并把这份上下文状态当作上一份状态处理」），
// 不属于回合。本文件把这句话钉成判据。

// TestReproCompactionRecordSurvivesNextRoundAfterColdMaintenance 冷加载维护
// 身份（`StatusIdle`）下压缩一次之后，下一回合必须继承这份上下文事实。
func TestReproCompactionRecordSurvivesNextRoundAfterColdMaintenance(t *testing.T) {
	runtime := runtimeWithContextLimits{fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))
	sessionID := service.Snapshot().Session.ID
	appendWindowRounds(t, service, "task-cold-repro", 2, 400)

	// 1. 冷加载会话的显式压缩（与 TestCompactWithoutEpochKeepsExecutionFacesClean 同一入口）。
	if _, err := service.CompactContextNow(task_context.WithSessionID(context.Background(), sessionID)); err != nil {
		t.Fatalf("CompactContextNow: %v", err)
	}

	service.ViewMu.RLock()
	before := service.components.tasks.CurrentTaskExecutionFor(sessionID)
	snapshotBefore := service.Core.Snapshot.Task
	service.ViewMu.RUnlock()
	if before == nil || len(before.ContextCompactions) != 1 {
		t.Fatalf("压缩后应留下一条压缩记录：%#v", before)
	}
	if snapshotBefore == nil || len(snapshotBefore.ContextCompactions) != 1 {
		t.Fatalf("压缩记录应进快照可见面：%#v", snapshotBefore)
	}
	retained := before.ContextRetainedFrom
	version := before.ContextVersion
	if retained == 0 {
		t.Fatalf("压缩应推前保留窗口起点（ContextRetainedFrom>0）：%#v", before)
	}

	// 2. 下一轮对话：与 startChatFor 完全同形——取当前状态当 previous，再开新回合。
	service.ViewMu.Lock()
	previous := service.components.tasks.CurrentTaskExecutionFor(sessionID)
	service.components.tasks.BeginTaskFor(sessionID, "task-next-repro", "next request", "high", previous, TaskCheckpoint{})
	service.ViewMu.Unlock()

	// 3. 上下文事实必须随会话活下来。
	service.ViewMu.RLock()
	next := service.components.tasks.CurrentTaskExecutionFor(sessionID)
	snapshotNext := service.Core.Snapshot.Task
	service.ViewMu.RUnlock()
	if next == nil {
		t.Fatal("下一回合应有任务状态")
	}
	if len(next.ContextCompactions) != 1 {
		t.Fatalf("压缩记录在下一回合被丢掉：%d 条（want 1）——用户现象「压缩之后下一轮就不见了」", len(next.ContextCompactions))
	}
	if next.ContextRetainedFrom != retained {
		t.Fatalf("保留窗口起点被重置：%d → %d（want %d）——已被折出的前缀会被重新计入，每回合重新压一次",
			retained, next.ContextRetainedFrom, retained)
	}
	if next.ContextVersion != version {
		t.Fatalf("上下文版本被重置：%d → %d（want %d）", version, next.ContextVersion, version)
	}
	if snapshotNext == nil || len(snapshotNext.ContextCompactions) != 1 {
		t.Fatalf("快照可见面的压缩记录在下一回合消失（前端「上下文压缩」列表与压缩分界的唯一来路）：%#v", snapshotNext)
	}
}

// TestReproContextFactsSurviveCompletedTurnBoundary 回合**正常收尾**
// （`StatusCompleted`）之后开新回合，同样不得丢掉会话上下文事实。
//
// 现场形状：回合 1 达峰压缩 → 回合 1 收尾（completed）→ 用户发第二条消息。
// 若这里丢掉 `ContextRetainedFrom`，下一次装配就会从 transcript 头部重新累积，
// 稳定越过软阈值——「一发消息一条压缩记录」。
func TestReproContextFactsSurviveCompletedTurnBoundary(t *testing.T) {
	runtime := runtimeWithContextLimits{fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))
	sessionID := service.Snapshot().Session.ID
	appendWindowRounds(t, service, "task-completed-repro", 2, 400)

	if _, err := service.CompactContextNow(task_context.WithSessionID(context.Background(), sessionID)); err != nil {
		t.Fatalf("CompactContextNow: %v", err)
	}

	service.ViewMu.Lock()
	state := service.components.tasks.CurrentTaskExecutionFor(sessionID)
	if state == nil || len(state.ContextCompactions) != 1 {
		service.ViewMu.Unlock()
		t.Fatalf("夹具前提：压缩后应有一条压缩记录：%#v", state)
	}
	// 回合收尾（task_service 的终态落点）。
	state.Status = task_context.StatusCompleted
	retained, version := state.ContextRetainedFrom, state.ContextVersion
	service.ViewMu.Unlock()

	service.ViewMu.Lock()
	previous := service.components.tasks.CurrentTaskExecutionFor(sessionID)
	service.components.tasks.BeginTaskFor(sessionID, "task-after-completed", "next request", "high", previous, TaskCheckpoint{})
	service.ViewMu.Unlock()

	service.ViewMu.RLock()
	next := service.components.tasks.CurrentTaskExecutionFor(sessionID)
	service.ViewMu.RUnlock()
	if next == nil {
		t.Fatal("下一回合应有任务状态")
	}
	if len(next.ContextCompactions) != 1 {
		t.Fatalf("回合收尾边界丢掉压缩记录：%d 条（want 1）", len(next.ContextCompactions))
	}
	if next.ContextRetainedFrom != retained {
		t.Fatalf("回合收尾边界重置保留窗口起点：%d → %d（want %d）", retained, next.ContextRetainedFrom, retained)
	}
	if next.ContextVersion != version {
		t.Fatalf("回合收尾边界重置上下文版本：%d → %d（want %d）", version, next.ContextVersion, version)
	}
}
