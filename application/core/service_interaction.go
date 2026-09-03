package core

import (
	"context"
	"errors"
	"fmt"
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
				return service.Approval.Resolve(id, ApprovalDecision{OptionID: optionID})
			}
		}
		return ErrInteractionNotFound
	}
	if optionID == "__CANCEL__" {
		if interaction.Kind == "approval" {
			return service.Approval.Resolve(id, ApprovalDecision{OptionID: optionID})
		}
		service.closeInteraction(id)
		return nil
	}
	switch interaction.Kind {
	case "approval":
		return service.Approval.Resolve(id, ApprovalDecision{OptionID: optionID})
	case "session":
		if err := service.resumeSession(optionID); err != nil {
			service.addNotice("恢复失败: " + err.Error())
			service.closeInteraction(id)
			return err
		}
	case "account":
		if err := service.SelectAccount(ctx, optionID); err != nil {
			service.addNotice("账号切换失败: " + err.Error())
			service.closeInteraction(id)
			return err
		}
	case "plan_retry":
		switch optionID {
		case "replan":
			if err := service.replanFailedWork(ctx, interaction.ID, interaction.Question); err != nil {
				return err
			}
		case "retry":
			service.appendPlanRetryNotice("节点失败，用户选择重试。请调用 plan_run 重新执行工作流。")
		case "skip":
			service.appendPlanRetryNotice("节点失败，用户选择跳过。请修改工作流（plan_clear + plan_load）排除失败节点后重新 plan_run。")
		case "abort":
			service.abortPlanInteraction()
		}
	default:
		return fmt.Errorf("unsupported interaction kind %q", interaction.Kind)
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
	service.appendMessageLocked("system", "工作流已终止。", nil)
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
	service.ViewMu.RLock()
	viewSessionID := service.Core.Snapshot.Session.ID
	viewRunning := false
	if unit := service.sessions.Unit(viewSessionID); unit != nil {
		viewRunning = unit.ChatState().Running
	}
	service.ViewMu.RUnlock()
	if viewRunning {
		return errors.New("当前会话正在运行：请等待回合结束或取消后再切换 Effort")
	}
	if level == "" || level == "cycle" {
		next, err := service.effortManager.Cycle()
		if err != nil {
			return err
		}
		level = next
	}
	if err := service.effortManager.Apply(level); err != nil {
		return err
	}
	service.Deps.Runtime.SetPlanPolicy(service.effortManager.PlanPolicy())
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
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventSnapshotChanged, revision, "", service.currentViewSessionID(), nil)
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
		service.effortManager = NewEffortManager(service.promptStack, service.Deps.Engine)
		service.resetConversation("已停用插件")
	} else {
		if err := service.Deps.Plugins.Activate(ctx, name); err != nil {
			return fmt.Errorf("activate plugin: %w", err)
		}
		service.Deps.Engine.ClearHistory()
		if current, ok := service.Deps.Plugins.Current(); ok {
			service.promptStack.Reset(strings.TrimSpace(current.Prompt))
		}
		service.effortManager = NewEffortManager(service.promptStack, service.Deps.Engine)
		_ = service.effortManager.Apply(service.effortManager.Current())
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

func (service *Service) SetFullAccess(on bool) {
	if service == nil {
		return
	}
	if !on && service.Approval != nil {
		service.Approval.SetPermissionAutoApproval(false)
	}
	viewSessionID := service.currentViewSessionID()
	// G4：全权选择归属视图会话单元（每个会话记住自己的模式）；引擎门立即
	// 同步（运行中开全权用于放行当前审批），chat 起点再按槽兜底同步。
	if unit := service.sessions.Unit(viewSessionID); unit != nil {
		unit.SetFullAccessMode(on)
	}
	if service.Deps.Runtime != nil {
		service.Deps.Runtime.SetFullAccess(on)
	}
	effective := service.fullAccessForSession(viewSessionID)
	if effective && service.Approval != nil {
		service.Approval.SetPermissionAutoApproval(true)
		service.Approval.ResolveAll(ApprovalDecision{OptionID: "always"})
	}
	service.ViewMu.Lock()
	service.Core.Snapshot.Runtime.FullAccess = effective
	revision := service.bumpLocked()
	runtime := cloneRuntimeState(service.Core.Snapshot.Runtime)
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventRuntimeChanged, revision, "", viewSessionID, runtime)
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
func (service *Service) observeInteraction(sessionID, requestID string, interaction *Interaction) {
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
		copied.Options = append([]InteractionOption(nil), interaction.Options...)
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
		return
	}
	service.publishSessionEvent(EventInteractionOpened, revision, requestID, publishSID, interaction)
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
	copied.Options = append([]InteractionOption(nil), pending[0].Options...)
	service.Core.Snapshot.Interaction = &copied
}

func (service *Service) openInteraction(interaction *Interaction) {
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

func (service *Service) sessionInteraction() *Interaction {
	sessions := service.Snapshot().Sessions
	options := make([]InteractionOption, 0, len(sessions))
	for _, session := range sessions {
		label := session.Name
		if label == "" {
			label = service.components.sessions.ShortSessionID(session.ID)
		}
		options = append(options, InteractionOption{ID: session.ID, Label: label, Description: fmt.Sprintf("tok:%d  %s", session.TokenCount, session.UpdatedAt.Format("01-02 15:04"))})
	}
	return &Interaction{ID: fmt.Sprintf("session-%d", time.Now().UnixNano()), Kind: "session", Title: "选择会话", Options: options, OpenedAt: time.Now()}
}

func (service *Service) accountInteraction() *Interaction {
	accounts := service.Deps.Runtime.Accounts()
	options := make([]InteractionOption, 0, len(accounts))
	for _, account := range accounts {
		label := account.Name
		if account.Disabled {
			label += " [禁用]"
		}
		options = append(options, InteractionOption{ID: account.Name, Label: label, Description: strings.TrimSpace(account.Provider + " " + account.Model)})
	}
	return &Interaction{ID: fmt.Sprintf("account-%d", time.Now().UnixNano()), Kind: "account", Title: "切换账号", Options: options, OpenedAt: time.Now()}
}
