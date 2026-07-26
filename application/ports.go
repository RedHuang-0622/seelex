package application

import "context"

type EngineMessage struct {
	Role             string
	ReasoningContent string
	Content          string
	ContentSet       bool
	ToolCallID       string
	Name             string
	ToolCalls        []EngineToolCall
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
	ReplaceHistory(string, []EngineMessage) error
	SessionID() string
	StartSession() string
	SetSystemPrompt(string)
	SetMaxLoops(int)
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
	SetFullAccess(bool)
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
	Delete(string) error
	List() []SessionInfo
	LoadHistory(string) ([]EngineMessage, error)
	// LoadHistoryRange 按偏移量窗口加载，返回 [offset, offset+limit) 和总数。
	LoadHistoryRange(sessionID string, offset, limit int) ([]EngineMessage, int, error)
}

type WorkspaceInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	RootPath  string `json:"root_path"`
	GitRemote string `json:"git_remote,omitempty"`
}

type WorkspacePort interface {
	Create(name, rootPath, gitRemote string) (WorkspaceInfo, error)
	Get(id string) (WorkspaceInfo, error)
	List() []WorkspaceInfo
	Delete(id string) error
	BindSession(sessionID, workspaceID string)
	UnbindSession(sessionID string)
	SessionWorkspace(sessionID string) (WorkspaceInfo, bool)
	AllBindings() map[string]string
	DetectGitRemote(rootPath string) string
}

type Dependencies struct {
	Engine    ChatEngine
	Runtime   RuntimePort
	Plugins   PluginPort
	Skills    SkillPort
	Sessions  SessionPort
	Workspace WorkspacePort
	Events    *EventHub
	Approval  *ApprovalBroker
}
