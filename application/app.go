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
	mu         sync.RWMutex
	deps       Dependencies
	events     *EventHub
	approval   *ApprovalBroker
	commands   *CommandRegistry
	snapshot   Snapshot
	messageSeq uint64
	cancelChat context.CancelFunc
	closed     bool
}

func New(deps Dependencies) *Service {
	if deps.Events == nil {
		deps.Events = NewEventHub()
	}
	if deps.Approval == nil {
		deps.Approval = NewApprovalBroker(deps.Events)
	}
	service := &Service{deps: deps, events: deps.Events, approval: deps.Approval, commands: NewCommandRegistry()}
	service.snapshot = Snapshot{
		Session:      SessionState{ID: deps.Engine.SessionID()},
		Runtime:      RuntimeState{Model: deps.Runtime.Model()},
		Capabilities: Capabilities{SessionResume: false, SessionResumeReason: "Seele engine does not expose history replacement"},
	}
	service.registerBuiltinCommands()
	service.refreshRuntimeLocked(context.Background())
	service.appendMessageLocked("system", fmt.Sprintf("Seele CLI — %s", deps.Runtime.Model()), nil)
	service.snapshot.Revision = 1
	service.approval.setObserver(service.observeInteraction)
	return service
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

func (service *Service) SwitchPlugin(ctx context.Context, name string) error {
	if name == "off" || name == "none" || name == "" {
		if err := service.deps.Plugins.Deactivate(ctx); err != nil {
			return fmt.Errorf("停用插件失败: %w", err)
		}
		service.deps.Engine.SetSystemPrompt("")
	} else {
		if err := service.deps.Plugins.Activate(ctx, name); err != nil {
			return fmt.Errorf("切换插件失败: %w", err)
		}
		if current, ok := service.deps.Plugins.Current(); ok {
			service.deps.Engine.SetSystemPrompt(strings.TrimSpace(current.Prompt))
		}
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
