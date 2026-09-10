package adapters

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
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
	if err := port.granular().SaveCommit(projectID, sessionID, commit); err != nil {
		return err
	}
	// fork 子会话的三栈由 §2.4 栈通道按 message 锚重建（context blob 不再
	// 携带栈）；失败必须显式报错，不能留下丢栈的子会话。
	if ref := record.ForkedFrom; ref != nil {
		if err := port.Manager.Router().ForkStacks(
			ref.ParentWorkspaceID, ref.ParentSessionID, projectID, sessionID, ref.ForkPoint.EventSeq,
		); err != nil {
			return fmt.Errorf("fork session stacks: %w", err)
		}
	}
	return nil
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

// ---------- R2/R4 群聊角色会话适配（Application 可选能力面） ----------

func (port SessionPort) roleRouter(mainSessionID string) (*sessionstore.Router, string, error) {
	if port.Manager == nil || port.Manager.Router() == nil {
		return nil, "", errors.New("session storage is not assembled")
	}
	if strings.TrimSpace(mainSessionID) == "" {
		return nil, "", errors.New("main session ID is required")
	}
	projectID := port.granular().ResolveProjectForSession(mainSessionID)
	return port.Manager.Router(), projectID, nil
}

// ── R2/R4 角色会话与定时插话（S27：端口签名只用 dto 纯 DTO）────────────

func (port SessionPort) CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (dto.RoleSessionInfo, error) {
	router, projectID, err := port.roleRouter(mainSessionID)
	if err != nil {
		return dto.RoleSessionInfo{}, err
	}
	info, err := router.CreateRoleSessionWorkspace(projectID, mainSessionID, roleName, roleSessionID, joinSeq)
	if err != nil {
		return dto.RoleSessionInfo{}, err
	}
	return dto.RoleSessionInfo{
		MainSessionID: info.MainSessionID, RoleName: info.RoleName,
		RoleSessionID: info.RoleSessionID, Root: info.Root,
	}, nil
}

func (port SessionPort) AppendRoleDraft(mainSessionID, roleName, roleSessionID string, rows []dto.RoleDraftRow) error {
	router, projectID, err := port.roleRouter(mainSessionID)
	if err != nil {
		return err
	}
	return router.AppendRoleDraftWorkspace(projectID, mainSessionID, roleName, roleSessionID,
		storeRoleDraftRows(rows))
}

func (port SessionPort) ReadRoleDraft(mainSessionID, roleName, roleSessionID string) ([]dto.RoleDraftRow, error) {
	router, projectID, err := port.roleRouter(mainSessionID)
	if err != nil {
		return nil, err
	}
	rows, err := router.ReadRoleDraftWorkspace(projectID, mainSessionID, roleName, roleSessionID)
	if err != nil {
		return nil, err
	}
	return roleDraftRows(rows), nil
}

func (port SessionPort) SyncRoleDraft(mainSessionID, roleName, roleSessionID string, order []string) (dto.RoleDraftSyncResult, error) {
	router, projectID, err := port.roleRouter(mainSessionID)
	if err != nil {
		return dto.RoleDraftSyncResult{}, err
	}
	result, err := router.SyncRoleDraftWorkspace(projectID, mainSessionID, roleName, roleSessionID, order)
	if err != nil {
		return dto.RoleDraftSyncResult{}, err
	}
	return dto.RoleDraftSyncResult{
		CommitID: result.CommitID, SyncedRows: result.SyncedRows, LastSeq: result.LastSeq,
		LastMessageID: result.LastMessageID, AlreadySynced: result.AlreadySynced,
	}, nil
}

func (port SessionPort) AppendRoleSessionRows(mainSessionID, roleName, roleSessionID string, rows []dto.RoleRow) error {
	router, projectID, err := port.roleRouter(mainSessionID)
	if err != nil {
		return err
	}
	return router.AppendRoleSessionRowsWorkspace(projectID, mainSessionID, roleName, roleSessionID,
		storeRoleRows(rows))
}

func (port SessionPort) ReadRoleSessionRows(mainSessionID, roleName, roleSessionID string) ([]dto.RoleRow, error) {
	router, projectID, err := port.roleRouter(mainSessionID)
	if err != nil {
		return nil, err
	}
	rows, err := router.ReadRoleSessionRowsWorkspace(projectID, mainSessionID, roleName, roleSessionID)
	if err != nil {
		return nil, err
	}
	return roleRows(rows), nil
}

func (port SessionPort) RoleSnapshot(mainSessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error) {
	router, projectID, err := port.roleRouter(mainSessionID)
	if err != nil {
		return dto.RoleSnapshot{}, err
	}
	snapshot, err := router.RoleSnapshotWorkspace(projectID, mainSessionID, roleName, roleSessionID)
	if err != nil {
		return dto.RoleSnapshot{}, err
	}
	return roleSnapshot(snapshot), nil
}

func (port SessionPort) AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (dto.RoleWireSnapshot, error) {
	router, projectID, err := port.roleRouter(mainSessionID)
	if err != nil {
		return dto.RoleWireSnapshot{}, err
	}
	snapshot, err := router.AssembleRoleWireWorkspace(projectID, mainSessionID, roleName, roleSessionID, budget, k)
	if err != nil {
		return dto.RoleWireSnapshot{}, err
	}
	return roleWireSnapshot(snapshot), nil
}

func (port SessionPort) SetLifecycleOrder(sessionID, policy string, roles []string) error {
	router, projectID, err := port.roleRouter(sessionID)
	if err != nil {
		return err
	}
	return router.SetLifecycleOrderWorkspace(projectID, sessionID, policy, roles)
}

func (port SessionPort) SetRoleLifecycle(mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *dto.CompactFrameRef) error {
	router, projectID, err := port.roleRouter(mainSessionID)
	if err != nil {
		return err
	}
	return router.SetRoleLifecycleWorkspace(projectID, mainSessionID, roleName, roleSessionID, joinSeq,
		storeCompactRef(ref))
}

func (port SessionPort) ListRoleSessions(mainSessionID string) ([]string, error) {
	router, projectID, err := port.roleRouter(mainSessionID)
	if err != nil {
		return nil, err
	}
	return router.ListRoleSessionsWorkspace(projectID, mainSessionID)
}

func (port SessionPort) ScheduleRegister(sessionID string, payload dto.ScheduleEventPayload) error {
	router, projectID, err := port.roleRouter(sessionID)
	if err != nil {
		return err
	}
	return router.ScheduleRegisterWorkspace(projectID, sessionID, storeSchedulePayload(payload))
}

func (port SessionPort) ScheduleCancel(sessionID string, payload dto.ScheduleEventPayload) error {
	router, projectID, err := port.roleRouter(sessionID)
	if err != nil {
		return err
	}
	return router.ScheduleCancelWorkspace(projectID, sessionID, storeSchedulePayload(payload))
}

func (port SessionPort) ScheduleFire(sessionID string, payload dto.ScheduleEventPayload) error {
	router, projectID, err := port.roleRouter(sessionID)
	if err != nil {
		return err
	}
	return router.ScheduleFireWorkspace(projectID, sessionID, storeSchedulePayload(payload))
}

// ── DTO ↔ 存储映射（S27）：存储类型只在本文件出现 ────────────────────

func storeRoleRows(rows []dto.RoleRow) []sessionstore.Event {
	stored := make([]sessionstore.Event, len(rows))
	for index, row := range rows {
		calls := make([]sessionstore.EventToolCall, len(row.ToolCalls))
		for callIndex, call := range row.ToolCalls {
			calls[callIndex] = sessionstore.EventToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments}
		}
		stored[index] = sessionstore.Event{
			Seq: row.Seq, TaskID: row.TaskID, MessageID: row.MessageID, Kind: row.Kind, Role: row.Role,
			ReasoningContent: row.ReasoningContent, Content: row.Content, ToolCallID: row.ToolCallID,
			Name: row.Name, ToolCalls: calls, ResultRef: row.ResultRef, TokenCount: row.TokenCount,
			CreatedAt: row.CreatedAt, CommitID: row.CommitID, InOutJSON: row.InOutJSON,
			WireMaterial: row.WireMaterial, RoleName: row.RoleName, RoleSessionID: row.RoleSessionID,
			RoundID: row.RoundID, UnitSeq: row.UnitSeq,
		}
	}
	return stored
}

func roleRows(events []sessionstore.Event) []dto.RoleRow {
	rows := make([]dto.RoleRow, len(events))
	for index, event := range events {
		calls := make([]dto.RoleToolCall, len(event.ToolCalls))
		for callIndex, call := range event.ToolCalls {
			calls[callIndex] = dto.RoleToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments}
		}
		rows[index] = dto.RoleRow{
			Seq: event.Seq, TaskID: event.TaskID, MessageID: event.MessageID, Kind: event.Kind,
			Role: event.Role, ReasoningContent: event.ReasoningContent, Content: event.Content,
			ToolCallID: event.ToolCallID, Name: event.Name, ToolCalls: calls,
			ResultRef: event.ResultRef, TokenCount: event.TokenCount, CreatedAt: event.CreatedAt,
			CommitID: event.CommitID, InOutJSON: event.InOutJSON, WireMaterial: event.WireMaterial,
			RoleName: event.RoleName, RoleSessionID: event.RoleSessionID,
			RoundID: event.RoundID, UnitSeq: event.UnitSeq,
		}
	}
	return rows
}

func storeRoleDraftRows(rows []dto.RoleDraftRow) []sessionstore.RoleDraftRow {
	stored := make([]sessionstore.RoleDraftRow, len(rows))
	for index, row := range rows {
		stored[index] = sessionstore.RoleDraftRow{
			RoundID: row.RoundID, RoleName: row.RoleName, RoleSessionID: row.RoleSessionID,
			UnitSeq: row.UnitSeq, MessageID: row.MessageID,
			Event: storeRoleRows([]dto.RoleRow{row.Event})[0],
		}
	}
	return stored
}

func roleDraftRows(rows []sessionstore.RoleDraftRow) []dto.RoleDraftRow {
	drafts := make([]dto.RoleDraftRow, len(rows))
	for index, row := range rows {
		drafts[index] = dto.RoleDraftRow{
			RoundID: row.RoundID, RoleName: row.RoleName, RoleSessionID: row.RoleSessionID,
			UnitSeq: row.UnitSeq, MessageID: row.MessageID,
			Event: roleRows([]sessionstore.Event{row.Event})[0],
		}
	}
	return drafts
}

func storeCompactRef(ref *dto.CompactFrameRef) *sessionstore.CompactRef {
	if ref == nil {
		return nil
	}
	return &sessionstore.CompactRef{FrameID: ref.FrameID, AppliedSeq: ref.AppliedSeq}
}

func compactRef(ref *sessionstore.CompactRef) *dto.CompactFrameRef {
	if ref == nil {
		return nil
	}
	return &dto.CompactFrameRef{FrameID: ref.FrameID, AppliedSeq: ref.AppliedSeq}
}

func roleSnapshot(snapshot sessionstore.RoleSnapshot) dto.RoleSnapshot {
	out := dto.RoleSnapshot{
		MainSessionID: snapshot.MainSessionID, RoleName: snapshot.RoleName,
		RoleSessionID: snapshot.RoleSessionID, Root: snapshot.Root,
		JoinSeqID: snapshot.JoinSeqID, CompactRef: compactRef(snapshot.CompactRef),
		OrderPolicy: snapshot.OrderPolicy, OrderRoles: append([]string(nil), snapshot.OrderRoles...),
		MainHeadCommitID: snapshot.MainHeadCommitID, MainHeadSeq: snapshot.MainHeadSeq,
		MainRows: roleRows(snapshot.MainRows), RoleRows: roleRows(snapshot.RoleRows),
		DraftRows:          roleDraftRows(snapshot.DraftRows),
		UnassignedRoleRows: snapshot.UnassignedRoleRows,
		DesignWarnings:     append([]string(nil), snapshot.DesignWarnings...),
	}
	if snapshot.Floor != nil {
		out.Floor = &dto.RoleFloor{
			RoleName: snapshot.Floor.RoleName, RoleSessionID: snapshot.Floor.RoleSessionID,
			RoundID: snapshot.Floor.RoundID, Seq: snapshot.Floor.Seq, UpdatedAt: snapshot.Floor.UpdatedAt,
		}
	}
	return out
}

func roleWireSnapshot(snapshot sessionstore.RoleWireSnapshot) dto.RoleWireSnapshot {
	messages := make([]dto.RoleWireMessage, len(snapshot.Messages))
	for index, message := range snapshot.Messages {
		calls := make([]dto.RoleToolCall, len(message.ToolCalls))
		for callIndex, call := range message.ToolCalls {
			calls[callIndex] = dto.RoleToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments}
		}
		messages[index] = dto.RoleWireMessage{
			Role: message.Role, Content: message.Content, ReasoningContent: message.ReasoningContent,
			ToolCalls: calls, ToolCallID: message.ToolCallID, Name: message.Name,
			ResultRef: message.ResultRef, Seq: message.Seq, Internal: message.Internal,
			Repair: message.Repair, Attempt: message.Attempt,
		}
	}
	return dto.RoleWireSnapshot{
		MainSessionID: snapshot.MainSessionID, RoleName: snapshot.RoleName,
		RoleSessionID: snapshot.RoleSessionID, AppliedSeq: snapshot.AppliedSeq,
		Messages: messages, NeedCompact: snapshot.NeedCompact, PrefixDigest: snapshot.PrefixDigest,
		TailStartSeq: snapshot.TailStartSeq, FrameApplied: snapshot.FrameApplied,
		Open: snapshot.Open, PendingRows: snapshot.PendingRows,
		DesignWarnings: append([]string(nil), snapshot.DesignWarnings...),
	}
}

func storeSchedulePayload(payload dto.ScheduleEventPayload) sessionstore.ScheduleEventPayload {
	return sessionstore.ScheduleEventPayload{
		ScheduleID: payload.ScheduleID, RoleName: payload.RoleName, RoleSessionID: payload.RoleSessionID,
		Cron: payload.Cron, Interval: payload.Interval, NextFireAt: payload.NextFireAt,
		PayloadRef: payload.PayloadRef,
	}
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
			ResultRef: event.ResultRef, TokenCount: event.TokenCount,
			WireMaterial: event.WireMaterial, CreatedAt: event.CreatedAt,
			RoleName: event.RoleName, RoleSessionID: event.RoleSessionID,
			RoundID: event.RoundID, UnitSeq: event.UnitSeq,
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
			ResultRef: event.ResultRef, TokenCount: event.TokenCount,
			WireMaterial: event.WireMaterial, CreatedAt: event.CreatedAt,
			RoleName: event.RoleName, RoleSessionID: event.RoleSessionID,
			RoundID: event.RoundID, UnitSeq: event.UnitSeq,
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
	workspaceID := granular.ResolveProjectForSession(id)
	payload, err := granular.LoadRecordRaw(workspaceID, id)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, sql.ErrNoRows) {
			// S20：record 通道退役；无派生 payload 时按该会话自身的事件流
			// 构造最小 record，避免上层回退到引擎共享历史造成视图串写。
			return port.deriveSessionRecord(workspaceID, id)
		}
		return model.SessionRecord{}, err
	}
	return decodeSessionRecord(payload, id)
}

func (port SessionPort) LoadSessionRecordWorkspace(workspaceID, id string) (model.SessionRecord, error) {
	payload, err := port.granular().LoadRecordRaw(workspaceID, id)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, sql.ErrNoRows) {
			return port.deriveSessionRecord(workspaceID, id)
		}
		return model.SessionRecord{}, err
	}
	return decodeSessionRecord(payload, id)
}

// deriveSessionRecord 从该会话自身的事件流构造最小 SessionRecord（S20：
// 保证每个会话的可见会话域只来自自己的持久事实）。
func (port SessionPort) deriveSessionRecord(workspaceID, sessionID string) (model.SessionRecord, error) {
	events, err := port.LoadEventRangeWorkspace(workspaceID, sessionID, 1, math.MaxUint64)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, sql.ErrNoRows) {
			return model.SessionRecord{}, fs.ErrNotExist
		}
		return model.SessionRecord{}, err
	}
	record := model.SessionRecord{Version: 3, ID: sessionID}
	for _, event := range events {
		id := event.MessageID
		if id == "" {
			id = fmt.Sprintf("seq-%d", event.Seq)
		}
		record.Conversation.Messages = append(record.Conversation.Messages, model.Message{
			ID: id, Role: event.Role, Content: event.Content, CreatedAt: event.CreatedAt,
		})
	}
	return record, nil
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
