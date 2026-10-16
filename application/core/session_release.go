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
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/middle/residency"
	"github.com/RedHuang-0622/seelex/middle/switch"
	"github.com/RedHuang-0622/seelex/session"
)

// sessionReleaseMode 是一次释放的强度。
type sessionReleaseMode int

const (
	// releaseIdle 是 LRU 释放：先把内存态 flush 到盘，再释放引擎/桥本体/正文；
	// 会话单元、标题与任务投影保留（重开走冷加载）。
	releaseIdle sessionReleaseMode = iota
	// releaseFull 是卸载/归档/删除用的完整释放：在 releaseIdle 之上连会话单元、
	// 标题与任务投影一起移除（会话在内存里不留任何一格）。
	releaseFull
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

// releaseBudget 把"本层能留几个"折算成一个数字。
//
//   - legacyLimit > 0：**分层覆盖（老口径）**——上限是"该层驻留会话总数（含受保护
//     会话）"，因此可释放的额度 = 上限 − 受保护数（下限 0）。
//   - legacyLimit <= 0：**统一窗口（默认口径）**——受保护集之外保留窗口个。
func releaseBudget(legacyLimit int, protectedCount int) int {
	return residency.Budget(legacyLimit, protectedCount, Limits().ReleaseWindow())
}

// releaseSessionMemory 释放一个会话的内存占用（引擎 + 桥侧本体 + 正文 + 使用序，
// releaseFull 另加会话单元/标题/任务投影）。
//
// 调用方保证"挑中"时的守卫检查在 ViewMu 临界区内完成；这里再确认一次（挑选与释放
// 之间状态可能已变），确认不过就原样返回 nil（保持超限容忍，不做半途释放）。
func (service *Service) releaseSessionMemory(sessionID string, mode sessionReleaseMode) error {
	if service == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}

	// 1) 二次确认：releaseIdle（LRU）必须再过一次受保护集——挑选与释放之间
	//    状态可能已变（切成视图 / 起了回合 / 等待审批 / 进入恢复）。
	//    releaseFull 的调用方（卸载 / 归档 / 删除）已经在自己的守卫里确认过
	//    （运行中会报 ErrChatRunning），且**卸载当前视图会话是合法入口**，
	//    因此不在这里用受保护集拦住它。
	if mode == releaseIdle {
		service.ViewMu.RLock()
		protected := service.protectedSessionLocked(sessionID)
		service.ViewMu.RUnlock()
		if protected {
			return nil
		}
	}

	// 2) flush（释放前必须落盘）：内存窗口里可能还有尚未发布的收尾消息。
	//    失败就不释放（数据不丢；下一轮 touch/收尾再试）。
	if mode == releaseIdle {
		location := service.components.sessions.LocateSession(sessionID)
		if err := service.components.sessions.PersistCurrentSession(location, sessionID); err != nil {
			return fmt.Errorf("flush session %q before release: %w", sessionID, err)
		}
	}

	// 3) 引擎实例：先放引擎（可失败：会话在飞时端口会拒绝）。
	if engine, ok := service.Deps.Engine.(residentEngine); ok {
		if err := engine.UnloadSession(sessionID); err != nil {
			return fmt.Errorf("unload engine %q: %w", sessionID, err)
		}
	}

	// 4) 桥侧会话本体 + per-会话残格（可选能力：legacy 引擎没有这一格）。
	if releaser, ok := service.Deps.Engine.(interface{ ReleaseSessionBundle(string) bool }); ok {
		releaser.ReleaseSessionBundle(sessionID)
	}

	// 5) 下游按会话缓存了"释放面句柄"的格子一起清：goal 协调器把 StoreFor 解析
	//    出来的上下文存储适配器按会话缓存（第五栈持久化句柄）。本体重开时会绑新的
	//    store，旧句柄留着 = 同一会话两个 store 同时写盘（存储层单写者纪律失效）。
	service.components.goal.ForgetSession(sessionID)

	// 6) 可见正文（只丢内存副本；窗口标志/统计/文件引用保留，冷回读据此还原）。
	service.releaseSessionContentMemory(sessionID)

	// 7) 完整释放：会话单元 / 标题 / 任务投影一起移除。
	//
	// 移除会话单元是**卸载/删除的既有契约**（重开走 cold_load 重建单元），因此这里
	// 无条件移除。"卸载/删掉正在看的那个会话"由调用方在自己的临界区里收口——卸载与
	// 删除都在**移除之后**把视图切到一个新建草稿单元（见
	// switchViewToFreshDraftLocked），视图单例因此始终有对应单元。
	if mode == releaseFull {
		service.ViewMu.Lock()
		service.sessions.Remove(sessionID)
		service.components.tasks.DropPlanProjection(sessionID)
		service.components.tasks.UnloadSessionState(sessionID)
		service.components.sessions.UnloadSessionTitle(sessionID)
		service.ViewMu.Unlock()
	}

	// 8) 使用序记账：移出两层序（重开时经 touch 重新入序为最近使用）。
	service.ViewMu.Lock()
	service.forgetResidentLocked(sessionID)
	service.forgetContentLocked(sessionID)
	service.ViewMu.Unlock()
	service.components.sessions.RequestCatalogRefresh()
	return nil
}

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
	service.residentOrder = service.residentOrder.Forget(sessionID)
	if unit := service.sessions.Unit(sessionID); unit != nil {
		unit.SetResident(false)
	}
}

// reconcileSessionRelease 跑一遍两层释放面（引擎层 + 内容层）。调用点：
// touch* 的收敛，以及会话**由忙转闲**的时刻（回合收尾、审批结案）——后者是
// "切走之后那个会话跑完了"的唯一收敛点：没有它，一个刚跑完的后台会话要等到
// 下一次 touch 才可能被收回。
func (service *Service) reconcileSessionRelease() {
	service.reconcileResidentLimit()
	service.reconcileLoadedContentLimit("")
}
