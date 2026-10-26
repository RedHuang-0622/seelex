package core

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// 本文件钉住"进草稿壳"三条路径的**差异矩阵**（M5-P18 的规整：字段清理收成
// `enterDraftShellLocked` + `draftShellOptions` 的显式开关）。
//
// 三条路径的角色（用户 2026-10-17 裁决时点明）：
//
//  1. `/new` 进草稿（`BeginNewSession`）——复用保留槽、恢复展示绑定与档位，清掉全部会话事实；
//  2. 冷加载中断时恢复草稿内容（`resetViewToDraftAfterRestoreFailure`）——**不持久化**，
//     故保留窗口读数（避免用空壳覆盖目标会话记录）；已读文件属于未加载成功的会话，不继承；
//  3. 卸载/删掉正在看的会话（`switchViewToFreshDraftLocked`）——就地换单元、不写槽位，
//     进程级交互保留、会话级交互随被卸下的会话失效。
//
// 2026-10-17 用户裁决：02 的 `ReadFiles`、03 的会话级 `Interaction` 两处漂移已收敛。
// 下面的期望值就是收敛后的语义，谁改语义谁改这张表。

// seedPreviousSessionFacts 在快照里铺满"上一个会话留下的会话事实"，供三条路径消费。
func seedPreviousSessionFacts(service *Service) {
	service.ViewMu.Lock()
	defer service.ViewMu.Unlock()
	// 冷启动装配期会留下一份草稿槽；这里清掉，让"写完槽位没有"成为可断言的差异。
	service.draft = nil
	service.Core.Snapshot.Task = &model.TaskState{Summary: "上一个会话的任务面"}
	service.Core.Snapshot.ReadFiles = []model.ReadFileRef{{Path: "上一会话读过的文件"}}
	service.Core.Snapshot.Runtime.Plan = &model.PlanState{Name: "上一个会话的 plan"}
	service.Core.Snapshot.Interaction = &model.Interaction{ID: "interaction-1", Kind: "approval"}
	service.Core.Snapshot.HistoryOffset = 7
	service.Core.Snapshot.TotalMessages = 21
	service.Core.Snapshot.HasMoreHistory = true
	service.Core.Snapshot.CurrentWorkspace = &model.WorkspaceInfo{ID: "ws-previous", RootPath: "G:/tmp/previous"}
	service.Core.Snapshot.Session.Composer = "上一个会话留在输入框里的草稿文本"
}

func TestBeginNewSessionDraftShellClearsEverySessionFact(t *testing.T) {
	engine := newMultiSessionEngine()
	service := newTestService(t, engine)
	defer frontendOfTestService(service).Shutdown()
	// `/new` 是幂等的：视图已在草稿上时直接返回。所以先落到一个真实会话上，
	// 再铺"上一个会话留下的会话事实"，这样 BeginNewSession 才走真正的清理路径。
	engine.register("sess-before-new")
	if err := frontendOfTestService(service).ResumeSession("sess-before-new"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	seedPreviousSessionFacts(service)

	if err := frontendOfTestService(service).BeginNewSession(); err != nil {
		t.Fatalf("BeginNewSession: %v", err)
	}

	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	snapshot := service.Core.Snapshot
	if snapshot.Task != nil || snapshot.ReadFiles != nil || snapshot.Runtime.Plan != nil || snapshot.Interaction != nil {
		t.Fatalf("会话事实必须清干净：Task=%v ReadFiles=%v Plan=%v Interaction=%v",
			snapshot.Task, snapshot.ReadFiles, snapshot.Runtime.Plan, snapshot.Interaction)
	}
	if snapshot.HistoryOffset != 0 || snapshot.TotalMessages != 0 || snapshot.HasMoreHistory {
		t.Fatalf("窗口读数必须归零：offset=%d total=%d hasMore=%v",
			snapshot.HistoryOffset, snapshot.TotalMessages, snapshot.HasMoreHistory)
	}
	if snapshot.CurrentWorkspace != nil {
		t.Fatalf("任务会话草稿必须真正未关联工作区，实际 %+v", snapshot.CurrentWorkspace)
	}
	if !snapshot.Session.Draft || snapshot.Session.ID == "" {
		t.Fatalf("草稿壳的会话格不对：%+v", snapshot.Session)
	}
	// `/new` 把草稿单元里的 composer 文本写回快照（03/02 不做这件事）。
	if snapshot.Session.Composer != "" {
		t.Fatalf("草稿单元的 composer 应为空，实际 %q", snapshot.Session.Composer)
	}
}

func TestColdRestoreFailureDraftShellKeepsWindowOnly(t *testing.T) {
	engine := newMultiSessionEngine()
	service := newTestService(t, engine)
	defer frontendOfTestService(service).Shutdown()
	seedPreviousSessionFacts(service)

	service.components.sessionRestoreDraftReset.Reset()

	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	snapshot := service.Core.Snapshot
	if snapshot.Task != nil || snapshot.Runtime.Plan != nil || snapshot.Interaction != nil {
		t.Fatalf("会话事实（Task/Plan/Interaction）必须清：Task=%v Plan=%v Interaction=%v",
			snapshot.Task, snapshot.Runtime.Plan, snapshot.Interaction)
	}
	// 这条路径**不持久化**（避免空壳覆盖目标会话记录），所以保留窗口读数。
	if snapshot.HistoryOffset != 7 || snapshot.TotalMessages != 21 || !snapshot.HasMoreHistory {
		t.Fatalf("窗口读数应保留（不持久化的降级路径）：offset=%d total=%d hasMore=%v",
			snapshot.HistoryOffset, snapshot.TotalMessages, snapshot.HasMoreHistory)
	}
	if snapshot.ReadFiles != nil {
		t.Fatalf("已读文件属于未加载成功的会话，草稿壳不得继承：%+v", snapshot.ReadFiles)
	}
	if snapshot.CurrentWorkspace == nil || snapshot.CurrentWorkspace.ID != "ws-previous" {
		t.Fatalf("工作区绑定不动：%+v", snapshot.CurrentWorkspace)
	}
	if !snapshot.Session.Draft || snapshot.Session.ID == "" {
		t.Fatalf("草稿壳的会话格不对：%+v", snapshot.Session)
	}
}

func TestUnloadViewDraftShellKeepsProcessInteractionAndWritesNoSlot(t *testing.T) {
	engine := newMultiSessionEngine()
	service := newTestService(t, engine)
	defer frontendOfTestService(service).Shutdown()
	seedPreviousSessionFacts(service)

	service.ViewMu.Lock()
	service.switchViewToFreshDraftLocked()
	service.ViewMu.Unlock()

	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	snapshot := service.Core.Snapshot
	if snapshot.Task != nil || snapshot.Runtime.Plan != nil || snapshot.ReadFiles != nil {
		t.Fatalf("会话事实（Task/Plan/ReadFiles）必须清：Task=%v Plan=%v ReadFiles=%v",
			snapshot.Task, snapshot.Runtime.Plan, snapshot.ReadFiles)
	}
	// 就地换单元：进程级交互（无会话归属）保留、不清窗口读数、不写槽位。
	if snapshot.Interaction == nil {
		t.Fatal("进程级交互（SessionID 为空）不随会话卸载失效，应保留")
	}
	if snapshot.HistoryOffset != 7 || snapshot.TotalMessages != 21 {
		t.Fatalf("窗口读数应保留：offset=%d total=%d", snapshot.HistoryOffset, snapshot.TotalMessages)
	}
	if service.draft != nil {
		t.Fatalf("就地草稿不写槽位（不进入会话树），实际槽位 %+v", service.draft)
	}
	draftID := snapshot.Session.ID
	if !snapshot.Session.Draft || draftID == "" {
		t.Fatalf("草稿壳的会话格不对：%+v", snapshot.Session)
	}
	if unit := service.sessions.Unit(draftID); unit == nil {
		t.Fatalf("视图单例必须有对应单元（nil 会让后续 Unit(viewID) 读成 nil）")
	}
}

// 会话级交互挂在被卸下的会话上：换单元后必须清掉，否则用户会对着一个已不存在的
// 会话确认审批。
func TestUnloadViewDraftShellDropsSessionScopedInteraction(t *testing.T) {
	engine := newMultiSessionEngine()
	service := newTestService(t, engine)
	defer frontendOfTestService(service).Shutdown()
	seedPreviousSessionFacts(service)

	service.ViewMu.Lock()
	service.Core.Snapshot.Interaction = &model.Interaction{
		ID: "interaction-gone", Kind: "approval", SessionID: "sess-unloaded",
	}
	service.switchViewToFreshDraftLocked()
	service.ViewMu.Unlock()

	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	if service.Core.Snapshot.Interaction != nil {
		t.Fatalf("会话级交互属于被卸下的会话，必须清：%+v", service.Core.Snapshot.Interaction)
	}
}
