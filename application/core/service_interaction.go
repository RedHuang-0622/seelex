package core

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (service *Service) ResolveInteraction(ctx context.Context, id, optionID string) error {
	service.mu.RLock()
	interaction := service.snapshot.Interaction
	service.mu.RUnlock()
	if interaction == nil || interaction.ID != id {
		return ErrInteractionNotFound
	}
	if optionID == "__CANCEL__" {
		if interaction.Kind == "approval" {
			return service.approval.Resolve(id, ApprovalDecision{OptionID: optionID})
		}
		service.closeInteraction(id)
		return nil
	}
	switch interaction.Kind {
	case "approval":
		return service.approval.Resolve(id, ApprovalDecision{OptionID: optionID})
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

func (service *Service) appendPlanRetryNotice(message string) {
	service.mu.Lock()
	service.appendMessageLocked("system", message, nil)
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventSnapshotChanged, revision, "", nil)
}

func (service *Service) abortPlanInteraction() {
	service.mu.Lock()
	if plan := service.snapshot.Runtime.Plan; plan != nil {
		plan.Status = PlanAborted
		for index := range plan.Nodes {
			if plan.Nodes[index].Status == NodePending || plan.Nodes[index].Status == NodeRunning {
				plan.Nodes[index].Status = NodeAborted
			}
		}
	}
	service.appendMessageLocked("system", "工作流已终止。", nil)
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventSnapshotChanged, revision, "", nil)
}

func (service *Service) SelectAccount(_ context.Context, name string) error {
	if !service.deps.Runtime.SelectAccount(name) {
		return fmt.Errorf("账号不可用: %s", name)
	}
	service.mu.Lock()
	service.snapshot.Runtime.Account = name
	service.refreshRuntimeLocked(context.Background())
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventRuntimeChanged, revision, "", service.Snapshot().Runtime)
	service.addNotice("已切换账号: " + name)
	return nil
}

func (service *Service) SwitchEffort(_ context.Context, level string) error {
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
	service.deps.Runtime.SetPlanPolicy(service.effortManager.PlanPolicy())
	service.deps.Engine.SetSystemPrompt(service.promptStack.Render())
	service.mu.Lock()
	service.snapshot.Runtime.Effort = service.effortManager.Current()
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventSnapshotChanged, revision, "", nil)
	return nil
}

func (service *Service) SwitchPlugin(ctx context.Context, name string) error {
	if name == "off" || name == "none" || name == "" {
		if err := service.deps.Plugins.Deactivate(ctx); err != nil {
			return fmt.Errorf("deactivate plugin: %w", err)
		}
		service.deps.Engine.ClearHistory()
		service.promptStack.Reset("")
		service.deps.Engine.SetSystemPrompt("")
		service.effortManager = NewEffortManager(service.promptStack, service.deps.Engine)
		service.resetConversation("已停用插件")
	} else {
		if err := service.deps.Plugins.Activate(ctx, name); err != nil {
			return fmt.Errorf("activate plugin: %w", err)
		}
		service.deps.Engine.ClearHistory()
		if current, ok := service.deps.Plugins.Current(); ok {
			service.promptStack.Reset(strings.TrimSpace(current.Prompt))
		}
		service.effortManager = NewEffortManager(service.promptStack, service.deps.Engine)
		_ = service.effortManager.Apply(service.effortManager.Current())
		service.deps.Engine.SetSystemPrompt(service.promptStack.Render())
		service.resetConversation("已切换到 " + name + " 插件")
	}
	service.mu.Lock()
	service.refreshRuntimeLocked(ctx)
	revision := service.bumpLocked()
	runtime := cloneRuntimeState(service.snapshot.Runtime)
	service.mu.Unlock()
	service.events.Publish(EventRuntimeChanged, revision, "", runtime)
	return nil
}

func (service *Service) SetFullAccess(on bool) {
	if !on && service.approval != nil {
		service.approval.SetPermissionAutoApproval(false)
	}
	service.deps.Runtime.SetFullAccess(on)
	fullAccess := service.deps.Runtime.FullAccess()
	if fullAccess && service.approval != nil {
		service.approval.SetPermissionAutoApproval(true)
		service.approval.ResolveAll(ApprovalDecision{OptionID: "always"})
	}
	service.mu.Lock()
	service.snapshot.Runtime.FullAccess = fullAccess
	revision := service.bumpLocked()
	runtime := cloneRuntimeState(service.snapshot.Runtime)
	service.mu.Unlock()
	service.events.Publish(EventRuntimeChanged, revision, "", runtime)
}

func (service *Service) observeInteraction(interaction *Interaction) {
	service.mu.Lock()
	previousID := ""
	if service.snapshot.Interaction != nil {
		previousID = service.snapshot.Interaction.ID
	}
	if interaction == nil {
		service.snapshot.Interaction = nil
	} else {
		copied := *interaction
		copied.Options = append([]InteractionOption(nil), interaction.Options...)
		service.snapshot.Interaction = &copied
	}
	revision := service.bumpLocked()
	service.mu.Unlock()
	if interaction == nil {
		service.events.Publish(EventInteractionClosed, revision, previousID, nil)
		return
	}
	service.events.Publish(EventInteractionOpened, revision, interaction.ID, interaction)
}

func (service *Service) openInteraction(interaction *Interaction) {
	if interaction == nil {
		return
	}
	service.mu.Lock()
	service.snapshot.Interaction = interaction
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventInteractionOpened, revision, interaction.ID, interaction)
}

func (service *Service) closeInteraction(id string) {
	service.mu.Lock()
	delete(service.replanInFlight, id)
	if service.snapshot.Interaction != nil && service.snapshot.Interaction.ID == id {
		service.snapshot.Interaction = nil
	}
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventInteractionClosed, revision, id, nil)
}

func (service *Service) sessionInteraction() *Interaction {
	sessions, _ := service.components.sessions.sessionCatalog()
	options := make([]InteractionOption, 0, len(sessions))
	for _, session := range sessions {
		label := session.Name
		if label == "" {
			label = shortSessionID(session.ID)
		}
		options = append(options, InteractionOption{ID: session.ID, Label: label, Description: fmt.Sprintf("tok:%d  %s", session.TokenCount, session.UpdatedAt.Format("01-02 15:04"))})
	}
	return &Interaction{ID: fmt.Sprintf("session-%d", time.Now().UnixNano()), Kind: "session", Title: "选择会话", Options: options, OpenedAt: time.Now()}
}

func (service *Service) accountInteraction() *Interaction {
	accounts := service.deps.Runtime.Accounts()
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
