// Package contract defines the application-owned interfaces for external systems.
package contract

import (
	"context"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/approval"
	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/seelebridge"
)

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
	// AppendHistory 追加消息到引擎内部对话历史。
	// 仅在 OnIterationComplete 回调（ChatStream 同 goroutine）中调用，不加锁。
	AppendHistory(msg types.Message)
}
type RuntimePort interface {
	Model() string
	Provider() string
	Accounts() []model.AccountInfo
	SelectAccount(string) bool
	VisibleTools(context.Context) []model.Tool
	ActivePlugin() string
	SetFullAccess(bool)
	SetPlanPolicy(seelebridge.PlanPolicy)
	AcquirePlanActScope(string) (seelebridge.PlanActScope, error)
	PreparePlan(context.Context, string) (seelebridge.PlanPreflight, error)
	PrepareReplan(context.Context, seelebridge.ReplanRequest) (seelebridge.PlanPreflight, error)
	ReplanMetrics() seelebridge.ReplanMetrics
	SetPlanBranchBinding(seelebridge.PlanBranchBinding)
	BindProjectRoot(rootPath string) error
	UnbindProjectRoot()
}
type PluginPort interface {
	All() []model.PluginInfo
	Activate(context.Context, string) error
	Deactivate(context.Context) error
	Current() (model.PluginInfo, bool)
}
type SkillPort interface {
	All() []model.SkillInfo
	Get(string) (model.SkillInfo, bool)
}
type SessionPort interface {
	SaveCurrent(string) error
	Delete(string) error
	List() []model.SessionInfo
	LoadHistory(string) ([]EngineMessage, error)
	// LoadHistoryRange 按偏移量窗口加载，返回 [offset, offset+limit) 和总数。
	LoadHistoryRange(sessionID string, offset, limit int) ([]EngineMessage, int, error)
	// SetWorkspace routes subsequent Save/Load/List to the workspace directory.
	SetWorkspace(workspaceID string)
	// Workspace returns the active workspace ID ("" = default).
	Workspace() string
}

type WorkspacePort interface {
	Create(name, rootPath, gitRemote string) (model.WorkspaceInfo, error)
	Get(id string) (model.WorkspaceInfo, error)
	List() []model.WorkspaceInfo
	Delete(id string) error
	BindSession(sessionID, workspaceID string)
	UnbindSession(sessionID string)
	SessionWorkspace(sessionID string) (model.WorkspaceInfo, bool)
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
	Events    *event.EventHub
	Approval  *approval.ApprovalBroker
}
