package core

// 会话释放面（2026-10-14）：把"哪些会话的内存可以被释放、放掉哪些面"收在一处。
//
// 口径（用户口径，出厂默认）：
//   - **受保护集永不释放**：当前视图会话（切回必须瞬时）、待审批会话（外部事件
//     的等待方）、运行/排队/在飞会话（回合还没收尾）、正在恢复的会话。
//   - 其余「已加载过且已静默」的会话（典型：切走之后回合跑完的那个）按 LRU 释放，
//     **窗口默认 1** ⇒ 只保留上一个：切走一个不释放，再切走一个才释放最旧的。
//   - 窗口是配置项：`config/seelex.yaml` 的 `limits.session_release_window`
//     （见 limit.Limits.ReleaseWindow）；`resident_limit` /
//     `loaded_content_limit` 是**分层覆盖**的老口径（显式 >0 才生效）。
//
// 一次释放放掉的面（重到轻）：引擎实例（EnginePort）→ 桥侧会话本体
// （assemble.Runtime.bundles：框架 Session + 会话上下文存储 + 主历史）→
// 可见正文（View.Conversation）→ 使用序与 per-会话残格。**保留**会话单元、
// 目录行、标题、统计与磁盘事实：重开走冷加载（正文尾部窗口 + 上下文按盘重建）。
//
// 为什么不是"只丢引擎"：桥侧 bundles 只增不删（见 docs/2026-10-14-memory-cpu-accumulation/
// verdict.md §3 A1），只丢 EnginePort 的引用等于把"驱逐"做成空操作。

import (
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	session "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/application/session/switchpolicy"
)

// sessionReleaseMode 是一次释放的强度。
type sessionReleaseMode = applicationsession.ReleaseMode

const (
	// releaseIdle 是 LRU 释放：先把内存态 flush 到盘，再释放引擎/桥本体/正文；
	// 会话单元、标题与任务投影保留（重开走冷加载）。
	releaseIdle = applicationsession.ReleaseIdle
	// releaseFull 是卸载/归档/删除用的完整释放：在 releaseIdle 之上连会话单元、
	// 标题与任务投影一起移除（会话在内存里不留任何一格）。
	releaseFull = applicationsession.ReleaseFull
)

// protectedSessionLocked 报告目标会话是否属于**受保护集**（绝不释放；调用方持有
// Core.ViewMu）：当前视图、待审批、运行/排队/在飞、正在恢复。
//
// 这是释放面的唯一致判据——engine 层与 content 层都必须调它，不允许各自复制一份
// 条件（两层漂移会让"哪个会话被留着"变得不可解释）。
func (service *Service) protectedSessionLocked(sessionID string) bool {
	if service == nil || sessionID == "" {
		return true
	}
	return switching.Protected(switching.ProtectedInput{
		SessionID:     sessionID,
		ViewSessionID: service.Core.Snapshot.Session.ID,
		Restoring:     service.isRestoringLocked(sessionID),
		State:         sessionBusyState(service.sessions.Unit(sessionID)),
	})
}

// releaseSessionMemory 释放一个会话的内存占用（引擎 + 桥侧本体 + 正文 + 使用序，
// releaseFull 另加会话单元/标题/任务投影）。
//
// 调用方保证"挑中"时的守卫检查在 ViewMu 临界区内完成；这里再确认一次（挑选与释放
// 之间状态可能已变），确认不过就原样返回 nil（保持超限容忍，不做半途释放）。
// releaseSessionContentMemory 只释放可见正文（调用方已完成守卫与 flush）：会话
// 事实/元数据/标题/统计/窗口标志全部保留，磁盘是唯一事实源。
func (service *Service) releaseSessionContentMemory(sessionID string) {
	if service == nil || sessionID == "" {
		return
	}
	service.ViewMu.Lock()
	defer service.ViewMu.Unlock()
	if sessionID == service.Core.Snapshot.Session.ID || !service.sessionContentLoadedLocked(sessionID) {
		return
	}
	service.components.view.SessionViewMutateLocked(sessionID, func(view *session.View) {
		// 只卸载正文：窗口标志/统计/聊天运行态/文件引用保留，冷回读据此还原
		// 同一窗口位置（不重置 offset/total）。
		view.Conversation = nil
		view.ContentUnloaded = true
	})
	service.forgetContentLocked(sessionID)
}

// forgetResidentLocked 把会话移出驻留使用序并清驻留标记（调用方持有 Core.ViewMu）。
func (service *Service) forgetResidentLocked(sessionID string) {
	service.residentOrder = service.components.residency.Forget(service.residentOrder, sessionID)
	if unit := service.sessions.Unit(sessionID); unit != nil {
		unit.SetResident(false)
	}
}

// reconcileSessionRelease 跑一遍两层释放面（引擎层 + 内容层）。调用点：
// touch* 的收敛，以及会话**由忙转闲**的时刻（回合收尾、审批结案）——后者是
// "切走之后那个会话跑完了"的唯一收敛点：没有它，一个刚跑完的后台会话要等到
// 下一次 touch 才可能被收回。
func newServiceSessionReleaseApplication(service *Service) *applicationsession.ReleaseApplication {
	return applicationsession.NewReleaseApplication(applicationsession.ReleaseDeps{
		Protected: func(sessionID string) bool {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			return service.protectedSessionLocked(sessionID)
		},
		Persist: func(sessionID string) error {
			location := service.components.sessions.LocateSession(sessionID)
			return service.components.sessions.PersistCurrentSession(location, sessionID)
		},
		FlushToolRecord: service.flushToolRecord,
		UnloadEngine: func(sessionID string) error {
			if engine, ok := service.Deps.Engine.(residentEngine); ok {
				return engine.UnloadSession(sessionID)
			}
			return nil
		},
		ReleaseBundle: func(sessionID string) {
			if releaser, ok := service.Deps.Engine.(interface{ ReleaseSessionBundle(string) bool }); ok {
				releaser.ReleaseSessionBundle(sessionID)
			}
		},
		ForgetGoal: func(sessionID string) {
			service.components.goal.ForgetSession(sessionID)
		},
		ReleaseContent: service.releaseSessionContentMemory,
		ReleaseFull: func(sessionID string) {
			service.ViewMu.Lock()
			service.sessions.Remove(sessionID)
			service.components.tasks.DropPlanProjection(sessionID)
			service.components.tasks.UnloadSessionState(sessionID)
			service.components.sessions.UnloadSessionTitle(sessionID)
			service.ViewMu.Unlock()
		},
		ForgetOrders: func(sessionID string) {
			service.ViewMu.Lock()
			service.forgetResidentLocked(sessionID)
			service.forgetContentLocked(sessionID)
			service.ViewMu.Unlock()
		},
		ReleaseToolRecord: service.releaseToolRecordSession,
		RequestRefresh: func() {
			service.components.sessions.RequestCatalogRefresh()
		},
		ReconcileResident: service.reconcileResidentLimit,
		ReconcileContent: func() {
			service.reconcileLoadedContentLimit("")
		},
	})
}
