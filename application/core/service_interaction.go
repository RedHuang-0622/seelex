package core

import (
	"context"
	"errors"
	"fmt"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/middle/switch"
	"strings"
	"time"
)

func (service *Service) ResolveInteraction(ctx context.Context, id, optionID string) error {
	service.ViewMu.RLock()
	interaction := service.Core.Snapshot.Interaction
	service.ViewMu.RUnlock()
	if interaction == nil || interaction.ID != id {
		// 波 4 approval 会话级归属：待批可能不在视图单格（后台会话卡在
		// 审批、或单格已被下一笔覆盖）。broker 仍持有时按 id 直接结案；
		// 非 approval 交互（session/account/plan_retry）保持视图单格校验。
		if service.Approval != nil {
			if _, ok := service.pendingApprovalSession(id); ok {
				return service.Approval.Resolve(id, contract.ApprovalDecision{OptionID: optionID})
			}
		}
		return ErrInteractionNotFound
	}
	if optionID == switching.InteractionCancelOptionID {
		if interaction.Kind == switching.InteractionKindApproval {
			return service.Approval.Resolve(id, contract.ApprovalDecision{OptionID: optionID})
		}
		service.closeInteraction(id)
		return nil
	}
	switch interaction.Kind {
	case switching.InteractionKindApproval:
		return service.Approval.Resolve(id, contract.ApprovalDecision{OptionID: optionID})
	case switching.InteractionKindSession:
		if err := service.resumeSession(optionID); err != nil {
			service.addNotice("恢复失败: " + err.Error())
			service.closeInteraction(id)
			return err
		}
	case switching.InteractionKindAccount:
		if err := service.SelectAccount(ctx, optionID); err != nil {
			service.addNotice("账号切换失败: " + err.Error())
			service.closeInteraction(id)
			return err
		}
	case switching.InteractionKindPlanRetry:
		switch optionID {
		case switching.PlanRetryOptionReplan:
			if err := service.replanFailedWork(ctx, interaction.ID, interaction.Question); err != nil {
				return err
			}
		case switching.PlanRetryOptionRetry:
			service.appendPlanRetryNotice(switching.PlanRetryNoticeRetry)
		case switching.PlanRetryOptionSkip:
			service.appendPlanRetryNotice(switching.PlanRetryNoticeSkip)
		case switching.PlanRetryOptionAbort:
			service.abortPlanInteraction()
		}
	default:
		return switching.UnsupportedInteractionKind(interaction.Kind)
	}
	service.closeInteraction(id)
	return nil
}

// pendingApprovalSession 在 broker 待批集合中按审批 ID 反查归属会话
// （跨会话待批解析/镜像判定用；集合小，线性扫描可接受）。
func (service *Service) pendingApprovalSession(id string) (string, bool) {
	for _, entry := range service.Approval.Pending() {
		if entry.Interaction.ID == id {
			return entry.SessionID, true
		}
	}
	return "", false
}

func (service *Service) appendPlanRetryNotice(message string) {
	service.ViewMu.Lock()
	service.appendMessageLocked("system", message, nil)
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventSnapshotChanged, revision, "", service.currentViewSessionID(), nil)
}

func (service *Service) abortPlanInteraction() {
	service.ViewMu.Lock()
	if plan := service.Core.Snapshot.Runtime.Plan; plan != nil {
		plan.Status = PlanAborted
		for index := range plan.Nodes {
			if plan.Nodes[index].Status == NodePending || plan.Nodes[index].Status == NodeRunning {
				plan.Nodes[index].Status = NodeAborted
			}
		}
	}
	service.appendMessageLocked("system", switching.PlanAbortNotice, nil)
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventSnapshotChanged, revision, "", service.currentViewSessionID(), nil)
}

func (service *Service) SelectAccount(_ context.Context, name string) error {
	if !service.Deps.Runtime.SelectAccount(name) {
		return fmt.Errorf("账号不可用: %s", name)
	}
	runtimeProjection := service.collectRuntimeProjection(context.Background())
	service.ViewMu.Lock()
	service.Core.Snapshot.Runtime.Account = name
	service.applyRuntimeProjectionLocked(runtimeProjection)
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventRuntimeChanged, revision, "", service.currentViewSessionID(), service.Snapshot().Runtime)
	service.addNotice("已切换账号: " + name)
	return nil
}

// SwitchEffort 切换 Effort 等级（用户级动作，作用于视图会话）。
//
// 运行守卫（G0b/INV-G7）：effort 会改写视图会话的 system prompt 与后续
// 回合预算（含排队回合），因此视图会话 running 时拒绝；系统提示只经
// SetSystemPromptFor 写目标（视图）会话引擎，绝不触碰运行中的后台会话
// （后台会话的 prompt 由各自的 runChat 起点/热挂载刷新）。会话化 effort
// 属刀 4，在此之前的全局选择器只允许在视图空闲时变更。
func (service *Service) SwitchEffort(_ context.Context, level string) error {
	viewSessionID, err := service.knobs.EnsureViewIdle("切换 Effort")
	if err != nil {
		return err
	}
	if level == "" || level == "cycle" {
		next, cycleErr := service.effortManager.Cycle()
		if cycleErr != nil {
			return cycleErr
		}
		level = next
	}
	if err := service.effortManager.Apply(level); err != nil {
		return err
	}
	service.Deps.Runtime.SetPlanPolicy(service.effortManager.PlanPolicy())
	// 档位变化的另一半（"想多深"）落在账号客户端上：这里是这条链唯一的下发点
	// ——loop/预算由 EffortManager 直接写引擎（applyLocked 里的 SetMaxLoops），
	// 思考强度只此一条路（syncSessionReasoningEffort → 账号池）。
	service.syncSessionReasoningEffort(service.effortManager.Current())
	promptText := service.promptStack.Render()
	if routed, ok := service.Deps.Engine.(interface{ SetSystemPromptFor(string, string) }); ok {
		routed.SetSystemPromptFor(viewSessionID, promptText)
	} else {
		service.Deps.Engine.SetSystemPrompt(promptText)
	}
	service.ViewMu.Lock()
	if unit := service.sessions.Unit(viewSessionID); unit != nil {
		// G4：effort 选择归属进 SessionUnit（视图会话 idle 才允许变更；
		// 后台会话各自保留自己的选择）。
		unit.SetEffortLevel(service.effortManager.Current())
	}
	service.Core.Snapshot.Runtime.Effort = service.effortManager.Current()
	// 档位 = **两条拉条的预设**：切档位把两条拉条的读数一起带进该档（前端因此看得见
	// "档位改了什么"），而拉条各自只改自己那一项（composer 底部的两条独立拉条，
	// 见 backend/limit.Knobs）。权威读数与快照一起写：runtime 投影每次整份重建 Runtime，
	// 只写快照会被下一次投影清零。加锁次序：ViewMu → 组件锁（此处已持 ViewMu）。
	service.knobs.SeedReadings(
		reasoningEffortFor(service.effortManager.Current()),
		maxLoopsFor(service.effortManager.Current()),
	)
	service.Core.Snapshot.Runtime.ReasoningEffort = service.knobs.ReasoningEfforts()
	service.Core.Snapshot.Runtime.LoopLimit = service.knobs.LoopLimit()
	revision := service.bumpLocked()
	runtime := cloneRuntimeState(service.Core.Snapshot.Runtime)
	service.ViewMu.Unlock()
	// 下发完整运行时而不是 nil：payload 为 nil 的 runtime.changed 会被前端
	// 判为不可增量应用 → 触发一次整份快照刷新（表现为"调 effort 卡一下、
	// 插件/技能列表闪动"）。同 SelectAccount/SetFullAccess 的口径。
	service.publishSessionEvent(EventRuntimeChanged, revision, "", service.currentViewSessionID(), runtime)
	return nil
}

// SwitchPlugin 切换/停用插件（进程级动作，G0b/M6）。
//
// 运行守卫：插件切换会重建进程级 prompt 栈、清空引擎历史并重置对话，任何
// 会话 running 都会被打到（运行中会话的下一轮 prompt 与历史原件被替换），
// 因此任一会话 running 即拒绝；模型侧 switch_plugin 工具走 seelebridge
// 的独立激活面，不经过本进程级入口。
func (service *Service) SwitchPlugin(ctx context.Context, name string) error {
	service.ViewMu.RLock()
	anyRunning := service.anyChatRunningLocked()
	service.ViewMu.RUnlock()
	if anyRunning {
		return errors.New("有会话正在运行：插件切换会重建进程级提示与历史，请等待全部会话空闲后再切换")
	}
	if name == "off" || name == "none" || name == "" {
		if err := service.Deps.Plugins.Deactivate(ctx); err != nil {
			return fmt.Errorf("deactivate plugin: %w", err)
		}
		service.Deps.Engine.ClearHistory()
		service.promptStack.Reset("")
		service.Deps.Engine.SetSystemPrompt("")
		service.reapplyEffortAfterPluginSwitch()
		service.resetConversation("已停用插件")
	} else {
		if err := service.Deps.Plugins.Activate(ctx, name); err != nil {
			return fmt.Errorf("activate plugin: %w", err)
		}
		service.Deps.Engine.ClearHistory()
		if current, ok := service.Deps.Plugins.Current(); ok {
			service.promptStack.Reset(strings.TrimSpace(current.Prompt))
		}
		service.reapplyEffortAfterPluginSwitch()
		service.Deps.Engine.SetSystemPrompt(service.promptStack.Render())
		service.resetConversation("已切换到 " + name + " 插件")
	}
	runtimeProjection := service.collectRuntimeProjection(ctx)
	service.ViewMu.Lock()
	service.applyRuntimeProjectionLocked(runtimeProjection)
	revision := service.bumpLocked()
	runtime := cloneRuntimeState(service.Core.Snapshot.Runtime)
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventRuntimeChanged, revision, "", service.currentViewSessionID(), runtime)
	service.publishRuntimeProjections()
	return nil
}

// reapplyEffortAfterPluginSwitch 在插件切换后重新应用**用户当前的 effort
// 等级**。
//
// 为什么不是重建 EffortManager：管理器持有的是同一个 promptStack 与同一个
// engine，重建既没有必要，又会让用户选择的强度被默认值（high）吃掉，还会让
// prompt_layer 装配期捕获的旧指针变成孤儿。插件切换只该换 prompt 前缀。
func (service *Service) reapplyEffortAfterPluginSwitch() {
	if service == nil || service.effortManager == nil {
		return
	}
	_ = service.effortManager.Apply(service.effortManager.Current())
	// Apply 会把引擎循环上限写回**档位默认值**（applyLocked 里的 SetMaxLoops）——
	// 而用户可能已经用底部的循环次数拉条把它拧到了别的值。这里补回"当前生效的
	// 循环上限"，让插件切换不再偷偷把拉条值吃掉（currentLoopLimit 读的就是快照里
	// 那份权威值，没被用户改过时回退档位默认值，与 Apply 写的一致）。
	service.knobs.ApplyLoopLimit(service.knobs.CurrentLoopLimit())
}

// SetPermissionTier 切换**视图会话的权限档位**，并返回真正生效的档位。
//
// 返回值的意义：前端不能再靠"取反本地旧状态"猜方向——快照滞后一格时，取反会把
// "开启"点成"关闭"，用户看到的就是"点了全权仍被拒"。未识别的档位 id 报错且
// **不改变**当前档位（写入侧显式失败）。
//
// 会话级归属（G4）：选择落在视图会话单元，执行门与审批面都按同一个会话写——A
// 会话切档不得替 B 会话放行（污染），B 的起点同步也不得关掉 A（失灵）。
func (service *Service) SetPermissionTier(tier string) (string, error) {
	if service == nil {
		return "", errors.New("permission tier: nil service")
	}
	normalized, err := dto.NormalizePermissionTier(tier)
	if err != nil {
		return "", err
	}
	viewSessionID := service.currentViewSessionID()
	// 需求变更（2026-09-17）：档位是"这一 session 的权限设置"，先落盘再改内存态——
	// 写盘失败时保持原档位并显式报错，而不是让用户看到"已切全权"、重启后又变回去。
	if err := service.persistPermissionTier(viewSessionID, normalized); err != nil {
		return "", err
	}
	// G4：档位选择归属视图会话单元（每个会话记住自己的档位）；执行门与审批面
	// 同步按**本会话**写入（运行中切档用于放行/收紧当前审批），不影响其它会话。
	if unit := service.sessions.Unit(viewSessionID); unit != nil {
		unit.SetPermissionTier(normalized)
	}
	full := dto.PermissionTierIsFullAccess(normalized)
	if service.Approval != nil {
		service.Approval.SetPermissionAutoApprovalFor(viewSessionID, full)
	}
	if service.Deps.Runtime != nil {
		if err := service.Deps.Runtime.SetPermissionTierFor(viewSessionID, normalized); err != nil {
			return "", err
		}
	}
	effective := service.permissionTierForSession(viewSessionID)
	if effective == dto.PermissionTierFull && service.Approval != nil {
		// 只结**本会话**正在等待的权限审批：full 档是会话级决定，替别的会话
		// 点头就是污染。放行用 "allow"（本笔放行）而不是 "always"——自动放行
		// 不得在共享 checker 里留下永久 allow 规则（那是持久权限污染，切回低档
		// 后仍会在别的会话生效）。
		service.Approval.ResolveAllFor(viewSessionID, contract.ApprovalDecision{OptionID: "allow"})
	}
	service.ViewMu.Lock()
	service.Core.Snapshot.Runtime.PermissionTier = effective
	service.Core.Snapshot.Runtime.FullAccess = dto.PermissionTierIsFullAccess(effective)
	revision := service.bumpLocked()
	runtime := cloneRuntimeState(service.Core.Snapshot.Runtime)
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventRuntimeChanged, revision, "", viewSessionID, runtime)
	return effective, nil
}

// SetFullAccess 是权限档位的**兼容壳**：true → full 档、false → manual 档；返回
// 生效档位是否等价旧的全权开启（前端按旧口径渲染时的读面）。
func (service *Service) SetFullAccess(on bool) bool {
	effective, err := service.SetPermissionTier(dto.PermissionTierFromFullAccess(on))
	if err != nil {
		return false
	}
	return dto.PermissionTierIsFullAccess(effective)
}

// observeInteraction 是 ApprovalBroker 的开/结观察回调（波 4 approval 会话
// 级归属）：open → (sessionID, requestID, interaction)，close →
// (sessionID, requestID, nil)。
//
// 归属路由：审批按所属 sid 记账到会话单元（Unit.Approvals →
// awaiting_approval），事件按所属 sid 发布（(通道,sid) 语义）；单格
// Snapshot.Interaction 只镜像当前视图会话（或进程级空归属）的审批——
// 后台会话的待批不进视图单格，由目录行 awaiting_approval 状态与跨会话
// 待批计数承载。
func (service *Service) observeInteraction(sessionID, requestID string, interaction *model.Interaction) {
	sessionID = strings.TrimSpace(sessionID)
	viewSessionID := service.currentViewSessionID()
	service.ViewMu.Lock()
	if interaction == nil {
		// 结案：摘除会话归属（幂等），并只清与本次结案同归属的视图单格。
		if sessionID != "" {
			if unit := service.sessions.Unit(sessionID); unit != nil {
				unit.RemoveApproval(requestID)
			}
		}
		slot := service.Core.Snapshot.Interaction
		if slot != nil && (sessionID == "" || (slot.SessionID == sessionID && slot.ID == requestID)) {
			service.Core.Snapshot.Interaction = nil
			// 同会话仍有多笔待批时，把下一笔镜像进单格（后开覆盖先开
			// 的进程级语义在会话格内退化为「同会话首笔」）。
			service.mirrorPendingApprovalsLocked(viewSessionID)
		}
	} else {
		copied := *interaction
		copied.SessionID = sessionID
		copied.Options = append([]model.InteractionOption(nil), interaction.Options...)
		if sessionID != "" {
			if unit := service.sessions.Unit(sessionID); unit != nil {
				unit.AddApproval(requestID)
			}
		}
		if sessionID == "" || sessionID == viewSessionID {
			service.Core.Snapshot.Interaction = &copied
		}
	}
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	publishSID := sessionID
	if publishSID == "" {
		publishSID = viewSessionID
	}
	if interaction == nil {
		service.publishSessionEvent(EventInteractionClosed, revision, requestID, publishSID, nil)
		service.publishSessionStateChanged(publishSID)
		return
	}
	service.publishSessionEvent(EventInteractionOpened, revision, requestID, publishSID, interaction)
	service.publishSessionStateChanged(publishSID)
}

// mirrorPendingApprovalsLocked 把指定会话当前首笔待批审批镜像到
// Snapshot.Interaction（调用方持有 Core.ViewMu；hotAttach 切换会话/结案
// 后调用——单格交互只表达当前视图会话的审批；无待批时清空）。
func (service *Service) mirrorPendingApprovalsLocked(sessionID string) {
	if service.Approval == nil {
		service.Core.Snapshot.Interaction = nil
		return
	}
	pending := service.Approval.PendingBySession(sessionID)
	if len(pending) == 0 {
		service.Core.Snapshot.Interaction = nil
		return
	}
	copied := pending[0]
	copied.Options = append([]model.InteractionOption(nil), pending[0].Options...)
	service.Core.Snapshot.Interaction = &copied
}

func (service *Service) openInteraction(interaction *model.Interaction) {
	if interaction == nil {
		return
	}
	service.ViewMu.Lock()
	service.Core.Snapshot.Interaction = interaction
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventInteractionOpened, revision, interaction.ID, service.currentViewSessionID(), interaction)
}

func (service *Service) closeInteraction(id string) {
	service.ViewMu.Lock()
	service.components.tasks.DeleteReplanInFlight(id)
	if service.Core.Snapshot.Interaction != nil && service.Core.Snapshot.Interaction.ID == id {
		service.Core.Snapshot.Interaction = nil
	}
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventInteractionClosed, revision, id, service.currentViewSessionID(), nil)
}

func (service *Service) sessionInteraction() *model.Interaction {
	sessions := service.Snapshot().Sessions
	choices := make([]switching.SessionChoice, 0, len(sessions))
	for _, session := range sessions {
		choice := switching.SessionChoice{
			ID:         session.ID,
			Name:       session.Name,
			TokenCount: session.TokenCount,
			UpdatedAt:  session.UpdatedAt,
		}
		// 短 ID 是**惰性端口**：只在名称缺失时取（判据在 middle/switch，取数在宿主）。
		choice.ShortID = func() string { return service.components.sessions.ShortSessionID(session.ID) }
		choices = append(choices, choice)
	}
	interaction := switching.BuildSessionInteraction(switching.SessionInteractionID(time.Now()), choices, time.Now())
	return &interaction
}

func (service *Service) accountInteraction() *model.Interaction {
	accounts := service.Deps.Runtime.Accounts()
	choices := make([]switching.AccountChoice, 0, len(accounts))
	for _, account := range accounts {
		choices = append(choices, switching.AccountChoice{
			Name:     account.Name,
			Disabled: account.Disabled,
			Provider: account.Provider,
			Model:    account.Model,
		})
	}
	interaction := switching.BuildAccountInteraction(switching.AccountInteractionID(time.Now()), choices, time.Now())
	return &interaction
}
