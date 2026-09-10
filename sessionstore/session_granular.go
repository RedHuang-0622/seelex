package sessionstore

import (
	"encoding/json"
	"io/fs"
	"math"
	"sync"
	"time"

	"github.com/RedHuang-0622/Seele/types"
)

// 会话粒度持久化（thin-wrapper-session-design.md §2）：原子单位 = session，
// 键 session:<id> 五片（record/history/transcript/toolresults/context）+
// 项目索引（project = 会话集合）。主会话与子代理会话同构（同五片，parent
// 经 record/binding 关联）。物理布局复用 Router 现有 (projectID, sessionID)
// 复合键，暴露层为会话粒度 API（module-map.md §4 迁移）。
//
// 注意：本包不得 import seelex/session（session 依赖本包，反向会成环）；
// 本文件定义存储侧类型，seelex/session 以类型别名暴露为 StorePort 契约。

// Kind 是会话种类（M2 K_i）。
type Kind string

const (
	KindMain     Kind = "main"
	KindSubagent Kind = "subagent"
)

// Status 是会话可见状态（M2 status_i；热/冷由 HasSession 驱动）。
type Status string

const (
	StatusDraft            Status = "draft"
	StatusIdle             Status = "idle"
	StatusRunning          Status = "running"
	StatusQueued           Status = "queued"
	StatusAwaitingApproval Status = "awaiting_approval"
	StatusArchived         Status = "archived"
)

// Record 是会话粒度记录（session:<id> → SessionRecord）。
type Record struct {
	ID         string    `json:"id"`
	Kind       Kind      `json:"kind"`
	ParentID   string    `json:"parent_id,omitempty"`
	Title      string    `json:"title,omitempty"`
	Status     Status    `json:"status"`
	Checkpoint string    `json:"checkpoint,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
	Binding    Binding   `json:"binding,omitempty"`
}

// Binding 是会话绑定（B_i：workspaceID / parent / kind）。
type Binding struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
	ParentID    string `json:"parent_id,omitempty"`
	Kind        Kind   `json:"kind,omitempty"`
}

// TranscriptEvent 是会话事件日志条目（session:<id>:transcript）。
type TranscriptEvent struct {
	Seq     uint64          `json:"seq"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// ToolResultRef 是工具结果归档引用（session:<id>:toolresults）。
type ToolResultRef struct {
	Ref  string `json:"ref"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// ContextStack 是会话上下文四栈（session:<id>:context；C_i）。
type ContextStack struct {
	Plan    []string `json:"plan,omitempty"`
	Task    []string `json:"task,omitempty"`
	Skill   []string `json:"skill,omitempty"`
	Compact []string `json:"compact,omitempty"`
}

// SessionInfo 是项目索引中的会话摘要（project:<p>:sessions）。
type SessionInfo struct {
	ID       string `json:"id"`
	Title    string `json:"title,omitempty"`
	Kind     Kind   `json:"kind"`
	Status   Status `json:"status"`
	ParentID string `json:"parent_id,omitempty"`
	// UpdatedAt / TokenCount 取自会话 manifest（每次提交即刷新的权威时间与
	// 计费面）。目录枚举摘要必须携带它们——侧栏/目录行据此渲染真实日期与
	// token 数；一旦在此丢弃成零值，前端会把 0001-01-01T00:00:00Z 当占位
	// 日期显示在每条会话上（9.3.2 粒度迁移回归）。
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
	TokenCount int       `json:"token_count,omitempty"`
}

// SessionGranularStore 是会话粒度存储实现（消费端口定义在
// application/core/session_runtime/ports.go，适配由 internal/adapters 承担；
// session.StorePort 死契约已删除）：包装 Router，暴露五片 + 项目索引的会话
// 粒度 API。projectID 为空 = 默认项目（未关联会话归属），不再回退 Router
// 活跃写作用域（视图切换不得改变会话的存储归属，R3 键漂移收敛）。
type SessionGranularStore struct {
	router *Router
	// resolverMu 只保护 workspaceResolver 的读写：注入发生在装配期，读取
	// 发生在 Delete/LoadHistory 等会话级操作（可与注入并发）。
	// workspaceResolver 返回会话绑定的 workspace（项目作用域）ID；用于
	// Delete/LoadHistory 等会话级操作的归属项目解析（生产 record 无
	// binding 字段，绑定在 workspace.Repo；未绑定 = 未关联默认项目）。
	resolverMu        sync.RWMutex
	workspaceResolver func(sessionID string) string
	// projectSource 返回应用已知的项目 ID 列表（生产装配 = workspace.Repo.List）：
	// 绑定丢失/旧布局时按"数据实际所在"定位，取代按活跃写作用域猜。
	projectSource func() []string
}

// NewSessionGranularStore 构造会话粒度存储；router 为 nil 时所有方法
// 退化为空操作/空结果（测试桩兼容）。
func NewSessionGranularStore(router *Router) *SessionGranularStore {
	return &SessionGranularStore{router: router}
}

// SetWorkspaceResolver 注入会话绑定项目解析器（main.go 装配点：从
// workspace.Repo.SessionWorkspace 读绑定；同 EventStore 模式）。
func (store *SessionGranularStore) SetWorkspaceResolver(resolver func(sessionID string) string) {
	if store == nil {
		return
	}
	store.resolverMu.Lock()
	store.workspaceResolver = resolver
	store.resolverMu.Unlock()
}

// resolver 返回注入的会话绑定解析器（可能为 nil）。回调在锁外调用。
func (store *SessionGranularStore) resolver() func(string) string {
	store.resolverMu.RLock()
	defer store.resolverMu.RUnlock()
	return store.workspaceResolver
}

// SetProjectSource 注入应用已知项目列表（main.go 装配点：workspace.Repo.List），
// 供归属解析在绑定缺失时按"数据实际所在"定位。
func (store *SessionGranularStore) SetProjectSource(source func() []string) {
	if store == nil {
		return
	}
	store.resolverMu.Lock()
	store.projectSource = source
	store.resolverMu.Unlock()
}

// projectList 返回注入的项目来源（可能为 nil）。回调在锁外调用。
func (store *SessionGranularStore) projectList() func() []string {
	store.resolverMu.RLock()
	defer store.resolverMu.RUnlock()
	return store.projectSource
}

func (store *SessionGranularStore) projectID(projectID string) string {
	if store == nil || store.router == nil {
		return projectID
	}
	return projectID
}

// SaveSession 原子写会话记录（幂等：同键重写不漂移；B6）。
func (store *SessionGranularStore) SaveSession(projectID string, record Record) error {
	if store == nil || store.router == nil {
		return nil
	}
	projectID = store.projectID(projectID)
	// 首次落盘：写一次空 commit 生成项目索引条目（manifest/meta），使
	// record-only 会话也能被 SessionsOf 枚举；已有条目（含仅历史、无
	// record 的旧会话）不再写 commit，避免覆盖历史 generation。
	if !store.sessionIndexed(projectID, record.ID) {
		if err := store.router.SaveCommitWorkspace(projectID, record.ID, Commit{}); err != nil {
			return err
		}
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = time.Now()
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return store.SaveRecordRaw(projectID, record.ID, payload)
}

// SaveRecordRaw 以会话粒度原子写 record 通道原始字节（state.json；
// 应用侧 model.SessionRecord 等自有 schema 经此通道落盘）。
func (store *SessionGranularStore) SaveRecordRaw(projectID, sessionID string, payload []byte) error {
	if store == nil || store.router == nil {
		return nil
	}
	projectID = store.projectID(projectID)
	if store.router.LayoutV8() {
		// S20：state/record 通道退役。record 只用于"首次索引 + 归档标记"：
		// 其余字段（Title/Binding/血缘/Checkpoints…）dev 阶段丢字段已接受。
		if err := store.EnsureIndexed(projectID, sessionID); err != nil {
			return err
		}
		archived := false
		var status struct {
			Status string `json:"status"`
		}
		if len(payload) > 0 && json.Unmarshal(payload, &status) == nil {
			archived = status.Status == string(StatusArchived)
		}
		_, err := store.router.SetSessionArchivedWorkspace(projectID, sessionID, archived)
		return err
	}
	return store.router.SaveStateWorkspace(projectID, sessionID, payload)
}

// LoadRecordRaw 读取 record 通道原始字节；不存在原样返回 fs.ErrNotExist
// （调用方按存储语义处理，如恢复路径的 record 缺失分支）。
func (store *SessionGranularStore) LoadRecordRaw(projectID, sessionID string) ([]byte, error) {
	if store == nil || store.router == nil {
		return []byte{}, nil
	}
	if store.router.LayoutV8() {
		// D9/S20：record 通道停读；按 §2.5.4 从既有通道派生 SessionRecord 形状
		// 供应用消费。
		payload, handled, err := store.router.DerivedRecordWorkspace(store.projectID(projectID), sessionID)
		if !handled {
			return store.router.LoadStateWorkspace(store.projectID(projectID), sessionID)
		}
		if err != nil {
			return nil, err
		}
		if len(payload) == 0 {
			return nil, fs.ErrNotExist
		}
		return payload, nil
	}
	return store.router.LoadStateWorkspace(store.projectID(projectID), sessionID)
}

// sessionIndexed 报告会话是否已出现在项目索引（物理实现 = Router
// workspace 列表，即 manifest/meta 存在性）。
func (store *SessionGranularStore) sessionIndexed(projectID, sessionID string) bool {
	for _, meta := range store.router.ListWorkspace(projectID) {
		if meta.SessionID == sessionID {
			return true
		}
	}
	return false
}

// EnsureIndexed 在项目索引尚无该会话时生成一次空 commit（manifest/meta），
// 使 record-only 草稿会话（工作区草稿按绑定项目落盘）能被 SessionsOf 枚举；
// 已有条目时为空操作。只在首次落盘前调用——之后 state 通道由
// SaveRecordRaw/写端口按 model.SessionRecord 覆盖，空 commit 不会清掉它。
func (store *SessionGranularStore) EnsureIndexed(projectID, sessionID string) error {
	if store == nil || store.router == nil {
		return nil
	}
	projectID = store.projectID(projectID)
	if store.sessionIndexed(projectID, sessionID) {
		return nil
	}
	return store.router.SaveCommitWorkspace(projectID, sessionID, Commit{})
}

// LoadSession 读取会话记录；不存在返回 (zero, false, nil)。
func (store *SessionGranularStore) LoadSession(projectID, sessionID string) (Record, bool, error) {
	if store == nil || store.router == nil {
		return Record{}, false, nil
	}
	if store.router.LayoutV8() {
		record, ok := store.derivedRecord(projectID, sessionID)
		return record, ok, nil
	}
	payload, err := store.router.LoadStateWorkspace(store.projectID(projectID), sessionID)
	if err != nil {
		if isSessionNotFound(err) {
			return Record{}, false, nil
		}
		return Record{}, false, err
	}
	var record Record
	if err := json.Unmarshal(payload, &record); err != nil {
		// 生产 state.json 为 application model.SessionRecord（title 为对象、
		// 含 conversation/execution/projection 等），与薄封装 Record 结构
		// 不同构。目录/绑定解析必须宽容：能提取身份即返回，绝不因类型不
		// 匹配把整个会话从侧栏清掉（左侧栏历史会话消失的根因）。
		info, ok := parseRecordInfo(payload)
		if !ok || info.ID != sessionID {
			return Record{}, false, nil
		}
		return Record{ID: sessionID, Kind: KindMain, Status: StatusIdle, Title: info.Title}, true, nil
	}
	return record, true, nil
}

// History 返回会话的框架工作历史句柄（session:<id>:history）。
func (store *SessionGranularStore) History(sessionID string) *DurableHistory {
	if store == nil || store.router == nil {
		return NewDurableHistory(nil, sessionID)
	}
	return NewDurableHistory(store.router, sessionID)
}

// HistoryForProject 返回绑定到显式项目作用域的会话历史句柄（会话粒度
// API 不再以 workspace 参数调用；项目解析由调用方/绑定提供）。
func (store *SessionGranularStore) HistoryForProject(projectID, sessionID string) *DurableHistory {
	if store == nil || store.router == nil {
		return NewDurableHistory(nil, sessionID)
	}
	history := NewDurableHistory(store.router, sessionID)
	history.SetWorkspaceResolver(func() string { return store.projectID(projectID) })
	return history
}

// ResolveProjectForSession 返回会话归属项目，定序：
//
//	resolver（workspace.Repo 绑定，权威）→ record 自带 Binding →
//	已知项目里数据实际所在 → 未关联（默认项目 ""）
//
// 刻意不回退 Router 活跃写作用域：活跃作用域是**视图**状态，切换项目就会变，
// 返回它等于把会话指向一个可能根本没有它数据的项目（读不到/删错/manifest 错键，
// R3 键漂移根因）。解析为 "" 的会话即左栏「未关联会话」分组，可显式重新绑定。
func (store *SessionGranularStore) ResolveProjectForSession(sessionID string) string {
	if store == nil || store.router == nil {
		return ""
	}
	if resolver := store.resolver(); resolver != nil {
		if projectID := resolver(sessionID); projectID != "" {
			// 绑定项目缺数据而默认项目有：按数据实际所在回退（迁移中/旧布局）。
			if !store.sessionIndexed(projectID, sessionID) && store.sessionIndexed("", sessionID) {
				return ""
			}
			return projectID
		}
	}
	if record, ok, err := store.LoadSession("", sessionID); err == nil && ok && record.Binding.WorkspaceID != "" {
		return record.Binding.WorkspaceID
	}
	if source := store.projectList(); source != nil {
		for _, projectID := range source() {
			if projectID != "" && store.sessionIndexed(projectID, sessionID) {
				return projectID
			}
		}
	}
	return ""
}

// SaveHistory 以会话粒度原子写 provider 历史（session:<id>:history；
// 幂等：同键重写不漂移）。
func (store *SessionGranularStore) SaveHistory(projectID, sessionID string, messages []types.Message) error {
	if store == nil || store.router == nil {
		return nil
	}
	return store.router.SaveWorkspace(store.projectID(projectID), sessionID, messages)
}

// HistoryRange 按窗口读取会话 provider 历史，返回 [offset, offset+limit)
// 与总数（会话粒度键）。
func (store *SessionGranularStore) HistoryRange(projectID, sessionID string, offset, limit int) ([]types.Message, int, error) {
	if store == nil || store.router == nil {
		return nil, 0, nil
	}
	return store.router.LoadRangeWorkspace(store.projectID(projectID), sessionID, offset, limit)
}

// Transcript 读取会话事件日志（session:<id>:transcript；事件通道与
// provider history 同库，事务序=seq 序）。
func (store *SessionGranularStore) Transcript(projectID, sessionID string) ([]TranscriptEvent, error) {
	if store == nil || store.router == nil {
		return []TranscriptEvent{}, nil
	}
	events, err := store.router.LoadEventTailWorkspace(store.projectID(projectID), sessionID, math.MaxInt, math.MaxInt)
	if err != nil {
		if isSessionNotFound(err) {
			return []TranscriptEvent{}, nil
		}
		return nil, err
	}
	transcript := make([]TranscriptEvent, 0, len(events))
	for _, event := range events {
		transcript = append(transcript, TranscriptEvent{
			Seq:  event.Seq,
			Type: event.Role,
		})
	}
	return transcript, nil
}

// TranscriptTail 读取会话事件日志尾部窗口（token + 单元上限；会话粒度键）。
func (store *SessionGranularStore) TranscriptTail(projectID, sessionID string, tokenBudget, maxUnits int) ([]Event, error) {
	if store == nil || store.router == nil {
		return []Event{}, nil
	}
	events, err := store.router.LoadEventTailWorkspace(store.projectID(projectID), sessionID, tokenBudget, maxUnits)
	if err != nil {
		if isSessionNotFound(err) {
			return []Event{}, nil
		}
		return nil, err
	}
	return events, nil
}

// EventRange 按 EventSeq 范围读取会话事件流（fork 切断点解析用）。
func (store *SessionGranularStore) EventRange(projectID, sessionID string, fromSeq, toSeq uint64) ([]Event, error) {
	if store == nil || store.router == nil {
		return []Event{}, nil
	}
	events, err := store.router.LoadEventRangeWorkspace(store.projectID(projectID), sessionID, fromSeq, toSeq)
	if err != nil {
		if isSessionNotFound(err) {
			return []Event{}, nil
		}
		return nil, err
	}
	return events, nil
}

// ToolResults 读取会话工具结果归档引用（session:<id>:toolresults）。
func (store *SessionGranularStore) ToolResults(projectID, sessionID string) ([]ToolResultRef, error) {
	if store == nil || store.router == nil {
		return []ToolResultRef{}, nil
	}
	results, err := store.router.ListToolResultsWorkspace(store.projectID(projectID), sessionID)
	if err != nil {
		if isSessionNotFound(err) {
			return []ToolResultRef{}, nil
		}
		return nil, err
	}
	refs := make([]ToolResultRef, 0, len(results))
	for _, result := range results {
		refs = append(refs, ToolResultRef{
			Ref:  result.Ref,
			Name: result.Tool,
			Size: int64(result.Size),
		})
	}
	return refs, nil
}

// ToolResult 读取会话工具结果原始内容（按 resultRef）。
func (store *SessionGranularStore) ToolResult(projectID, sessionID, resultRef string) (ToolResult, error) {
	if store == nil || store.router == nil {
		return ToolResult{}, nil
	}
	return store.router.LoadToolResultWorkspace(store.projectID(projectID), sessionID, resultRef)
}

// ListToolResults 读取会话 tool-results 通道全量（fork 深拷贝物理复制用）。
func (store *SessionGranularStore) ListToolResults(projectID, sessionID string) ([]ToolResult, error) {
	if store == nil || store.router == nil {
		return []ToolResult{}, nil
	}
	results, err := store.router.ListToolResultsWorkspace(store.projectID(projectID), sessionID)
	if err != nil {
		if isSessionNotFound(err) {
			return []ToolResult{}, nil
		}
		return nil, err
	}
	return results, nil
}

// Context 读取会话上下文四栈（session:<id>:context）。
func (store *SessionGranularStore) Context(projectID, sessionID string) (ContextStack, error) {
	if store == nil || store.router == nil {
		return ContextStack{}, nil
	}
	payload, err := store.router.LoadContextStateWorkspace(store.projectID(projectID), sessionID)
	if err != nil {
		if isSessionNotFound(err) {
			return ContextStack{}, nil
		}
		return ContextStack{}, err
	}
	var stack ContextStack
	if err := json.Unmarshal(payload, &stack); err != nil {
		return ContextStack{}, err
	}
	return stack, nil
}

// SaveContext 保存会话上下文四栈（session:<id>:context；幂等）。
func (store *SessionGranularStore) SaveContext(projectID, sessionID string, payload []byte) error {
	if store == nil || store.router == nil {
		return nil
	}
	return store.router.SaveContextStateWorkspace(store.projectID(projectID), sessionID, payload)
}

// LoadContextRaw 读取会话上下文模块原始字节（fork 四栈深拷贝用）；不存在
// 原样返回 fs.ErrNotExist（fork 据此走默认上下文分支）。
func (store *SessionGranularStore) LoadContextRaw(projectID, sessionID string) ([]byte, error) {
	if store == nil || store.router == nil {
		return []byte{}, nil
	}
	return store.router.LoadContextStateWorkspace(store.projectID(projectID), sessionID)
}

// SaveCommit 以会话粒度原子提交快照（record + transcript + tool-results）。
func (store *SessionGranularStore) SaveCommit(projectID, sessionID string, commit Commit) error {
	if store == nil || store.router == nil {
		return nil
	}
	return store.router.SaveCommitWorkspace(store.projectID(projectID), sessionID, commit)
}

// CurrentGeneration 返回会话当前已发布 generation（fork 血缘来源）。
func (store *SessionGranularStore) CurrentGeneration(projectID, sessionID string) (string, error) {
	if store == nil || store.router == nil {
		return "", nil
	}
	return store.router.CurrentGenerationWorkspace(store.projectID(projectID), sessionID)
}

// ConversationRange 读取会话 conversation 模块窗口（只解析 conversation
// 子树；offset/limit 语义与 Router 一致）。
func (store *SessionGranularStore) ConversationRange(projectID, sessionID string, offset, limit int) ([]ConversationMessage, int, error) {
	if store == nil || store.router == nil {
		return nil, 0, nil
	}
	return store.router.LoadConversationRangeWorkspace(store.projectID(projectID), sessionID, offset, limit)
}

// Delete 以会话粒度删除会话（record/history/transcript/toolresults/context
// 同键；项目索引同步移除）。
func (store *SessionGranularStore) Delete(projectID, sessionID string) error {
	if store == nil || store.router == nil {
		return nil
	}
	return store.router.DeleteWorkspace(store.projectID(projectID), sessionID)
}

// SessionsOf 按项目索引枚举会话（project = 会话集合；物理实现 = Router
// workspace 列表，record 补充 kind/status/parent）。
func (store *SessionGranularStore) SessionsOf(projectID string) ([]SessionInfo, error) {
	if store == nil || store.router == nil {
		return []SessionInfo{}, nil
	}
	// 目录枚举契约：projectID 原样使用（"" = 默认项目），不随 Router 活跃
	// 写作用域替换——否则视图切换（SetWorkspace）会让默认/未关联项目会话从
	// 左侧栏消失（视图变更污染列表）。活跃作用域会话由调用方显式传
	// workspaceID。
	metas := store.router.ListWorkspace(projectID)
	infos := make([]SessionInfo, 0, len(metas))
	for _, meta := range metas {
		// 目录枚举用字面项目读取 record（不做 "" → active scope 替换），
		// 保证标题补全不随视图切换读错项目。
		record, ok := store.loadSessionLiteral(projectID, meta.SessionID)
		info := SessionInfo{
			ID:         meta.SessionID,
			Title:      meta.Summary,
			Kind:       KindMain,
			Status:     StatusIdle,
			UpdatedAt:  meta.UpdatedAt,
			TokenCount: meta.TokenCount,
		}
		if ok {
			info.Kind = record.Kind
			info.Status = record.Status
			info.ParentID = record.ParentID
			if record.Title != "" {
				info.Title = record.Title
			}
			// manifest 时间缺失（异常/旧数据）时回退到 record 自带更新时间，
			// 保证目录行绝不带零值占位日期。
			if info.UpdatedAt.IsZero() && !record.UpdatedAt.IsZero() {
				info.UpdatedAt = record.UpdatedAt
			}
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// loadSessionLiteral 以字面项目 ID 读取会话记录（不替换 "" 为活跃作用域；
// 目录枚举专用）。生产 schema 宽容解析；不存在/解析失败返回 ok=false，
// 不阻断目录枚举。
func (store *SessionGranularStore) loadSessionLiteral(projectID, sessionID string) (Record, bool) {
	if store == nil || store.router == nil {
		return Record{}, false
	}
	if store.router.LayoutV8() {
		return store.derivedRecord(projectID, sessionID)
	}
	payload, err := store.router.LoadStateWorkspace(projectID, sessionID)
	if err != nil {
		return Record{}, false
	}
	var record Record
	if err := json.Unmarshal(payload, &record); err != nil {
		info, ok := parseRecordInfo(payload)
		if !ok || info.ID != sessionID {
			return Record{}, false
		}
		return Record{ID: sessionID, Kind: KindMain, Status: StatusIdle, Title: info.Title}, true
	}
	return record, true
}

// derivedRecord 按 §2.5.4 从 message head.Meta + lifecycle 派生会话记录
// （S20：record 通道退役；Title/Kind/子侧血缘不再持久化，dev 已接受）。
func (store *SessionGranularStore) derivedRecord(projectID, sessionID string) (Record, bool) {
	meta, handled, err := store.router.SessionMetaWorkspace(projectID, sessionID)
	if err != nil || !handled || meta.SessionID == "" {
		return Record{}, false
	}
	status := StatusIdle
	if archivedAt, _, err := store.router.SessionArchivedWorkspace(projectID, sessionID); err == nil && !archivedAt.IsZero() {
		status = StatusArchived
	} else if jsonRepository, ok := store.router.jsonRepositoryLocked(); ok && jsonRepository.hasDraft(Key{ProjectID: projectID, SessionID: sessionID}) {
		status = StatusDraft
	}
	return Record{
		ID: sessionID, Kind: KindMain, Status: status, Title: meta.Summary,
		UpdatedAt: meta.UpdatedAt,
		Binding:   Binding{WorkspaceID: projectID, Kind: KindMain},
	}, true
}

// parseRecordInfo 宽容解析会话记录头：兼容薄封装 Record（title 字符串）与
// 生产 model.SessionRecord（title 为 SessionTitle 对象）。解析失败返回
// ok=false（调用方降级，不阻断目录）。
func parseRecordInfo(payload []byte) (Record, bool) {
	if len(payload) == 0 {
		return Record{}, false
	}
	var header struct {
		ID     string          `json:"id"`
		Title  json.RawMessage `json:"title"`
		Kind   Kind            `json:"kind"`
		Status Status          `json:"status"`
	}
	if err := json.Unmarshal(payload, &header); err != nil {
		return Record{}, false
	}
	record := Record{ID: header.ID, Kind: header.Kind, Status: header.Status}
	if record.Kind == "" {
		record.Kind = KindMain
	}
	if record.Status == "" {
		record.Status = StatusIdle
	}
	record.Title = parseRecordTitle(header.Title)
	return record, record.ID != ""
}

// parseRecordTitle 兼容 title 的两种形态：字符串（薄封装 Record）与
// SessionTitle 对象（生产 model.SessionRecord，取 value 字段）。
func parseRecordTitle(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var object struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &object) == nil {
		return object.Value
	}
	return ""
}

// Bind 写会话绑定（session:<id>:binding；幂等合并进 record）。
func (store *SessionGranularStore) Bind(projectID, sessionID string, binding Binding) error {
	if store == nil || store.router == nil {
		return nil
	}
	projectID = store.projectID(projectID)
	record, ok, err := store.LoadSession(projectID, sessionID)
	if err != nil {
		return err
	}
	if !ok {
		record = Record{ID: sessionID, Kind: binding.Kind, Status: StatusDraft}
	}
	if record.Binding.WorkspaceID == "" {
		record.Binding.WorkspaceID = projectID
	}
	record.Binding.ParentID = binding.ParentID
	record.Binding.Kind = binding.Kind
	record.Kind = binding.Kind
	record.ParentID = binding.ParentID
	return store.SaveSession(projectID, record)
}
