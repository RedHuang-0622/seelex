package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const defaultHistoryWindow = 200

var ErrChatRunning = errors.New("chat is already running")

type Service struct {
	mu            sync.RWMutex
	deps          Dependencies
	events        *EventHub
	approval      *ApprovalBroker
	commands      *CommandRegistry
	snapshot      Snapshot
	promptStack   *PromptStack
	effortManager *EffortManager
	messageSeq    uint64
	cancelChat    context.CancelFunc
	closed        bool
	inputQueue    []string // 排队中的输入
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
	}
	service.effortManager = NewEffortManager(ps, deps.Engine)
	service.snapshot = Snapshot{
		Session:      SessionState{ID: deps.Engine.SessionID()},
		Runtime:      RuntimeState{Model: deps.Runtime.Model(), Effort: service.effortManager.Current()},
		Capabilities: Capabilities{SessionResume: false, SessionResumeReason: "Seele engine does not expose history replacement"},
	}
	service.registerBuiltinCommands()
	service.refreshRuntimeLocked(context.Background())
	service.buildSystemPrompt()
	service.appendMessageLocked("system", fmt.Sprintf("Seele CLI — %s", deps.Runtime.Model()), nil)
	service.snapshot.Revision = 1
	service.approval.setObserver(service.observeInteraction)
	return service
}

// buildSystemPrompt 组装完整的系统提示词并在引擎上生效。
// 层序: identity (固定) + effort (行为指令) + plugins (插件 prompt) + instructions (切换说明) + skill (可选)
func (service *Service) buildSystemPrompt() {
	service.promptStack.ClearKind("identity")
	service.promptStack.ClearKind("instructions")

	// 1. Identity — 始终在最底层
	service.promptStack.Push("identity", "identity",
		"You are Seelex, an intelligent engineering agent built on the Seele framework. "+
			"You can switch plugins, load skills, and use tools to solve engineering tasks.")

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

	// 4. Instructions — 固定在 effort/plugin 之上、skill 之下
	// 将可用 Skill 列表注入 system prompt，使 LLM 知道有哪些技能可以建议用户加载
	instructions := `## System Capabilities
- Use switch_plugin tool to switch between plugins for different tool sets.
- Load skills via #skillname (user-triggered). Use "#end" to unload current skill.
- Current effort determines thinking depth and tool usage intensity.`
	if skills := service.deps.Skills.All(); len(skills) > 0 {
		instructions += "\n\n### Available Skills\n"
		for _, sk := range skills {
			line := "  - #" + sk.Name
			if sk.Description != "" {
				line += ": " + sk.Description
			}
			instructions += line + "\n"
		}
	}
	service.promptStack.Push("instructions", "instructions", instructions)

	// 渲染并写入 engine
	service.deps.Engine.SetSystemPrompt(service.promptStack.Render())
}

func (service *Service) Snapshot() Snapshot {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return cloneSnapshot(service.snapshot)
}
func (service *Service) Subscribe(buffer int) Subscription { return service.events.Subscribe(buffer) }

func (service *Service) Submit(ctx context.Context, text string) error {
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
		return service.submitSkill(parts[0], parts[1:])
	}
	if strings.HasPrefix(input, "@") {
		name := strings.TrimSpace(strings.TrimPrefix(input, "@"))
		if name == "" {
			return nil
		}
		return service.SwitchPlugin(ctx, name)
	}

	// 对话输入：如果正在运行则排队
	service.mu.Lock()
	if service.snapshot.Chat.Running {
		service.inputQueue = append(service.inputQueue, input)
		service.snapshot.Chat.InputQueue = append([]string(nil), service.inputQueue...)
		service.snapshot.Chat.QueuedCount = len(service.inputQueue)
		revision := service.bumpLocked()
		service.mu.Unlock()
		service.events.Publish(EventSnapshotChanged, revision, "", nil)
		return nil
	}
	service.mu.Unlock()
	return service.startChat(ctx, input)
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
		service.resetConversation("已切换到 " + name + " 插件")
	}
	service.mu.Lock()
	service.refreshRuntimeLocked(ctx)
	revision := service.bumpLocked()
	runtime := service.snapshot.Runtime
	service.mu.Unlock()
	service.events.Publish(EventRuntimeChanged, revision, "", runtime)
	return nil
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
	if service.snapshot.Interaction != nil && service.snapshot.Interaction.ID == id {
		service.snapshot.Interaction = nil
	}
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventInteractionClosed, revision, id, nil)
}

func (service *Service) sessionInteraction() *Interaction {
	sessions := service.deps.Sessions.List()
	options := make([]InteractionOption, 0, len(sessions))
	for _, session := range sessions {
		label := session.ID
		if len(label) > 16 {
			label = label[:16]
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
	if err := service.deps.Sessions.Resume(sessionID); err != nil {
		return err
	}
	total, err := service.deps.Sessions.MessageCount(sessionID)
	if err != nil {
		return err
	}
	// 窗口加载：只取最后 defaultHistoryWindow 条，其余用 LoadMoreHistory 按需拉。
	offset := total - defaultHistoryWindow
	if offset < 0 {
		offset = 0
	}
	history, _, err := service.deps.Sessions.LoadHistoryRange(sessionID, offset, defaultHistoryWindow)
	if err != nil {
		// 降级：全量加载
		history, err = service.deps.Sessions.LoadHistory(sessionID)
		if err != nil {
			return err
		}
		offset = 0
		total = len(history)
	}
	service.deps.Engine.ClearHistory()
	service.mu.Lock()
	service.snapshot.Conversation = nil
	service.appendMessageLocked("system", "已恢复会话: "+sessionID, nil)
	service.appendHistoryLocked(history)
	service.snapshot.HistoryOffset = offset
	service.snapshot.TotalMessages = total
	service.snapshot.HasMoreHistory = offset > 0
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

	history, total, err := service.deps.Sessions.LoadHistoryRange(sessionID, loadOffset, loadLimit)
	if err != nil {
		return fmt.Errorf("load history range: %w", err)
	}

	adapted := make([]Message, 0, len(history))
	for _, msg := range history {
		adapted = append(adapted, adaptEngineMessage(msg))
	}

	service.mu.Lock()
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
	m := Message{Role: msg.Role, Content: msg.Content}
	for _, tc := range msg.ToolCalls {
		m.Tool = &ToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments, Status: "success"}
	}
	return m
}
