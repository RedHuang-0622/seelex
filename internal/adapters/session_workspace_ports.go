package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	seelectxstorage "github.com/RedHuang-0622/Seele/seelectx/storage"
	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/plugin"
	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/session"
	"github.com/RedHuang-0622/seelex/sessionstore"
	"github.com/RedHuang-0622/seelex/skill"
	"github.com/RedHuang-0622/seelex/workspace"
)

type PluginPort struct{ Manager *plugin.Manager }

func (port PluginPort) Activate(ctx context.Context, name string) error {
	return port.Manager.Activate(ctx, name)
}
func (port PluginPort) Deactivate(ctx context.Context) error { return port.Manager.Deactivate(ctx) }
func (port PluginPort) Current() (model.PluginInfo, bool) {
	current, ok := port.Manager.Current()
	if !ok {
		return model.PluginInfo{}, false
	}
	return adaptPlugin(current), true
}
func (port PluginPort) All() []model.PluginInfo {
	plugins := port.Manager.All()
	result := make([]model.PluginInfo, 0, len(plugins))
	for _, item := range plugins {
		result = append(result, adaptPlugin(item))
	}
	return result
}

type WorkspacePort struct{ Repo *workspace.Repo }

func (port WorkspacePort) Create(name, rootPath, gitRemote string) (model.WorkspaceInfo, error) {
	w, err := port.Repo.Create(name, rootPath, gitRemote)
	if err != nil {
		return model.WorkspaceInfo{}, err
	}
	return adaptWorkspace(w), nil
}
func (port WorkspacePort) Get(id string) (model.WorkspaceInfo, error) {
	w, err := port.Repo.Get(id)
	if err != nil {
		return model.WorkspaceInfo{}, err
	}
	return adaptWorkspace(w), nil
}
func (port WorkspacePort) List() []model.WorkspaceInfo {
	list := port.Repo.List()
	out := make([]model.WorkspaceInfo, len(list))
	for i, w := range list {
		out[i] = adaptWorkspace(w)
	}
	return out
}
func (port WorkspacePort) Delete(id string) error { return port.Repo.Delete(id) }
func (port WorkspacePort) BindSession(sessionID, workspaceID string) {
	port.Repo.BindSession(sessionID, workspaceID)
}
func (port WorkspacePort) UnbindSession(sessionID string) {
	port.Repo.UnbindSession(sessionID)
}
func (port WorkspacePort) SessionWorkspace(sessionID string) (model.WorkspaceInfo, bool) {
	w, ok := port.Repo.SessionWorkspace(sessionID)
	if !ok {
		return model.WorkspaceInfo{}, false
	}
	return adaptWorkspace(w), true
}
func (port WorkspacePort) AllBindings() map[string]string {
	return port.Repo.AllBindings()
}
func (port WorkspacePort) DetectGitRemote(rootPath string) string {
	return workspace.DetectGitRemote(rootPath)
}

// WorkspaceTreePort 实现：转发给 Repo（工作树只读元数据；root/relPath 的
// containment 与忽略规则在 workspace 域内保证）。
func (port WorkspacePort) ListTree(root, relPath string, depth int) (dto.TreeListing, error) {
	return port.Repo.ListTree(root, relPath, depth)
}

func (port WorkspacePort) CountFiles(root string) (dto.TreeCount, error) {
	return port.Repo.CountFiles(root)
}

func (port WorkspacePort) GitLog(root string, limit int) (dto.GitLogResult, error) {
	return port.Repo.GitLog(root, limit)
}

func adaptWorkspace(item workspace.Info) model.WorkspaceInfo {
	return model.WorkspaceInfo{
		ID:        item.ID,
		Name:      workspaceDisplayName(item.RootPath, item.Name),
		RootPath:  item.RootPath,
		GitRemote: item.GitRemote,
	}
}

func workspaceDisplayName(rootPath, fallback string) string {
	cleaned := filepath.Clean(strings.TrimSpace(rootPath))
	name := strings.TrimSpace(filepath.Base(cleaned))
	if name == "" || name == "." || name == string(filepath.Separator) || name == "/" || name == `\` {
		return strings.TrimSpace(fallback)
	}
	return name
}

type SkillPort struct{ Registry *skill.Registry }

func (port SkillPort) Get(name string) (model.SkillInfo, bool) {
	item, ok := port.Registry.Get(name)
	if !ok {
		return model.SkillInfo{}, false
	}
	return adaptSkill(item), true
}
func (port SkillPort) All() []model.SkillInfo {
	skills := port.Registry.All()
	result := make([]model.SkillInfo, 0, len(skills))
	for _, item := range skills {
		result = append(result, adaptSkill(item))
	}
	return result
}

type SessionPort struct {
	Manager *session.Manager
	Runtime *seelebridge.Runtime
}

// AttachSessionContext 装配会话 context 模块（system prompt + 四栈）：
// 创建按 sessionID 的 SessionContextStore，加载持久化记录并挂接到 Runtime，
// 使下一轮 prompt 组装（stackBlocks）能使用持久化的 Plan/Task/Skill/Compact
// 栈。损坏/不兼容的 context 显式失败（不静默降级成内存栈）。
func (port SessionPort) AttachSessionContext(workspaceID, sessionID string) error {
	store := sessionstore.NewSessionContextStore(port.Manager.Router(), sessionID)
	if err := store.Load(context.Background()); err != nil {
		return fmt.Errorf("load session context %q: %w", sessionID, err)
	}
	port.Runtime.AttachSessionContextStore(store)
	return nil
}

// DetachSessionContext 解绑当前会话的 context 模块（离开会话时调用，
// Runtime 退回内存态，防止四栈串到下一个会话）。
func (port SessionPort) DetachSessionContext() {
	port.Runtime.AttachSessionContextStore(nil)
}

func (port SessionPort) SaveCurrent(id string) error     { return port.Manager.SaveCurrent(id) }
func (port SessionPort) Delete(id string) error          { return port.Manager.Delete(id) }
func (port SessionPort) Resume(id string) error          { return port.Manager.Resume(id) }
func (port SessionPort) SetWorkspace(workspaceID string) { port.Manager.SetWorkspace(workspaceID) }
func (port SessionPort) Workspace() string               { return port.Manager.Workspace() }
func (port SessionPort) StorageConfig() (sessionstore.Config, error) {
	return port.Manager.StorageConfig()
}
func (port SessionPort) TestStorage(ctx context.Context, config sessionstore.Config) error {
	return port.Manager.TestStorage(ctx, config)
}
func (port SessionPort) ConfigureStorage(ctx context.Context, config sessionstore.Config) error {
	return port.Manager.ConfigureStorage(ctx, config)
}
func (port SessionPort) LoadHistory(id string) ([]contract.EngineMessage, error) {
	messages, err := port.Manager.LoadHistory(id)
	if err != nil {
		return nil, err
	}
	return adaptMessages(messages), nil
}

func (port SessionPort) SaveSessionRecord(id string, record model.SessionRecord) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode session record: %w", err)
	}
	return port.Manager.SaveState(id, payload)
}

func (port SessionPort) SaveSessionSnapshot(
	id string,
	providerHistory []contract.EngineMessage,
	record model.SessionRecord,
	events []model.TranscriptEvent,
	results []model.StoredToolResult,
) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode session record: %w", err)
	}
	commit := sessionstore.Commit{
		ProviderHistory: restoreMessages(providerHistory),
		Events:          storeTranscriptEvents(events),
		State:           payload,
		ToolResults:     storeToolResults(results),
	}
	return port.Manager.SaveCommit(id, commit)
}

// LoadEventRangeWorkspace 按 EventSeq 范围读取事件流（fork 切断点解析用）。
func (port SessionPort) LoadEventRangeWorkspace(projectID, sessionID string, fromSeq, toSeq uint64) ([]sessionstore.Event, error) {
	return port.Manager.LoadEventRangeByWorkspace(projectID, sessionID, fromSeq, toSeq)
}

// LoadToolResultsWorkspace 枚举会话 tool-results 通道全部结果（fork 深拷贝
// 物理复制用）。
func (port SessionPort) LoadToolResultsWorkspace(projectID, sessionID string) ([]sessionstore.ToolResult, error) {
	return port.Manager.ListToolResultsByWorkspace(projectID, sessionID)
}

// SaveSessionSnapshotWorkspace 在显式项目作用域下原子写入会话快照
// （fork 深拷贝写入子会话键用；不改变 active write scope）。
func (port SessionPort) SaveSessionSnapshotWorkspace(
	projectID, sessionID string,
	providerHistory []contract.EngineMessage,
	record model.SessionRecord,
	events []model.TranscriptEvent,
	results []model.StoredToolResult,
) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode session record: %w", err)
	}
	commit := sessionstore.Commit{
		ProviderHistory: restoreMessages(providerHistory),
		Events:          storeTranscriptEvents(events),
		State:           payload,
		ToolResults:     storeToolResults(results),
	}
	return port.Manager.SaveCommitWorkspace(projectID, sessionID, commit)
}

// LoadContextStateWorkspace 读取会话 context 模块（显式项目作用域）。
func (port SessionPort) LoadContextStateWorkspace(projectID, sessionID string) ([]byte, error) {
	return port.Manager.LoadContextStateByWorkspace(projectID, sessionID)
}

// SaveContextStateWorkspace 保存会话 context 模块（显式项目作用域；fork
// 四栈深拷贝写入子会话用）。
func (port SessionPort) SaveContextStateWorkspace(projectID, sessionID string, state []byte) error {
	return port.Manager.SaveContextStateWorkspace(projectID, sessionID, state)
}

// CurrentGenerationWorkspace 返回会话当前已发布 generation（fork 血缘
// forked_from_generation 来源）。
func (port SessionPort) CurrentGenerationWorkspace(projectID, sessionID string) (string, error) {
	return port.Manager.CurrentGenerationWorkspace(projectID, sessionID)
}

func (port SessionPort) LoadTranscriptTailWorkspace(workspaceID, id string, tokenBudget, maxUnits int) ([]model.TranscriptEvent, error) {
	events, err := port.Manager.LoadEventTailByWorkspace(workspaceID, id, tokenBudget, maxUnits)
	if err != nil {
		return nil, err
	}
	return adaptTranscriptEvents(events), nil
}

func (port SessionPort) LoadToolResultWorkspace(workspaceID, id, resultRef string) (model.StoredToolResult, error) {
	result, err := port.Manager.LoadToolResultByWorkspace(workspaceID, id, resultRef)
	if err != nil {
		return model.StoredToolResult{}, err
	}
	return model.StoredToolResult{
		ToolResultRef: model.ToolResultRef{
			Ref: result.Ref, Tool: result.Tool, Digest: result.Digest, Size: result.Size,
			TokenCount: result.TokenCount, CreatedAt: result.CreatedAt,
		},
		Content: result.Content,
	}, nil
}

func storeTranscriptEvents(events []model.TranscriptEvent) []sessionstore.Event {
	stored := make([]sessionstore.Event, len(events))
	for index, event := range events {
		calls := make([]sessionstore.EventToolCall, len(event.ToolCalls))
		for callIndex, call := range event.ToolCalls {
			calls[callIndex] = sessionstore.EventToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments}
		}
		stored[index] = sessionstore.Event{
			Seq: event.Seq, TaskID: event.TaskID, MessageID: event.MessageID, Role: event.Role,
			ReasoningContent: event.ReasoningContent, Content: event.Content,
			ToolCallID: event.ToolCallID, Name: event.Name, ToolCalls: calls,
			ResultRef: event.ResultRef, TokenCount: event.TokenCount, CreatedAt: event.CreatedAt,
		}
	}
	return stored
}

func adaptTranscriptEvents(events []sessionstore.Event) []model.TranscriptEvent {
	adapted := make([]model.TranscriptEvent, len(events))
	for index, event := range events {
		calls := make([]model.TranscriptToolCall, len(event.ToolCalls))
		for callIndex, call := range event.ToolCalls {
			calls[callIndex] = model.TranscriptToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments}
		}
		adapted[index] = model.TranscriptEvent{
			Seq: event.Seq, TaskID: event.TaskID, MessageID: event.MessageID, Role: event.Role,
			ReasoningContent: event.ReasoningContent, Content: event.Content,
			ToolCallID: event.ToolCallID, Name: event.Name, ToolCalls: calls,
			ResultRef: event.ResultRef, TokenCount: event.TokenCount, CreatedAt: event.CreatedAt,
		}
	}
	return adapted
}

func storeToolResults(results []model.StoredToolResult) []sessionstore.ToolResult {
	stored := make([]sessionstore.ToolResult, len(results))
	for index, result := range results {
		stored[index] = sessionstore.ToolResult{
			Ref: result.Ref, Tool: result.Tool, Content: result.Content, Digest: result.Digest,
			Size: result.Size, TokenCount: result.TokenCount, CreatedAt: result.CreatedAt,
		}
	}
	return stored
}

func (port SessionPort) LoadSessionRecord(id string) (model.SessionRecord, error) {
	payload, err := port.Manager.LoadState(id)
	if err != nil {
		return model.SessionRecord{}, err
	}
	return decodeSessionRecord(payload, id)
}

func (port SessionPort) LoadSessionRecordWorkspace(workspaceID, id string) (model.SessionRecord, error) {
	payload, err := port.Manager.LoadStateByWorkspace(workspaceID, id)
	if err != nil {
		return model.SessionRecord{}, err
	}
	return decodeSessionRecord(payload, id)
}

func (port SessionPort) LoadConversationRangeWorkspace(workspaceID, id string, offset, limit int) ([]model.Message, int, error) {
	// conversation 模块冷读：只解析 state blob 的 conversation 子树，
	// 不反序列化 Plan/Execution/Projection 等非 conversation 模块
	// （模块化方案 plan.md §阶段1：长会话翻页不加载完整 state blob）。
	messages, total, err := port.Manager.LoadConversationRangeByWorkspace(workspaceID, id, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	adapted := make([]model.Message, 0, len(messages))
	for _, message := range messages {
		adapted = append(adapted, adaptStoredConversationMessage(message))
	}
	return adapted, total, nil
}

// adaptStoredConversationMessage 把存储层 conversation DTO 转为 UI 消息
// （Tool 指针独立拷贝，避免共享内部状态）。
func adaptStoredConversationMessage(message sessionstore.ConversationMessage) model.Message {
	adapted := model.Message{ID: message.ID, Role: message.Role, Content: message.Content, CreatedAt: message.CreatedAt}
	if message.Tool != nil {
		tool := *message.Tool
		adapted.Tool = &model.ToolCall{
			ID: tool.ID, Name: tool.Name, Arguments: tool.Arguments,
			Result: tool.Result, Error: tool.Error, Status: tool.Status, Duration: tool.Duration,
		}
	}
	return adapted
}

func decodeSessionRecord(payload []byte, sessionID string) (model.SessionRecord, error) {
	var version struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(payload, &version); err != nil {
		return model.SessionRecord{}, fmt.Errorf("decode session state header: %w", err)
	}
	if version.Version == 1 {
		var archive model.SessionArchive
		if err := json.Unmarshal(payload, &archive); err != nil {
			return model.SessionRecord{}, fmt.Errorf("decode legacy session archive: %w", err)
		}
		return migrateSessionArchive(sessionID, archive), nil
	}
	var record model.SessionRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return model.SessionRecord{}, fmt.Errorf("decode session record: %w", err)
	}
	if record.Version == 2 && record.ID == sessionID {
		return record, nil
	}
	if record.Version == 3 && record.ID == sessionID {
		return record, nil
	}
	return model.SessionRecord{}, fmt.Errorf("unsupported session state version %d", version.Version)
}

func migrateSessionArchive(sessionID string, archive model.SessionArchive) model.SessionRecord {
	record := model.SessionRecord{
		Version: 2,
		ID:      sessionID,
		Title: model.SessionTitle{
			Value:       archive.Name,
			Source:      "legacy_history",
			FinalizedAt: archive.UpdatedAt,
		},
		Conversation: model.ConversationRecord{
			Messages:  archive.Conversation,
			UpdatedAt: archive.UpdatedAt,
		},
		Execution: model.SessionExecutionRecord{
			Task:         archive.Task,
			ReadFiles:    archive.ReadFiles,
			Continuation: archive.Continuation,
		},
		UpdatedAt: archive.UpdatedAt,
	}
	if archive.Plan != nil || archive.PlanArguments != "" {
		record.ActivePlanID = "legacy-plan"
		record.PlanStack = []model.SessionPlanFrame{{
			ID:        record.ActivePlanID,
			Plan:      archive.Plan,
			Arguments: archive.PlanArguments,
			LoadedAt:  archive.UpdatedAt,
			UpdatedAt: archive.UpdatedAt,
		}}
	}
	return record
}
func (port SessionPort) LoadHistoryWorkspace(workspaceID, id string) ([]contract.EngineMessage, error) {
	messages, err := port.Manager.LoadHistoryByWorkspace(workspaceID, id)
	if err != nil {
		return nil, err
	}
	return adaptMessages(messages), nil
}
func (port SessionPort) LoadHistoryRange(id string, offset, limit int) ([]contract.EngineMessage, int, error) {
	messages, total, err := port.Manager.LoadHistoryRange(id, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	return adaptMessages(messages), total, nil
}
func (port SessionPort) LoadHistoryRangeWorkspace(workspaceID, id string, offset, limit int) ([]contract.EngineMessage, int, error) {
	messages, total, err := port.Manager.LoadHistoryRangeByWorkspace(workspaceID, id, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	return adaptMessages(messages), total, nil
}
func (port SessionPort) MessageCount(id string) (int, error) {
	return port.Manager.MessageCount(id)
}
func (port SessionPort) List() []model.SessionInfo {
	return adaptSessionMeta(port.Manager.List())
}
func (port SessionPort) ListWorkspace(workspaceID string) []model.SessionInfo {
	return adaptSessionMeta(port.Manager.ListByWorkspace(workspaceID))
}
func (port SessionPort) DeleteWorkspace(workspaceID, id string) error {
	return port.Manager.DeleteByWorkspace(workspaceID, id)
}

func adaptSessionMeta(sessions []seelectxstorage.SessionMeta) []model.SessionInfo {
	result := make([]model.SessionInfo, 0, len(sessions))
	for _, item := range sessions {
		result = append(result, model.SessionInfo{ID: item.SessionID, UpdatedAt: item.UpdatedAt, TokenCount: item.TokenCount})
	}
	return result
}

func adaptPlugin(item plugin.Plugin) model.PluginInfo {
	return model.PluginInfo{Name: item.Name, Description: item.Description, Prompt: item.Prompt}
}
func adaptSkill(item skill.Skill) model.SkillInfo {
	return model.SkillInfo{Name: item.Name, Description: item.Description, Prompt: item.Prompt}
}
