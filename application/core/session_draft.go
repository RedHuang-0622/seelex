package core

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
)

const draftSessionName = "新会话"

// newDraftSessionIDLocked 生成早分配的草稿会话 ID（调用方持有 Core.ViewMu）。
// 草稿 ID 使用独立前缀与序号：Windows 时间戳低分辨率下同一 tick 多次
// BeginNewSession 也不会碰撞；引擎按该显式 ID 建 bundle（HasSession=false
// 阶段不建，首次提交物化时经 ActivateSession 创建）。
func (service *Service) newDraftSessionIDLocked() string {
	service.draftSeq++
	return fmt.Sprintf("draft_%d_%d", time.Now().UnixNano(), service.draftSeq)
}

// BeginNewSession 进入幂等的草稿状态：早分配真实会话 ID 并建 SessionUnit
// （HasSession=false，不建引擎 bundle、不写空历史），引擎会话只在第一条
// 真实 conversation 请求发出时创建。草稿槽位携带 ID 与工作区绑定，切换
// 会话后仍可恢复；首次提交（materializeDraftSession）时消费并清空。
func (service *Service) BeginNewSession() error {
	transition := service.transitionView()
	transition.Lock()
	defer transition.Unlock()

	service.ViewMu.RLock()
	closed := service.closed
	draining := service.draining
	draft := service.Core.Snapshot.Session.Draft
	sessionID := service.Core.Snapshot.Session.ID
	currentRunning := false
	if unit := service.sessions.Unit(sessionID); unit != nil {
		currentRunning = unit.ChatState().Running
	}
	currentWorkspaceID := session_runtime.WorkspaceID(service.Core.Snapshot.CurrentWorkspace)
	service.ViewMu.RUnlock()
	if closed {
		return errors.New("application is shut down")
	}
	if draining {
		return ErrApplicationDraining
	}
	if draft {
		return nil
	}

	if !currentRunning && len(service.engineHistoryFor(sessionID)) > 0 {
		service.setWorkspaceWriteScope(currentWorkspaceID)
		location := service.components.sessions.LocateSession(sessionID)
		if err := service.components.sessions.PersistCurrentSession(location, sessionID); err != nil {
			return fmt.Errorf("save current session before drafting a new one: %w", err)
		}
	}
	// 离开当前会话：清空**该会话自己的**引擎历史（历史已持久化；会话引擎缓存按需
	// 由 ResumeSession 重建），防止 draft 状态串入旧会话内容。禁止经进程级活跃
	// 别名清空：别名指向哪个会话不可预期，可能是另一个正在跑的会话（其 framework
	// Session 锁被 ChatStream 全程持有，清空会排在它后面并清掉它的运行历史）。
	if !currentRunning {
		service.clearEngineHistoryFor(sessionID)
	}
	service.promptStack.ClearKind("skill")
	// 离开当前会话：解绑 context 模块，防止四栈串到新会话。
	if store, ok := service.Deps.Sessions.(session_runtime.SessionContextPort); ok {
		store.DetachSessionContext()
	}
	// 新会话是「任务会话」——必须真正未关联工作区：清空上一个会话继承的
	// 项目绑定（CurrentWorkspace / Runtime project root / session store
	// workspace），防止上个对话的项目信息（项目地址、资源管理器文件树与
	// 提交记录、工作台投影）污染新会话。旧会话的 workspace binding 保留
	// （上面已按 currentWorkspaceID 持久化，会话树仍归入原工作区分组）。
	// 需要项目上下文的「工作区会话」由调用方在草稿上显式 BindWorkspace，
	// 再在首次请求物化时绑定。
	// 草稿槽位：恢复已保留的草稿（含早分配 ID 与工作区绑定）或新建并
	// 早分配真实 SID。
	service.ViewMu.Lock()
	slot := service.draft
	if slot == nil {
		slot = &draftSlot{ID: service.newDraftSessionIDLocked(), CreatedAt: time.Now()}
		service.draft = slot
	}
	slot.UpdatedAt = time.Now()
	draftID := slot.ID
	var restoredWorkspace *WorkspaceInfo
	if slot.Workspace != nil {
		item := *slot.Workspace
		restoredWorkspace = &item
	}
	service.ViewMu.Unlock()

	if restoredWorkspace == nil {
		// 任务会话草稿：必须真正未关联工作区（清空上个会话继承的项目绑定）。
		if service.Deps.Runtime != nil {
			service.unbindGlobalProjectRoot()
		}
		service.setWorkspaceWriteScope("")
	}
	// 恢复"工作区会话"草稿：只恢复展示绑定，不在此处切换全局工程根 / store
	// 写作用域（若其它会话运行中，切换会串写；首次提交物化时再绑定）。

	service.ViewMu.Lock()
	service.nextViewEpochLocked()
	service.Core.Snapshot.Session = SessionState{ID: draftID, Name: draftSessionName, Draft: true, Status: SessionStatusDraft}
	service.sessions.SetActive(draftID)
	service.Core.Snapshot.CurrentWorkspace = restoredWorkspace
	service.Core.Snapshot.Conversation = nil
	service.Core.Snapshot.HistoryOffset = 0
	service.Core.Snapshot.TotalMessages = 0
	service.Core.Snapshot.HasMoreHistory = false
	service.Core.Snapshot.Runtime.Plan = nil
	service.Core.Snapshot.Interaction = nil
	draftRuntime := service.sessionUnitLocked(draftID)
	draftRuntime.SetChatState(ChatState{}, nil)
	draftRuntime.SetCancel(nil)
	draftRuntime.SetRequests(nil)
	service.Core.Snapshot.Chat = draftRuntime.ChatState()
	service.Core.Snapshot.Session.Composer = draftRuntime.ComposerText()
	service.components.sessions.SetSessionTitleLocked(draftID, SessionTitle{})
	service.components.tasks.ResetForNewSessionLocked()
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	service.publishRuntimeProjections()
	// 会话切换：换当前会话指针并清空子代理树。工作表格是项目/全局台账
	// （TaskSnapshot = 注册表 + 全部分区），不在此清空——/new 只切换指针，
	// 先前会话的条目仍留在表里。
	service.Deps.Runtime.SwitchSessionTasks(draftID, nil)
	_ = service.Deps.Runtime.ClearSubagentTree()
	service.refreshWorkTableFromSources()
	service.publishSessionEvent(EventSnapshotChanged, revision, "", draftID, nil)
	return nil
}

// materializeDraftSession 为首条请求创建引擎会话与项目绑定：复用早分配
// 的草稿 SID（支持显式 ID 建引擎的宿主经 ActivateSession 创建，旧单会话
// 引擎退化为 StartSession 自动分配），并清空已提交的 composer 草稿。
// ambient 提交（submitConversation）与显式提交（materializeDraftForSubmit）
// 两条路径共用它。
// 调用方必须持有 sessionTransitionMu。
func (service *Service) materializeDraftSession(firstQuestion string) error {
	service.ViewMu.RLock()
	draft := service.Core.Snapshot.Session.Draft
	draftID := service.Core.Snapshot.Session.ID
	if service.draft != nil && service.draft.ID != "" {
		draftID = service.draft.ID
	}
	var workspace *WorkspaceInfo
	if service.Core.Snapshot.CurrentWorkspace != nil {
		item := *service.Core.Snapshot.CurrentWorkspace
		workspace = &item
	}
	service.ViewMu.RUnlock()
	if !draft {
		return nil
	}
	if draftID == "" {
		return errors.New("draft session has no pre-assigned session ID")
	}

	if workspace != nil {
		if err := service.bindGlobalProjectRoot(workspace.RootPath); err != nil {
			return fmt.Errorf("bind project root for new session: %w", err)
		}
		service.setWorkspaceWriteScope(workspace.ID)
	} else {
		service.unbindGlobalProjectRoot()
		service.setWorkspaceWriteScope("")
	}
	newID := draftID
	if activator, ok := service.Deps.Engine.(interface{ ActivateSession(string) error }); ok {
		// 会话路由宿主：按早分配 SID 显式创建引擎 bundle（草稿阶段
		// HasSession=false，此刻才建）。
		if err := activator.ActivateSession(newID); err != nil {
			return fmt.Errorf("create engine session %q: %w", newID, err)
		}
	} else {
		newID = strings.TrimSpace(service.Deps.Engine.StartSession())
		if newID == "" {
			return errors.New("engine returned an empty session ID")
		}
	}
	// framework DurableHistory 按会话 workspace 显式键落盘（R3 键漂移收敛）。
	if workspace != nil {
		service.Deps.Runtime.SetSessionWorkspace(newID, workspace.ID)
	} else {
		service.Deps.Runtime.SetSessionWorkspace(newID, "")
	}
	// 新会话无既有 context：保持解绑（Runtime 退回内存态，与 draft 一致）。
	if store, ok := service.Deps.Sessions.(session_runtime.SessionContextPort); ok {
		store.DetachSessionContext()
	}
	service.Deps.Engine.SetSystemPrompt(service.promptStack.Render())
	if workspace != nil && service.Deps.Workspace != nil {
		service.Deps.Workspace.BindSession(newID, workspace.ID)
	}
	workspaceProjection := service.collectWorkspaceProjection()

	service.ViewMu.Lock()
	service.draft = nil // 草稿已物化为真实会话，消费槽位
	title := SessionTitle{Value: session_runtime.SessionTitle(firstQuestion), Source: "first_request", FinalizedAt: time.Now()}
	service.Core.Snapshot.Session = SessionState{ID: newID, Name: title.Value}
	// 视图单例一致性：V 的唯一镜像随物化切到新会话（与 hot_attach/resume 同
	// 步路径），否则流式增量会按 ActiveID="" 误判为后台，Snapshot 收不到增量。
	service.sessions.SetActive(newID)
	service.components.sessions.SetSessionTitleLocked(newID, title)
	service.components.tasks.ResetPlanStateLocked()
	service.applyWorkspaceProjectionLocked(workspaceProjection)
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	service.publishRuntimeProjections()
	clearProjectID := ""
	if workspace != nil {
		clearProjectID = workspace.ID
	}
	service.clearComposerDraft(newID, clearProjectID)
	service.publishSessionEvent(EventSnapshotChanged, revision, "", newID, nil)
	service.components.sessions.RequestCatalogRefresh()
	// G6 驻留 LRU：物化完成（引擎 bundle 已建）即记录使用序并收敛超限。
	service.touchResident(newID)
	return nil
}

// isUnmaterializedDraftTarget 预判显式提交的目标是否就是那份尚未物化的草稿
// （视图里的草稿，或切换走后仍留在草稿槽里的草稿）。仅作廉价闸门用，避免每次
// 后台提交都去抢视图过渡锁；归属复判在 materializeDraftForSubmit 锁内再做。
func (service *Service) isUnmaterializedDraftTarget(sessionID string) bool {
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	if service.draft != nil && service.draft.ID == sessionID {
		return true
	}
	return service.Core.Snapshot.Session.Draft && service.Core.Snapshot.Session.ID == sessionID
}

// materializeDraftForSubmit 把「显式提交的目标恰好是未物化的草稿」接回物化路径。
//
// 存在理由：前端普通输入一律走 SubmitToSession + 显式视图会话 ID（含草稿的早分配
// SID，见 gui/frontend/dist/composer-input.js），而草稿从来没有可冷回读的历史——
// 不先物化，SubmitToSession 会按"目标未加载"去 ActivateSession→冷加载那份
// Status=draft 的 record：新会话以「已恢复会话: draft_…」开头、草稿槽位不消费、
// 草稿 record 不清理（重启后已发送的正文又回到输入框），有会话运行中时还要先经过
// restoring 空壳与延后提交。物化是草稿首条提交的唯一正解。
//
// 归属：草稿槽是进程单例，物化会把共享视图镜像切到该 SID，因此只有视图仍停在这份
// 草稿上时才允许；否则返回 ErrDraftNotInView（渲染层拿着过期快照提交，明确失败
// 比把输入投给别的会话安全）。调用方不得持有视图过渡锁。
func (service *Service) materializeDraftForSubmit(sessionID, firstInput string) error {
	if !service.isUnmaterializedDraftTarget(sessionID) {
		return nil
	}
	transition := service.transitionView()
	transition.Lock()
	defer transition.Unlock()
	service.ViewMu.RLock()
	mine := service.Core.Snapshot.Session.Draft &&
		service.Core.Snapshot.Session.ID == sessionID &&
		(service.draft == nil || service.draft.ID == sessionID)
	service.ViewMu.RUnlock()
	if !mine {
		return ErrDraftNotInView
	}
	return service.materializeDraftSession(strings.TrimSpace(firstInput))
}
