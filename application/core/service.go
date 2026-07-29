// Package core orchestrates application use cases while depending only on contracts.
package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/internal/promptassets"
)

const defaultHistoryWindow = 200

const maxReplansPerPlanChain = 2

var (
	ErrChatRunning         = errors.New("chat is already running")
	ErrApplicationDraining = errors.New("application is finishing active work")
)

type Service struct {
	mu                  sync.RWMutex
	sessionNameMu       sync.Mutex
	sessionTransitionMu sync.Mutex
	deps                Dependencies
	events              *EventHub
	approval            *ApprovalBroker
	commands            *CommandRegistry
	snapshot            Snapshot
	promptStack         *PromptStack
	effortManager       *EffortManager
	messageSeq          uint64
	cancelChat          context.CancelFunc
	idle                chan struct{}
	draining            bool
	closed              bool
	sessionNames        map[string]sessionNameCacheEntry
	replanInFlight      map[string]struct{}
	inputQueue          []chatRequest // 排队中的界面输入和模型输入
}

func New(deps Dependencies) *Service {
	if deps.Events == nil {
		deps.Events = NewEventHub()
	}
	if deps.Approval == nil {
		deps.Approval = NewApprovalBroker(deps.Events)
	}
	ps := NewPromptStack()
	service := &Service{
		deps: deps, events: deps.Events, approval: deps.Approval,
		commands: NewCommandRegistry(), promptStack: ps,
		sessionNames:   make(map[string]sessionNameCacheEntry),
		replanInFlight: make(map[string]struct{}),
	}
	service.effortManager = NewEffortManager(ps, deps.Engine)
	service.deps.Runtime.SetPlanPolicy(service.effortManager.PlanPolicy())
	service.idle = closedSignal()
	service.snapshot = Snapshot{
		ProtocolVersion: ProtocolVersion,
		Session:         SessionState{ID: deps.Engine.SessionID()},
		Runtime:         RuntimeState{Model: deps.Runtime.Model(), Effort: service.effortManager.Current()},
		Capabilities:    Capabilities{SessionResume: true},
	}
	service.registerBuiltinCommands()
	service.refreshRuntimeLocked(context.Background())
	if deps.Workspace != nil {
		service.refreshWorkspaceLocked()
		if workspace, ok := deps.Workspace.SessionWorkspace(service.snapshot.Session.ID); ok {
			if err := deps.Runtime.BindProjectRoot(workspace.RootPath); err == nil {
				deps.Sessions.SetWorkspace(workspace.ID)
				service.snapshot.CurrentWorkspace = &workspace
			}
		}
	}
	service.buildSystemPrompt()
	service.snapshot.Revision = 1
	service.approval.SetObserver(service.observeInteraction)
	return service
}

// buildSystemPrompt 组装完整的系统提示词并在引擎上生效。
// 层序: identity (固定) + plugins (插件 prompt) + effort (行为指令) + instructions (能力说明)。
// Skill 指令作为结构化用户上下文发送，不参与 system prompt。
func (service *Service) buildSystemPrompt() {
	service.promptStack.ClearKind("identity")
	service.promptStack.ClearKind("instructions")

	// 1. Identity — 始终在最底层
	service.promptStack.Push("identity", "identity", promptassets.SystemIdentity())

	// 2. Plugin prompt（从当前插件读取，已被 activateDefaultPlugin 激活）
	if current, ok := service.deps.Plugins.Current(); ok {
		// promptStack.Reset 会清除所有层重建 base
		// 所以用 Push 而不是 Reset：先清掉旧的 base，再推新的
		service.promptStack.ClearKind("base")
		if prompt := strings.TrimSpace(current.Prompt); prompt != "" {
			service.promptStack.Push("base", "plugin-"+current.Name, prompt)
		}
	}

	// 3. Effort（effortManager.Apply 内部会 Push "effort" 层）
	service.effortManager.Apply(service.effortManager.Current())

	// 4. Instructions — 只描述协议，不注入 Skill 名称、描述或指令。
	service.promptStack.Push("instructions", "instructions", promptassets.SystemInstructions())

	// 渲染并写入 engine
	service.deps.Engine.SetSystemPrompt(service.promptStack.Render())
}

func (service *Service) Snapshot() Snapshot {
	service.mu.RLock()
	snapshot := cloneSnapshot(service.snapshot)
	service.mu.RUnlock()
	sessions, discoveredBindings := service.sessionCatalog()
	snapshot.Sessions = sessions
	if snapshot.Session.Name == "" {
		for _, session := range sessions {
			if session.ID == snapshot.Session.ID {
				snapshot.Session.Name = session.Name
				break
			}
		}
	}
	if len(discoveredBindings) > 0 {
		if snapshot.SessionWorkspaces == nil {
			snapshot.SessionWorkspaces = make(map[string]string, len(discoveredBindings))
		}
		for sessionID, workspaceID := range discoveredBindings {
			snapshot.SessionWorkspaces[sessionID] = workspaceID
		}
	}
	return snapshot
}
func (service *Service) Subscribe(buffer int) Subscription { return service.events.Subscribe(buffer) }

func (service *Service) Submit(ctx context.Context, text string) error {
	service.mu.RLock()
	draining := service.draining
	closed := service.closed
	service.mu.RUnlock()
	if closed {
		return errors.New("application is shut down")
	}
	if draining {
		return ErrApplicationDraining
	}
	input := strings.TrimSpace(text)
	if input == "" {
		return nil
	}
	// 命令/Skill/插件 不排队，直接执行
	if strings.HasPrefix(input, "/") {
		return service.submitCommand(ctx, input)
	}
	if strings.HasPrefix(input, "#") {
		parts := strings.Fields(strings.TrimSpace(strings.TrimPrefix(input, "#")))
		if len(parts) == 0 {
			return nil
		}
		return service.submitSkill(ctx, parts[0], parts[1:], input)
	}
	if strings.HasPrefix(input, "@") {
		name := strings.TrimSpace(strings.TrimPrefix(input, "@"))
		if name == "" {
			return nil
		}
		return service.SwitchPlugin(ctx, name)
	}

	return service.submitConversation(ctx, input)
}

func (service *Service) submitConversation(ctx context.Context, input string) error {
	request := newChatRequest(input, service.promptStack.Layers())
	request.requirePlan = service.effortManager.PlanPolicy().RequirePlan
	service.sessionTransitionMu.Lock()
	defer service.sessionTransitionMu.Unlock()
	if err := service.materializeDraftSession(request.displayInput); err != nil {
		return err
	}
	service.mu.Lock()
	if service.closed {
		service.mu.Unlock()
		return errors.New("application is shut down")
	}
	if service.draining {
		service.mu.Unlock()
		return ErrApplicationDraining
	}
	if service.snapshot.Chat.Running {
		service.inputQueue = append(service.inputQueue, request)
		service.snapshot.Chat.InputQueue = chatRequestDisplays(service.inputQueue)
		service.snapshot.Chat.QueuedCount = len(service.inputQueue)
		revision := service.bumpLocked()
		service.mu.Unlock()
		service.events.Publish(EventSnapshotChanged, revision, "", nil)
		return nil
	}
	service.mu.Unlock()
	return service.startChat(ctx, request)
}

// BeginGracefulShutdown stops new user input while allowing the active chat
// and any input already queued behind it to finish naturally.
func (service *Service) BeginGracefulShutdown() {
	service.mu.Lock()
	service.draining = true
	service.mu.Unlock()
}

// WaitForIdle waits for all accepted chat work to finish. It never cancels an
// active chat; callers control abandonment through ctx.
func (service *Service) WaitForIdle(ctx context.Context) error {
	for {
		service.mu.RLock()
		idle := service.idle
		service.mu.RUnlock()
		select {
		case <-idle:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (service *Service) CancelChat(requestID string) bool {
	service.mu.Lock()
	defer service.mu.Unlock()
	if !service.snapshot.Chat.Running || (requestID != "" && requestID != service.snapshot.Chat.RequestID) || service.cancelChat == nil {
		return false
	}
	service.cancelChat()
	return true
}

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
			service.mu.Lock()
			service.appendMessageLocked("system", "节点失败，用户选择重试。请调用 plan_run 重新执行工作流。", nil)
			revision := service.bumpLocked()
			service.mu.Unlock()
			service.events.Publish(EventSnapshotChanged, revision, "", nil)
		case "skip":
			service.mu.Lock()
			service.appendMessageLocked("system", "节点失败，用户选择跳过。请修改工作流（plan_clear + plan_load）排除失败节点后重新 plan_run。", nil)
			revision := service.bumpLocked()
			service.mu.Unlock()
			service.events.Publish(EventSnapshotChanged, revision, "", nil)
		case "abort":
			service.mu.Lock()
			if plan := service.snapshot.Runtime.Plan; plan != nil {
				plan.Status = PlanAborted
				for i := range plan.Nodes {
					if plan.Nodes[i].Status == NodePending || plan.Nodes[i].Status == NodeRunning {
						plan.Nodes[i].Status = NodeAborted
					}
				}
			}
			service.appendMessageLocked("system", "工作流已终止。", nil)
			revision := service.bumpLocked()
			service.mu.Unlock()
			service.events.Publish(EventSnapshotChanged, revision, "", nil)
		}
	default:
		return fmt.Errorf("unsupported interaction kind %q", interaction.Kind)
	}
	service.closeInteraction(id)
	return nil
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
	service.snapshot.Runtime.PromptStack = service.promptStack.Describe()
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventSnapshotChanged, revision, "", nil)
	return nil
}

func (service *Service) SwitchPlugin(ctx context.Context, name string) error {
	if name == "off" || name == "none" || name == "" {
		if err := service.deps.Plugins.Deactivate(ctx); err != nil {
			return fmt.Errorf("停用插件失败: %w", err)
		}
		service.deps.Engine.ClearHistory()
		service.promptStack.Reset("")
		service.deps.Engine.SetSystemPrompt("")
		service.effortManager = NewEffortManager(service.promptStack, service.deps.Engine)
		service.resetConversation("已停用插件")
	} else {
		if err := service.deps.Plugins.Activate(ctx, name); err != nil {
			return fmt.Errorf("切换插件失败: %w", err)
		}
		service.deps.Engine.ClearHistory()
		if current, ok := service.deps.Plugins.Current(); ok {
			service.promptStack.Reset(strings.TrimSpace(current.Prompt))
		}
		service.effortManager = NewEffortManager(service.promptStack, service.deps.Engine)
		service.effortManager.Apply(service.effortManager.Current())
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

// ── Permissions ───────────────────────────────────────────────

func (service *Service) SetFullAccess(on bool) {
	service.deps.Runtime.SetFullAccess(on)
}

// ── Sessions ──────────────────────────────────────────────────

func (service *Service) DeleteSession(sessionID string) error {
	location := service.locateSession(sessionID)
	if scoped, ok := service.deps.Sessions.(scopedSessionPort); ok {
		if err := scoped.DeleteWorkspace(location.workspaceID, sessionID); err != nil {
			return err
		}
	} else if err := service.deps.Sessions.Delete(sessionID); err != nil {
		return err
	}
	if service.deps.Workspace != nil {
		service.deps.Workspace.UnbindSession(sessionID)
		service.mu.Lock()
		service.refreshWorkspaceLocked()
		service.bumpLocked()
		service.mu.Unlock()
	}
	service.invalidateSessionName(sessionID)
	return nil
}

// ── Workspace ─────────────────────────────────────────────────

func (service *Service) CreateWorkspace(name, rootPath, gitRemote string) error {
	if name == "" || rootPath == "" {
		return fmt.Errorf("workspace name and root path are required")
	}
	// 自动检测 git remote（如果调用方未提供）
	if gitRemote == "" {
		if detected := service.deps.Workspace.DetectGitRemote(rootPath); detected != "" {
			gitRemote = detected
		}
	}
	w, err := service.deps.Workspace.Create(name, rootPath, gitRemote)
	if err != nil {
		return err
	}
	return service.bindWorkspaceInfo(w)
}

func (service *Service) BindWorkspace(workspaceID string) error {
	w, err := service.deps.Workspace.Get(workspaceID)
	if err != nil {
		return err
	}
	return service.bindWorkspaceInfo(w)
}

func (service *Service) bindWorkspaceInfo(workspace WorkspaceInfo) error {
	service.sessionTransitionMu.Lock()
	defer service.sessionTransitionMu.Unlock()

	service.mu.RLock()
	if service.snapshot.Chat.Running {
		service.mu.RUnlock()
		return ErrChatRunning
	}
	currentSessionID := service.snapshot.Session.ID
	draft := service.snapshot.Session.Draft
	currentWorkspaceID := ""
	if service.snapshot.CurrentWorkspace != nil {
		currentWorkspaceID = service.snapshot.CurrentWorkspace.ID
	}
	service.mu.RUnlock()
	if draft {
		if err := service.deps.Runtime.BindProjectRoot(workspace.RootPath); err != nil {
			return err
		}
		service.deps.Sessions.SetWorkspace(workspace.ID)
		service.mu.Lock()
		service.snapshot.CurrentWorkspace = &WorkspaceInfo{ID: workspace.ID, Name: workspace.Name, RootPath: workspace.RootPath, GitRemote: workspace.GitRemote}
		service.refreshWorkspaceLocked()
		revision := service.bumpLocked()
		service.mu.Unlock()
		service.events.Publish(EventSnapshotChanged, revision, "", nil)
		return nil
	}

	history := service.deps.Engine.History()
	startFreshSession := currentWorkspaceID != workspace.ID && len(history) > 0
	if startFreshSession {
		writeWorkspaceID := currentWorkspaceID
		if writeWorkspaceID == "" {
			writeWorkspaceID = service.locateSession(currentSessionID).workspaceID
		}
		service.deps.Sessions.SetWorkspace(writeWorkspaceID)
		if err := service.deps.Sessions.SaveCurrent(currentSessionID); err != nil {
			return fmt.Errorf("save current session before switching project: %w", err)
		}
	}
	if err := service.deps.Runtime.BindProjectRoot(workspace.RootPath); err != nil {
		return err
	}
	if startFreshSession {
		currentSessionID = service.deps.Engine.StartSession()
		service.deps.Engine.SetSystemPrompt(service.promptStack.Render())
	}
	service.deps.Workspace.BindSession(currentSessionID, workspace.ID)
	service.deps.Sessions.SetWorkspace(workspace.ID)
	service.mu.Lock()
	if startFreshSession {
		service.snapshot.Session.ID = currentSessionID
		service.snapshot.Session.Name = ""
		service.snapshot.Conversation = nil
		service.snapshot.HistoryOffset = 0
		service.snapshot.TotalMessages = 0
		service.snapshot.HasMoreHistory = false
		service.snapshot.Runtime.Plan = nil
		service.snapshot.Interaction = nil
		service.appendMessageLocked("system", fmt.Sprintf("已切换到项目 %s，新建独立会话", workspace.Name), nil)
	}
	service.snapshot.CurrentWorkspace = &WorkspaceInfo{ID: workspace.ID, Name: workspace.Name, RootPath: workspace.RootPath, GitRemote: workspace.GitRemote}
	service.refreshWorkspaceLocked()
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventSnapshotChanged, revision, "", nil)
	return nil
}

func (service *Service) UnbindWorkspace() {
	service.sessionTransitionMu.Lock()
	defer service.sessionTransitionMu.Unlock()

	service.deps.Runtime.UnbindProjectRoot()
	service.mu.RLock()
	sessionID := service.snapshot.Session.ID
	draft := service.snapshot.Session.Draft
	service.mu.RUnlock()
	if !draft && sessionID != "" {
		service.deps.Workspace.UnbindSession(sessionID)
	}
	service.deps.Sessions.SetWorkspace("")
	service.mu.Lock()
	service.snapshot.CurrentWorkspace = nil
	service.refreshWorkspaceLocked()
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventSnapshotChanged, revision, "", nil)
}

func (service *Service) refreshWorkspaceLocked() {
	all := service.deps.Workspace.List()
	service.snapshot.Workspaces = make([]WorkspaceInfo, len(all))
	for i, w := range all {
		service.snapshot.Workspaces[i] = WorkspaceInfo{ID: w.ID, Name: w.Name, RootPath: w.RootPath, GitRemote: w.GitRemote}
	}
	service.snapshot.SessionWorkspaces = service.deps.Workspace.AllBindings()
}

func (service *Service) Shutdown() {
	service.mu.Lock()
	if service.closed {
		service.mu.Unlock()
		return
	}
	service.closed = true
	if service.cancelChat != nil {
		service.cancelChat()
	}
	service.mu.Unlock()
	service.approval.Shutdown()
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

func (service *Service) refreshRuntimeLocked(ctx context.Context) {
	service.snapshot.Session.ID = service.deps.Engine.SessionID()
	service.snapshot.Runtime.Model = service.deps.Runtime.Model()
	service.snapshot.Runtime.Provider = service.deps.Runtime.Provider()
	service.snapshot.Runtime.Plugin = service.deps.Runtime.ActivePlugin()
	service.snapshot.Runtime.Effort = service.effortManager.Current()
	service.snapshot.Runtime.PromptStack = service.promptStack.Describe()
	service.snapshot.Runtime.VisibleTools = append([]Tool(nil), service.deps.Runtime.VisibleTools(ctx)...)
	service.snapshot.Runtime.Skills = append([]SkillInfo(nil), service.deps.Skills.All()...)
	service.snapshot.Runtime.Tokens = service.deps.Engine.TokenCount()
	metrics := service.deps.Runtime.ReplanMetrics()
	service.snapshot.Runtime.Replan = ReplanMonitor{
		InFlight: metrics.InFlight, ConcurrentLimit: metrics.ConcurrentLimit,
		WindowAttempts: metrics.WindowAttempts, WindowLimit: metrics.WindowLimit,
		WindowStartedAt: metrics.WindowStartedAt, Accepted: metrics.Accepted,
		Succeeded: metrics.Succeeded, Failed: metrics.Failed, Rejected: metrics.Rejected,
		DuplicateRejected: metrics.DuplicateRejected, ProviderRequests: metrics.ProviderRequests,
		ProviderWindowRequests: metrics.ProviderWindowRequests, ProviderWindowLimit: metrics.ProviderWindowLimit,
	}
	service.snapshot.Runtime.Plugins = append([]PluginInfo(nil), service.deps.Plugins.All()...)
	service.snapshot.Runtime.Accounts = append([]AccountInfo(nil), service.deps.Runtime.Accounts()...)
}

func (service *Service) appendMessageLocked(role, content string, tool *ToolCall) *Message {
	service.messageSeq++
	message := Message{ID: fmt.Sprintf("message-%d", service.messageSeq), Role: role, Content: content, Tool: tool, CreatedAt: time.Now()}
	service.snapshot.Conversation = append(service.snapshot.Conversation, message)
	return &service.snapshot.Conversation[len(service.snapshot.Conversation)-1]
}

func (service *Service) bumpLocked() uint64 {
	service.snapshot.Revision++
	return service.snapshot.Revision
}

func (service *Service) addNotice(notice string) {
	if strings.TrimSpace(notice) == "" {
		return
	}
	service.mu.Lock()
	message := *service.appendMessageLocked("system", notice, nil)
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventMessageAdded, revision, "", message)
}

func (service *Service) resetConversation(notice string) {
	service.mu.Lock()
	service.snapshot.Conversation = nil
	service.snapshot.Session.Name = ""
	service.appendMessageLocked("system", fmt.Sprintf("Seele CLI — %s", service.deps.Runtime.Model()), nil)
	if notice != "" {
		service.appendMessageLocked("system", notice, nil)
	}
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventSnapshotChanged, revision, "", nil)
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
	sessions, _ := service.sessionCatalog()
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

func (service *Service) resumeSession(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session ID is required")
	}
	service.sessionTransitionMu.Lock()
	defer service.sessionTransitionMu.Unlock()

	service.mu.RLock()
	running := service.snapshot.Chat.Running
	service.mu.RUnlock()
	if running {
		return ErrChatRunning
	}

	location := service.locateSession(sessionID)
	history, err := service.loadSessionHistory(location, sessionID)
	if err != nil {
		return fmt.Errorf("load session %q: %w", sessionID, err)
	}
	if err := service.deps.Engine.ReplaceHistory(sessionID, history); err != nil {
		return fmt.Errorf("replace engine history: %w", err)
	}
	service.deps.Engine.SetSystemPrompt(service.promptStack.Render())

	total := len(history)
	offset := total - defaultHistoryWindow
	if offset < 0 {
		offset = 0
	}
	visibleHistory := history[offset:]
	currentWorkspace := location.workspace
	if service.deps.Workspace != nil {
		if currentWorkspace != nil {
			if err := service.deps.Runtime.BindProjectRoot(currentWorkspace.RootPath); err != nil {
				return fmt.Errorf("bind project root: %w", err)
			}
			service.deps.Sessions.SetWorkspace(currentWorkspace.ID)
			service.deps.Workspace.BindSession(sessionID, currentWorkspace.ID)
		} else {
			service.deps.Runtime.UnbindProjectRoot()
			service.deps.Sessions.SetWorkspace("")
			service.deps.Workspace.UnbindSession(sessionID)
		}
	}
	service.mu.Lock()
	service.snapshot.Session = SessionState{ID: sessionID, Name: sessionTitleFromHistory(history)}
	service.snapshot.Conversation = nil
	service.snapshot.Runtime.Plan = nil // 清除旧 Plan，避免跨会话残留
	service.snapshot.Interaction = nil  // 清除未完成的交互
	service.appendMessageLocked("system", "已恢复会话: "+sessionID, nil)
	service.appendHistoryLocked(visibleHistory)
	service.snapshot.HistoryOffset = offset
	service.snapshot.TotalMessages = total
	service.snapshot.HasMoreHistory = offset > 0
	// 恢复该会话绑定的工作区（每个会话独立的工作区上下文）
	if service.deps.Workspace != nil {
		service.snapshot.CurrentWorkspace = currentWorkspace
		service.refreshWorkspaceLocked()
	}
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventSnapshotChanged, revision, "", nil)
	return nil
}

// LoadMoreHistory 从存储中加载更早的消息，prepend 到 Conversation 头部。
// limit 为 0 时使用默认窗口大小。
func (service *Service) LoadMoreHistory(limit int) error {
	if limit <= 0 {
		limit = defaultHistoryWindow
	}

	service.mu.RLock()
	offset := service.snapshot.HistoryOffset
	sessionID := service.snapshot.Session.ID
	service.mu.RUnlock()

	if offset <= 0 {
		return nil // 已到最早
	}

	loadOffset := offset - limit
	if loadOffset < 0 {
		loadOffset = 0
	}
	loadLimit := offset - loadOffset

	workspaceID := ""
	service.mu.RLock()
	if service.snapshot.CurrentWorkspace != nil {
		workspaceID = service.snapshot.CurrentWorkspace.ID
	}
	service.mu.RUnlock()
	history, total, err := service.loadSessionHistoryRange(workspaceID, sessionID, loadOffset, loadLimit)
	if err != nil {
		return fmt.Errorf("load history range: %w", err)
	}

	adapted := make([]Message, 0, len(history))
	for _, msg := range history {
		adapted = append(adapted, adaptEngineMessage(msg))
	}

	service.mu.Lock()
	for index := range adapted {
		service.messageSeq++
		adapted[index].ID = fmt.Sprintf("message-%d", service.messageSeq)
	}
	service.snapshot.Conversation = append(adapted, service.snapshot.Conversation...)
	service.snapshot.HistoryOffset = loadOffset
	service.snapshot.TotalMessages = total
	service.snapshot.HasMoreHistory = loadOffset > 0
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventSnapshotChanged, revision, "", nil)
	return nil
}

// adaptEngineMessage 将 EngineMessage 转为本包 Message（由 SessionPort 返回时已是 EngineMessage）。
func adaptEngineMessage(msg EngineMessage) Message {
	content := msg.Content
	if msg.Role == "user" {
		content = displayUserInput(content)
	}
	m := Message{Role: msg.Role, Content: content}
	for _, tc := range msg.ToolCalls {
		m.Tool = &ToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments, Status: "success"}
	}
	return m
}
