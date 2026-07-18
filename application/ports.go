package application

import "context"

type EngineMessage struct {
	Role      string
	Content   string
	Name      string
	ToolCalls []EngineToolCall
}
type EngineToolCall struct {
	ID        string
	Name      string
	Arguments string
}
type ChatEngine interface {
	ChatStream(context.Context, string, func(string)) (string, error)
	History() []EngineMessage
	ClearHistory()
	SessionID() string
	SetSystemPrompt(string)
	TraceText() string
	TokenCount() string
}
type RuntimePort interface {
	Model() string
	Provider() string
	Accounts() []AccountInfo
	SelectAccount(string) bool
	VisibleTools(context.Context) []Tool
	ActivePlugin() string
}
type PluginPort interface {
	All() []PluginInfo
	Activate(context.Context, string) error
	Deactivate(context.Context) error
	Current() (PluginInfo, bool)
}
type SkillPort interface {
	All() []SkillInfo
	Get(string) (SkillInfo, bool)
}
type SessionPort interface {
	SaveCurrent(string) error
	Resume(string) error
	List() []SessionInfo
	LoadHistory(string) ([]EngineMessage, error)
	// LoadHistoryRange 按偏移量窗口加载，返回 [offset, offset+limit) 和总数。
	LoadHistoryRange(sessionID string, offset, limit int) ([]EngineMessage, int, error)
	// MessageCount 返回会话总消息数。
	MessageCount(sessionID string) (int, error)
}
type Dependencies struct {
	Engine   ChatEngine
	Runtime  RuntimePort
	Plugins  PluginPort
	Skills   SkillPort
	Sessions SessionPort
	Events   *EventHub
	Approval *ApprovalBroker
}
