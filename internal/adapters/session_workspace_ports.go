package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

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

// WorkspaceFilePort 实现：转发给 Repo（文件预览读取；root/relPath
// containment、敏感过滤与上限在 workspace 域内保证）。
func (port WorkspacePort) ReadFile(root, relPath string, limit int64) (dto.FileContent, error) {
	return port.Repo.ReadFile(root, relPath, limit)
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
	// workspaceResolver 解析会话绑定的项目（workspace.Repo 装配注入；
	// Delete/LoadHistory 等会话级操作按归属项目落键，避免删错/读错作用域）。
	workspaceResolver func(sessionID string) string
	// projectSource 提供应用已知项目列表（workspace.Repo.List）：绑定缺失时
	// 归属解析按"数据实际所在"定位，未找到即未关联（默认项目）。
	projectSource func() []string
	// Meta 是项目级会话展示元数据存取（main.go 装配）。它是指针：SessionPort
	// 会被值拷贝进 Dependencies，共享同一个实例才共用其读改写串行化。
	Meta *sessionstore.SessionMetaStore
}

// SetSessionMeta 实现 session.SessionMetaPort：写单个会话的展示元数据到其归属
// 项目 blob（零值语义即清除条目）。
func (port SessionPort) SetSessionMeta(sessionID string, meta model.SessionMeta) error {
	if port.Meta == nil {
		return errors.New("session meta storage is not assembled")
	}
	projectID := port.granular().ResolveProjectForSession(sessionID)
	_, err := port.Meta.Set(projectID, sessionID, adaptSessionMetaToStore(meta))
	return err
}

// SessionMeta 实现 session.SessionMetaPort：读单个会话的展示元数据。
func (port SessionPort) SessionMeta(sessionID string) (model.SessionMeta, error) {
	if port.Meta == nil {
		return model.SessionMeta{}, errors.New("session meta storage is not assembled")
	}
	projectID := port.granular().ResolveProjectForSession(sessionID)
	metas, err := port.Meta.Meta(projectID)
	if err != nil {
		return model.SessionMeta{}, err
	}
	return adaptSessionMetaFromStore(metas[sessionID]), nil
}

// applySessionMetas 把项目 blob 里的展示元数据盖到目录枚举结果上。缺 blob 或
// 读失败都不阻断目录：元数据只影响排序与标题显示。
func (port SessionPort) applySessionMetas(projectID string, infos []model.SessionInfo) []model.SessionInfo {
	if port.Meta == nil {
		return infos
	}
	metas, err := port.Meta.Meta(projectID)
	if err != nil || len(metas) == 0 {
		return infos
	}
	for index := range infos {
		infos[index].Meta = adaptSessionMetaFromStore(metas[infos[index].ID])
	}
	return infos
}

func adaptSessionMetaToStore(meta model.SessionMeta) sessionstore.SessionDisplayMeta {
	return sessionstore.SessionDisplayMeta{Pinned: meta.Pinned, Alias: meta.Alias, SortOrder: meta.SortOrder}
}

func adaptSessionMetaFromStore(meta sessionstore.SessionDisplayMeta) model.SessionMeta {
	return model.SessionMeta{Pinned: meta.Pinned, Alias: meta.Alias, SortOrder: meta.SortOrder}
}

// SetWorkspaceResolver 注入会话绑定项目解析器（main.go 装配点）。
func (port *SessionPort) SetWorkspaceResolver(resolver func(sessionID string) string) {
	if port == nil {
		return
	}
	port.workspaceResolver = resolver
}

// SetProjectSource 注入已知项目列表（main.go 装配点）。
func (port *SessionPort) SetProjectSource(source func() []string) {
	if port == nil {
		return
	}
	port.projectSource = source
}

// granular 返回会话粒度存储入口（Router 为物理布局，暴露层为
// session:<id> 五片 API；session.Manager 不再承担存储桥）。
func (port SessionPort) granular() *sessionstore.SessionGranularStore {
	store := sessionstore.NewSessionGranularStore(port.Manager.Router())
	store.SetWorkspaceResolver(port.workspaceResolver)
	store.SetProjectSource(port.projectSource)
	return store
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

// Delete 以会话粒度删除会话（resolve 项目绑定后整键删除）。
func (port SessionPort) Delete(id string) error {
	granular := port.granular()
	return granular.Delete(granular.ResolveProjectForSession(id), id)
}

func (port SessionPort) LoadHistory(id string) ([]contract.EngineMessage, error) {
	granular := port.granular()
	messages, err := granular.HistoryForProject(granular.ResolveProjectForSession(id), id).Load(context.Background())
	if err != nil {
		return nil, err
	}
	return adaptMessages(messages), nil
}

func (port SessionPort) LoadHistoryRange(id string, offset, limit int) ([]contract.EngineMessage, int, error) {
	granular := port.granular()
	messages, total, err := granular.HistoryRange(granular.ResolveProjectForSession(id), id, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	return adaptMessages(messages), total, nil
}

func (port SessionPort) MessageCount(id string) (int, error) {
	_, total, err := port.LoadHistoryRange(id, 0, 0)
	return total, err
}

// List 返回项目索引下的会话列表（project = 会话集合）。
func (port SessionPort) List() []model.SessionInfo {
	// List 语义 = 当前写作用域下的会话（与 Router active scope 一致）；
	// 目录全量枚举（含默认项目）走 SessionGranularPort.SessionsOf。
	infos, err := port.granular().SessionsOf(port.Manager.Workspace())
	if err != nil {
		return nil
	}
	return port.applySessionMetas(port.Manager.Workspace(), adaptGranularInfos(infos))
}

// SessionsOf 实现 session_runtime.SessionGranularPort：按项目索引枚举会话。
func (port SessionPort) SessionsOf(projectID string) []model.SessionInfo {
	infos, err := port.granular().SessionsOf(projectID)
	if err != nil {
		return nil
	}
	return port.applySessionMetas(projectID, adaptGranularInfos(infos))
}

// EnsureSessionIndexed 在项目索引尚无该会话时生成一次空 commit，使
// record-only 工作区草稿能被按项目枚举找回（G：草稿 binding 落盘）。
func (port SessionPort) EnsureSessionIndexed(projectID, sessionID string) error {
	return port.granular().EnsureIndexed(projectID, sessionID)
}

func (port SessionPort) SaveSessionRecord(id string, record model.SessionRecord) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode session record: %w", err)
	}
	return port.granular().SaveRecordRaw("", id, payload)
}

// SaveSessionRecordWorkspace 在显式项目作用域下写会话 record（物理布局
// 显式键；会话粒度存储入口统一经 SessionGranularStore）。
func (port SessionPort) SaveSessionRecordWorkspace(projectID, id string, record model.SessionRecord) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode session record: %w", err)
	}
	return port.granular().SaveRecordRaw(projectID, id, payload)
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
	return port.granular().SaveCommit("", id, commit)
}

// LoadEventRangeWorkspace 按 EventSeq 范围读取事件流（fork 切断点解析用）。
func (port SessionPort) LoadEventRangeWorkspace(projectID, sessionID string, fromSeq, toSeq uint64) ([]sessionstore.Event, error) {
	return port.granular().EventRange(projectID, sessionID, fromSeq, toSeq)
}

// LoadToolResultsWorkspace 枚举会话 tool-results 通道全部结果（fork 深拷贝
// 物理复制用）。
func (port SessionPort) LoadToolResultsWorkspace(projectID, sessionID string) ([]sessionstore.ToolResult, error) {
	return port.granular().ListToolResults(projectID, sessionID)
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
	return port.granular().SaveCommit(projectID, sessionID, commit)
}

// LoadContextStateWorkspace 读取会话 context 模块（显式项目作用域）。
func (port SessionPort) LoadContextStateWorkspace(projectID, sessionID string) ([]byte, error) {
	return port.granular().LoadContextRaw(projectID, sessionID)
}

// SaveContextStateWorkspace 保存会话 context 模块（显式项目作用域；fork
// 四栈深拷贝写入子会话用）。
func (port SessionPort) SaveContextStateWorkspace(projectID, sessionID string, state []byte) error {
	return port.granular().SaveContext(projectID, sessionID, state)
}

// CurrentGenerationWorkspace 返回会话当前已发布 generation（fork 血缘
// forked_from_generation 来源）。
func (port SessionPort) CurrentGenerationWorkspace(projectID, sessionID string) (string, error) {
	return port.granular().CurrentGeneration(projectID, sessionID)
}

func (port SessionPort) LoadTranscriptTailWorkspace(workspaceID, id string, tokenBudget, maxUnits int) ([]model.TranscriptEvent, error) {
	events, err := port.granular().TranscriptTail(workspaceID, id, tokenBudget, maxUnits)
	if err != nil {
		return nil, err
	}
	return adaptTranscriptEvents(events), nil
}

// AssembleWireHistoryWorkspace 实现 session_runtime.SessionWireAssemblerPort：
// v8 会话走 R2 装配（compact 摘要 + 尾窗 + 最近 K 条尝试）；非 v8/后端返回
// ok=false，装配方回退旧链路。
func (port SessionPort) AssembleWireHistoryWorkspace(projectID, sessionID string, budget, k int) ([]contract.EngineMessage, bool, error) {
	if port.Manager == nil || port.Manager.Router() == nil {
		return nil, false, nil
	}
	messages, ok, err := port.Manager.Router().AssembleWireWorkspace(projectID, sessionID, budget, k)
	if err != nil || !ok {
		return nil, ok, err
	}
	return adaptMessages(messages), true, nil
}

// LifecycleRecoverWorkspace 实现 session_runtime.SessionLifecycleRecoverPort。
func (port SessionPort) LifecycleRecoverWorkspace(projectID, sessionID string) (int, bool, error) {
	if port.Manager == nil || port.Manager.Router() == nil {
		return 0, false, nil
	}
	return port.Manager.Router().LifecycleRecoverWorkspace(projectID, sessionID)
}

func (port SessionPort) LoadToolResultWorkspace(workspaceID, id, resultRef string) (model.StoredToolResult, error) {
	result, err := port.granular().ToolResult(workspaceID, id, resultRef)
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
			Kind: event.Kind, ReasoningContent: event.ReasoningContent, Content: event.Content,
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
			Kind: event.Kind, ReasoningContent: event.ReasoningContent, Content: event.Content,
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
	granular := port.granular()
	payload, err := granular.LoadRecordRaw(granular.ResolveProjectForSession(id), id)
	if err != nil {
		return model.SessionRecord{}, err
	}
	return decodeSessionRecord(payload, id)
}

func (port SessionPort) LoadSessionRecordWorkspace(workspaceID, id string) (model.SessionRecord, error) {
	payload, err := port.granular().LoadRecordRaw(workspaceID, id)
	if err != nil {
		return model.SessionRecord{}, err
	}
	return decodeSessionRecord(payload, id)
}

func (port SessionPort) LoadConversationRangeWorkspace(workspaceID, id string, offset, limit int) ([]model.Message, int, error) {
	// conversation 模块冷读：只解析 state blob 的 conversation 子树，
	// 不反序列化 Plan/Execution/Projection 等非 conversation 模块
	// （模块化方案 plan.md §阶段1：长会话翻页不加载完整 state blob）。
	messages, total, err := port.granular().ConversationRange(workspaceID, id, offset, limit)
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
func adaptGranularInfos(sessions []sessionstore.SessionInfo) []model.SessionInfo {
	result := make([]model.SessionInfo, 0, len(sessions))
	for _, item := range sessions {
		result = append(result, model.SessionInfo{
			ID:     item.ID,
			Name:   item.Title,
			Status: model.SessionStatus(item.Status),
			// 时间线字段随枚举摘要透传：目录行日期/token 必须来自 manifest
			// 真实值，不能在此置零（否则快照里 updated_at 恒为
			// 0001-01-01T00:00:00Z，侧栏日期退化成占位符）。
			UpdatedAt:  item.UpdatedAt,
			TokenCount: item.TokenCount,
		})
	}
	return result
}

func adaptPlugin(item plugin.Plugin) model.PluginInfo {
	return model.PluginInfo{Name: item.Name, Description: item.Description, Prompt: item.Prompt}
}
func adaptSkill(item skill.Skill) model.SkillInfo {
	return model.SkillInfo{Name: item.Name, Description: item.Description, Prompt: item.Prompt}
}
