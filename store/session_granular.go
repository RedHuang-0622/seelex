package store

import (
	"encoding/json"
	"io/fs"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// 会话粒度持久化（thin-wrapper-session-design.md §2）：原子单位 = session，
// 键 session:<id> 五片（record/history/transcript/toolresults/context）+
// 项目索引（project = 会话集合）。主会话与子代理会话同构（同五片，parent
// 经 record/binding 关联）。物理布局复用 Router 现有 (projectID, sessionID)
// 复合键，暴露层为会话粒度 API（module-map.md §4 迁移）。
//
// 注意：本包不得 import seelex/session（session 依赖本包，反向会成环）；
// 本文件定义存储侧类型，seelex/session 以类型别名暴露为 StorePort 契约。

// 会话粒度形状的唯一事实源在 contract/dto（store 只做持久化与词表别名）。
// 同一份形状被 store / session 域 / application / 前端读面消费；放在契约层
// 让 session 域迁入中间层时不再被迫 import store。
type (
	Kind          = dto.SessionRecordKind
	Status        = dto.SessionRecordStatus
	Record        = dto.SessionIndexRecord
	Binding       = dto.SessionBinding
	ToolResultRef = dto.SessionToolResultRef
	ContextStack  = dto.SessionContextStack
	SessionInfo   = dto.SessionInfo
)

const (
	KindMain     = dto.SessionRecordKindMain
	KindSubagent = dto.SessionRecordKindSubagent

	StatusDraft            = dto.SessionRecordStatusDraft
	StatusActive           = dto.SessionRecordStatusActive
	StatusIdle             = dto.SessionRecordStatusIdle
	StatusRunning          = dto.SessionRecordStatusRunning
	StatusQueued           = dto.SessionRecordStatusQueued
	StatusAwaitingApproval = dto.SessionRecordStatusAwaitingApproval
	StatusArchived         = dto.SessionRecordStatusArchived
)

// SessionGranularStore 是会话粒度存储实现（消费端口定义在
// application/core/session_runtime/ports.go，适配由 wiring/adapters 承担；
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

// SetWorkspaceResolver 注入会话绑定项目解析器（boot 装配点：从
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

// SetProjectSource 注入应用已知项目列表（boot 装配点：workspace.Repo.List），
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
		// S20：state/record 通道退役。record 只用于"首次索引 + 归档标记"，
		// 外加标题写穿（head.Meta.Summary）。
		if err := store.EnsureIndexed(projectID, sessionID); err != nil {
			return err
		}
		archived := false
		title := ""
		var head struct {
			Status string          `json:"status"`
			Title  json.RawMessage `json:"title"`
		}
		if len(payload) > 0 && json.Unmarshal(payload, &head) == nil {
			archived = head.Status == string(StatusArchived)
			title = parseRecordTitle(head.Title)
		}
		// 标题写穿：record 通道退役后，标题是 head 独有的展示事实。不写穿的
		// 后果是重启后目录拿不到标题，只能读会话正文猜（目录刷新读正文的根因）。
		// 空标题不写——归档/后台落盘经常没有标题，落下空值会抹掉已有标题。
		if title != "" {
			if _, err := store.router.SetSessionTitleWorkspace(projectID, sessionID, title); err != nil {
				return err
			}
		}
		_, err := store.router.SetSessionArchivedWorkspace(projectID, sessionID, archived)
		return err
	}
	return store.router.SaveStateWorkspace(projectID, sessionID, payload)
}

// SaveSessionTitle 写会话标题（v8：message head 的目录枚举面
// head.Meta.Summary）。标题落在这里，目录枚举（读 message head）即带标题，
// 不必为标题打开消息分片。空 sessionID/标题不落盘：草稿清理路径传空值，
// 落下会造出没有数据的会话目录（幽灵会话）。
func (store *SessionGranularStore) SaveSessionTitle(projectID, sessionID, title string) error {
	if store == nil || store.router == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	title = strings.TrimSpace(title)
	if sessionID == "" || title == "" {
		return nil
	}
	_, err := store.router.SetSessionTitleWorkspace(store.projectID(projectID), sessionID, title)
	return err
}

// SessionTitle 只读会话标题（header-only：只打开 metadata/message.json，
// 不读消息分片）。ok=false 表示未命中（非 v8 布局/会话头不存在），调用方按
// "无标题"处理，不得因此退化为读正文。
func (store *SessionGranularStore) SessionTitle(projectID, sessionID string) (string, bool, error) {
	if store == nil || store.router == nil {
		return "", false, nil
	}
	return store.router.SessionTitleWorkspace(store.projectID(projectID), sessionID)
}

// FirstUserInputs 读取会话最早的若干条用户输入正文（标题回填用；有界读：
// 只打开首个消息分片）——老会话（标题写穿上线前落盘）会话头里没有标题，
// 目录面据此一次性重建"用户第一问"标题，不必打开尾部窗口猜标题。
// hasLayout=false = 会话尚无已发布行，调用方稍后重试。
func (store *SessionGranularStore) FirstUserInputs(projectID, sessionID string, limit int) ([]string, bool, error) {
	if store == nil || store.router == nil {
		return nil, false, nil
	}
	return store.router.FirstUserInputsWorkspace(store.projectID(projectID), sessionID, limit)
}

// LoadRecordRaw 读取 record 通道原始字节；不存在原样返回 fs.ErrNotExist
// （调用方按存储语义处理，如恢复路径的 record 缺失分支）。
func (store *SessionGranularStore) LoadRecordRaw(projectID, sessionID string) ([]byte, error) {
	if store == nil || store.router == nil {
		return []byte{}, nil
	}
	if store.router.LayoutV8() {
		// v8 的 record 通道已退役；conversation 由 Kernel Session 从原始
		// message 行投影，Store 不再返回派生 record payload。
		return nil, fs.ErrNotExist
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
		if IsSessionNotFound(err) {
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
		return Record{ID: sessionID, Kind: KindMain, Status: StatusActive, Title: info.Title}, true, nil
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

// TranscriptTail 读取会话事件日志尾部窗口（token + 单元上限；会话粒度键）。
func (store *SessionGranularStore) TranscriptTail(projectID, sessionID string, tokenBudget, maxUnits int) ([]Event, error) {
	if store == nil || store.router == nil {
		return []Event{}, nil
	}
	events, err := store.router.LoadEventTailWorkspace(store.projectID(projectID), sessionID, tokenBudget, maxUnits)
	if err != nil {
		if IsSessionNotFound(err) {
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
		if IsSessionNotFound(err) {
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
		if IsSessionNotFound(err) {
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
		if IsSessionNotFound(err) {
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
		if IsSessionNotFound(err) {
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

// LoadCompactionRecords 读取该会话的压缩记录（应用侧记录原文，按写入顺序）。
// 空结果 = 该会话从未压缩过（或后端未 v8 化，调用方回退 record 通道）。
func (store *SessionGranularStore) LoadCompactionRecords(projectID, sessionID string) ([]json.RawMessage, error) {
	if store == nil || store.router == nil {
		// 显式零值：没有 store/router = nil 记录（调用方一律看 len）；门禁禁的是裸 `return nil, nil`。
		return []json.RawMessage(nil), nil
	}
	records, _, err := store.router.LoadCompactionRecordsWorkspace(store.projectID(projectID), sessionID)
	if err != nil {
		return nil, err
	}
	return records, nil
}

// CurrentGeneration 返回会话当前已发布 generation（fork 血缘来源）。
func (store *SessionGranularStore) CurrentGeneration(projectID, sessionID string) (string, error) {
	if store == nil || store.router == nil {
		return "", nil
	}
	return store.router.CurrentGenerationWorkspace(store.projectID(projectID), sessionID)
}

// QueueEnqueue 把一条排队输入镜像落盘（同 requestID 重放幂等）。
func (store *SessionGranularStore) QueueEnqueue(projectID, sessionID, requestID, content string) error {
	if store == nil || store.router == nil {
		return nil
	}
	return store.router.QueueEnqueueWorkspace(store.projectID(projectID), sessionID, requestID, content)
}

// QueueMarkConsumed 把 lifecycle 队列中全部待发送项标记为被 turnID 消费。
func (store *SessionGranularStore) QueueMarkConsumed(projectID, sessionID, turnID string) error {
	if store == nil || store.router == nil {
		return nil
	}
	return store.router.QueueMarkConsumedWorkspace(store.projectID(projectID), sessionID, turnID)
}

// QueueConfirmConsumed 该轮已发布 → 消费项出队。
func (store *SessionGranularStore) QueueConfirmConsumed(projectID, sessionID, turnID string) error {
	if store == nil || store.router == nil {
		return nil
	}
	return store.router.QueueConfirmConsumedWorkspace(store.projectID(projectID), sessionID, turnID)
}

// QueueFailConsumed 该轮未发布 → 消费项内容回草稿并出队。
func (store *SessionGranularStore) QueueFailConsumed(projectID, sessionID, turnID string) error {
	if store == nil || store.router == nil {
		return nil
	}
	return store.router.QueueFailConsumedWorkspace(store.projectID(projectID), sessionID, turnID)
}

// QueueItems 读 lifecycle 队列投影。
func (store *SessionGranularStore) QueueItems(projectID, sessionID string) ([]QueueItem, bool, error) {
	if store == nil || store.router == nil {
		return nil, false, nil
	}
	return store.router.QueueItemsWorkspace(store.projectID(projectID), sessionID)
}

// QueueRecoverItems 重启恢复队列（条目级结果）。
func (store *SessionGranularStore) QueueRecoverItems(projectID, sessionID string) (QueueRecoveryReport, bool, error) {
	if store == nil || store.router == nil {
		return QueueRecoveryReport{}, false, nil
	}
	return store.router.QueueRecoverItemsWorkspace(store.projectID(projectID), sessionID)
}

// PendingMessageTail 探测 message 通道草稿尾部（只读；v8 未装配返回
// ok=false）。
func (store *SessionGranularStore) PendingMessageTail(projectID, sessionID string) (PendingTailReport, bool, error) {
	if store == nil || store.router == nil {
		return PendingTailReport{}, false, nil
	}
	return store.router.PendingMessageTailWorkspace(store.projectID(projectID), sessionID)
}

// RecoverPendingMessageTail 显式恢复草稿尾部（基座一致才发布）。
func (store *SessionGranularStore) RecoverPendingMessageTail(projectID, sessionID string) (PendingTailReport, bool, error) {
	if store == nil || store.router == nil {
		return PendingTailReport{}, false, nil
	}
	return store.router.RecoverPendingMessageTailWorkspace(store.projectID(projectID), sessionID)
}

// DiscardPendingMessageTail 显式丢弃草稿尾部。
func (store *SessionGranularStore) DiscardPendingMessageTail(projectID, sessionID string) (PendingTailReport, bool, error) {
	if store == nil || store.router == nil {
		return PendingTailReport{}, false, nil
	}
	return store.router.DiscardPendingMessageTailWorkspace(store.projectID(projectID), sessionID)
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
			Status:     StatusActive,
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
		return Record{ID: sessionID, Kind: KindMain, Status: StatusActive, Title: info.Title}, true
	}
	return record, true
}

// derivedRecord 按 §2.5.4 从 message head.Meta + lifecycle 派生会话记录
// （S20：record 通道退役；Kind/子侧血缘不再持久化，dev 已接受）。标题随
// 会话头持久化（head.Meta.Summary）并在此派生回 record.Title：目录枚举
// 只需要 head，不需要为标题打开消息分片。
func (store *SessionGranularStore) derivedRecord(projectID, sessionID string) (Record, bool) {
	meta, handled, err := store.router.SessionMetaWorkspace(projectID, sessionID)
	if err != nil || !handled || meta.SessionID == "" {
		return Record{}, false
	}
	status := StatusActive
	if archivedAt, _, err := store.router.SessionArchivedWorkspace(projectID, sessionID); err == nil && !archivedAt.IsZero() {
		status = StatusArchived
	} else if jsonRepository, ok := store.router.jsonRepository(); ok && jsonRepository.hasDraft(Key{ProjectID: projectID, SessionID: sessionID}) {
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
		record.Status = StatusActive
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
