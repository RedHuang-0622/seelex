package core

import (
	"errors"
	"fmt"
	"github.com/RedHuang-0622/seelex/backend/engine/session"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/middle/switch"
	"github.com/RedHuang-0622/seelex/middle/switch/sessionusecase"
	"strings"
	"time"
)

const draftSessionName = switching.DraftSessionName

// newDraftSessionIDLocked 生成早分配的会话 ID（调用方持有 Core.ViewMu）。
// 口径只有一份：`middle/switch.NewDraftSessionID`。
// 序号自增也在该锁内，因此同一 tick 多次进入草稿不会撞号。
//
// 该 ID 是**真实会话 ID**而不是临时号：引擎按它建 bundle（HasSession=false 阶段不建，
// 首次提交物化时经 ActivateSession 创建）。
func (service *Service) newDraftSessionIDLocked() string {
	service.draftSeq++
	return switching.NewDraftSessionID(time.Now(), service.draftSeq)
}

// BeginNewSession 进入幂等的草稿状态：早分配真实会话 ID 并建 SessionUnit
// （HasSession=false，不建引擎 bundle、不写空历史），引擎会话只在第一条
// 真实 conversation 请求发出时创建。草稿槽位携带 ID 与工作区绑定，切换
// 会话后仍可恢复；首次提交（materializeDraftSession）时消费并清空。
//
// 同时清掉上一个会话留在共享快照里的**会话事实**（`Task` / `ReadFiles`）：
// 换视图指针就是换会话，这两个字段是宿主侧镜像（`Task` 上挂着压缩记录
// `ContextCompactions`），漏清会让新会话显示、并在首次提交时按会话落盘成
// 上一个会话的任务面与已读文件。口径与 unloadSession 的同名字段清理一致。
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
	// 离开会话：解绑 context 模块，防止四栈串到下一个会话。**只在被离开的会话
	// 空闲时**做（2026-10-04）：`DetachSessionContext()` 解绑的是**当前活跃
	// bundle**，而会话还在跑时它的回合会继续装配 provider 上下文——那一步遇到
	// 装配层压缩就会发现自己"未绑定"，PushCompactionFrame 报
	// "会话上下文存储未绑定（压缩栈不可用）"，与新建会话那条路径同一个症状。
	// 复用紧邻上方 `clearEngineHistoryFor` 的同一条判据：跑着的会话的状态一律不动；
	// 空闲会话解绑后，它下次作为当前会话会走 resume/切项目那条挂接重新绑上。
	if !currentRunning {
		if store, ok := service.Deps.Sessions.(session_runtime.SessionContextPort); ok {
			store.DetachSessionContext()
		}
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
	var restoredWorkspace *model.WorkspaceInfo
	if slot.Workspace != nil {
		item := *slot.Workspace
		restoredWorkspace = &item
	}
	service.ViewMu.Unlock()

	// 需求变更（P1-1 补口）：新会话的权限档位必须在这里**读回并重算**，与
	// 冷启动 / 热挂载 / 冷恢复三条切换路径同构。草稿槽位是可复用的（切走再新建
	// 会恢复同一份早分配 SID），所以"这个会话之前选过档位"是真实存在的情形；读盘
	// 只能在锁外（readStoredPermissionTier 含磁盘读，不得持 Core.ViewMu）。
	storedTier := service.readStoredPermissionTier(draftID)

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
	// 档位重算落在同一临界区里（会话单元已就位）：草稿自己的档位（会话槽或进程
	// 默认）覆盖掉上一个会话留在快照里的那个值——否则新会话的 chip 显示的是**上一个
	// 会话的档位**，而真正生效的是本会话的档位（见 syncViewPermissionTierLocked）。
	// `/new` 是唯一要读盘取档位的路径（草稿槽可复用 ⇒ "这个会话之前选过档位"真实存在），
	// 读盘已在锁外完成，这里只把值应用下去。
	service.enterDraftShellLocked(draftShellOptions{
		sessionID:          draftID,
		workspace:          restoredWorkspace,
		resetHistoryWindow: true,
		composerFromUnit:   true,
		resetTitle:         true,
		tier: func(sessionID string) {
			service.applyStoredPermissionTier(sessionID, storedTier)
		},
	})
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

// draftShellOptions 是一次"把视图单例切到草稿壳"的**显式差异表**（M5-P18：三条路径的字段清理收成一处）。
//
// 草稿在系统里有三个角色（2026-10-17 用户裁决时点明）：
//
//  1. **`/new` 进草稿**（`BeginNewSession`）：可能复用保留的草稿槽——恢复它的展示绑定与档位，
//     清掉上一个会话留在快照里的全部会话事实；
//  2. **冷加载中断时恢复草稿内容**（`resetViewToDraftAfterRestoreFailure`）：冷恢复失败且无前一会话
//     可回退时把视图退回草稿壳；**不做任何持久化**（避免空壳覆盖目标会话记录），因此保留窗口读数；
//     但已读文件属于"未加载成功的那个会话"，草稿壳不继承（否则物化时会被写成新会话的 Execution）；
//  3. **卸载/删掉正在看的会话**（`switchViewToFreshDraftLocked`）：视图单例必须始终有对应单元，
//     就地新建一个早分配草稿单元；**不写槽位**（不进入会话树，与卸载前语义一致）。进程级交互
//     （`SessionID` 为空）不随会话卸载失效，保留；会话级交互属于被卸下的会话，必须清掉。
//
// 三条 `keep*`/`reset*` 开关就是三条路径的**真实差异**（本片之前是三份手抄的清理清单，
// 集合各不相同、看不出哪些是有意的）。差异矩阵与理由见 `session_draft_shell_test.go`。
// 2026-10-17 用户裁决后，02 的 `ReadFiles` 与 03 的会话级 `Interaction` 两处漂移已收敛。
type draftShellOptions struct {
	// sessionID 是草稿的早分配 SID。
	sessionID string
	// workspace 写进 `Snapshot.CurrentWorkspace`；`keepWorkspace=true` 时整格不动。
	workspace     *model.WorkspaceInfo
	keepWorkspace bool
	// resetHistoryWindow 把 `HistoryOffset` / `TotalMessages` / `HasMoreHistory` 归零。
	resetHistoryWindow bool
	// keepProcessInteraction 只保留**进程级**交互（`SessionID` 为空）；会话级交互随
	// 被卸下的会话一起失效（03 就地换单元）。
	keepProcessInteraction bool
	// composerFromUnit 把草稿单元的 composer 文本写进快照（只有 01 写）。
	composerFromUnit bool
	// resetTitle 把该会话的标题清成空（01/02 清，03 不动）。
	resetTitle bool
	// tier 在临界区内重算本会话的生效档位：01 用**读盘值**，02/03 用会话单元的读数。
	tier func(sessionID string)
}

// enterDraftShellLocked 把视图单例切到草稿壳（调用方持有 Core.ViewMu），返回草稿的会话单元。
//
// 公共部分只有一份：会话指针、会话事实（`Task` / `Runtime.Plan`）、对话、聊天运行态都在这里清，
// 差异由 `draftShellOptions` 的开关显式表达。历史口径（2026-10-01 现场）：`Task` 是压缩记录与
// 任务状态的唯一投影面、`ReadFiles` 会被 `PersistCurrentSession` 写成目标会话的 `Execution`——
// 两者都归会话所有，换视图指针就必须清，否则新会话会显示、并落盘成上一会话的历史。
func (service *Service) enterDraftShellLocked(options draftShellOptions) *session.SessionUnit {
	service.nextViewEpochLocked()
	service.Core.Snapshot.Session = model.SessionState{
		ID: options.sessionID, Name: draftSessionName, Draft: true, Status: SessionStatusDraft,
	}
	service.sessions.SetActive(options.sessionID)
	if !options.keepWorkspace {
		service.Core.Snapshot.CurrentWorkspace = options.workspace
	}
	service.Core.Snapshot.Conversation = nil
	if options.resetHistoryWindow {
		service.Core.Snapshot.HistoryOffset = 0
		service.Core.Snapshot.TotalMessages = 0
		service.Core.Snapshot.HasMoreHistory = false
	}
	service.Core.Snapshot.Task = nil
	service.Core.Snapshot.ReadFiles = nil
	service.Core.Snapshot.Runtime.Plan = nil
	if !options.keepProcessInteraction {
		service.Core.Snapshot.Interaction = nil
	} else if interaction := service.Core.Snapshot.Interaction; interaction != nil && interaction.SessionID != "" {
		// 会话级交互（审批等）挂在被卸下的会话上，换单元后必须清；进程级交互
		// （账号/计划重试）与会话无关，保留。
		service.Core.Snapshot.Interaction = nil
	}
	runtime := service.sessionUnitLocked(options.sessionID)
	runtime.SetChatState(model.ChatState{}, nil)
	runtime.SetCancel(nil)
	runtime.SetRequests(nil)
	service.Core.Snapshot.Chat = runtime.ChatState()
	if options.composerFromUnit {
		service.Core.Snapshot.Session.Composer = runtime.ComposerText()
	}
	if options.tier != nil {
		options.tier(options.sessionID)
	}
	if options.resetTitle {
		service.components.sessions.SetSessionTitleLocked(options.sessionID, model.SessionTitle{})
	}
	service.components.tasks.ResetForNewSessionLocked()
	return runtime
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
	var workspace *model.WorkspaceInfo
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
	newID, err := service.openSessionEngine(draftID, workspace)
	if err != nil {
		return err
	}
	workspaceProjection := service.collectWorkspaceProjection()

	service.ViewMu.Lock()
	service.draft = nil // 草稿已物化为真实会话，消费槽位
	title := model.SessionTitle{Value: session_runtime.SessionTitle(firstQuestion), Source: "first_request", FinalizedAt: time.Now()}
	service.Core.Snapshot.Session = model.SessionState{ID: newID, Name: title.Value}
	// 视图单例一致性：V 的唯一镜像随物化切到新会话（与 hot_attach/resume 同
	// 步路径），否则流式增量会按 ActiveID="" 误判为后台，Snapshot 收不到增量。
	service.sessions.SetActive(newID)
	service.components.sessions.SetSessionTitleLocked(newID, title)
	service.components.tasks.ResetPlanStateLocked()
	service.applyWorkspaceProjectionLocked(workspaceProjection)
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	service.publishRuntimeProjections()
	service.publishSessionEvent(EventSnapshotChanged, revision, "", newID, nil)
	service.components.sessions.RequestCatalogRefresh()
	// 真相源提交时刻的**批量落盘**：草稿物化进真相源 = 这一会话的读数入账点
	// （见 tool_record.go 的 FlushToolRecord：不做逐次工具写盘，避免磁盘一直忙）。
	// 在锁外调（只读内存累计 + 写会话存储，不碰视图状态）。
	service.FlushToolRecord(newID)
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
	return service.draftTargetLocked().Matches(sessionID)
}

// draftTargetLocked 把草稿归属判据的两个来源（进程单例草稿槽 + 共享视图镜像）装成
// middle/switch 的 DraftTarget。调用方持有 Core.ViewMu；只读视图状态，不落盘、不换指针。
func (service *Service) draftTargetLocked() switching.DraftTarget {
	target := switching.DraftTarget{
		ViewDraft: service.Core.Snapshot.Session.Draft,
		ViewID:    service.Core.Snapshot.Session.ID,
	}
	if service.draft != nil {
		target.HasSlot = true
		target.SlotID = service.draft.ID
	}
	return target
}

// materializeDraftForSubmit 把「显式提交的目标恰好是未物化的草稿」接回物化路径。
//
// 存在理由：前端普通输入一律走 SubmitToSession + 显式视图会话 ID（含草稿的早分配
// SID，见 gui/frontend/dist/composer-input.js），而草稿从来没有可冷回读的历史——
// 不先物化，SubmitToSession 会按"目标未加载"去 ActivateSession→冷加载那份
// Status=draft 的 record：新会话以「已恢复会话: seelex-…」开头、草稿槽位不消费、
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
	mine := service.draftTargetLocked().InView(sessionID)
	service.ViewMu.RUnlock()
	if !mine {
		return ErrDraftNotInView
	}
	return service.materializeDraftSession(strings.TrimSpace(firstInput))
}
