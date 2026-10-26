package core

import (
	session "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/application/session/switchpolicy"
	"github.com/RedHuang-0622/seelex/contract/protocol"
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
	service.components.view.NextViewEpochLocked()
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
	service.components.conversationTurnState.ResetLocked(runtime)
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
