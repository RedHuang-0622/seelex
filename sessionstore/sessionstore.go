// Package sessionstore provides atomic, project-scoped persistence for chat
// sessions. The JSON v8 layout is the only active implementation; retired
// backend enums return ErrBackendRetired.
package sessionstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	frameworkStorage "github.com/RedHuang-0622/Seele/seelectx/storage"
	"github.com/RedHuang-0622/Seele/types"
)

type Backend string

const (
	BackendJSON       Backend = "json"
	BackendSQLite     Backend = "sqlite"
	BackendPostgreSQL Backend = "postgres"
	BackendRedis      Backend = "redis"
	// CompressedTurnRefPrefix 是压缩轮次原文在 tool-results 通道中的 ref
	// 前缀（ref = "compressed:"+segment_id）。前缀属于存储通道命名空间，
	// 压缩/检索与 fork 血缘共用同一常量，避免字符串漂移。
	CompressedTurnRefPrefix = "compressed:"
	// defaultMessageShardSize 是 JSON 存储分片条数的默认值
	// （seele.yaml limits 段 message_shard_size 可调，0 = 默认）。
	defaultMessageShardSize = 100
	// frameworkEventLogFile 是执行事实事件库的独立 append-only 文件
	// （v2 模块布局：不随 generation rollover 失效；见 plan.md §阶段1 P0）。
	frameworkEventLogFile = "framework-events.json"
	// frameworkEventLegacyFile 是 v1 布局下 generation 内的事件库文件名，
	// 首次写入 v2 模块时迁移合并，之后只读回退。
	frameworkEventLegacyFile = "events.json"
)

// ErrBackendRetired 标识已退役的会话存储后端。R1 只保留 JSON v8 实现；
// SQLite/PostgreSQL/Redis 枚举继续保留，便于配置层给出明确、可操作的提示，
// 但 Open 不再静默回退或打开旧链路。
var ErrBackendRetired = errors.New("session storage: backend retired/unsupported")

func retiredBackendError(backend Backend) error {
	return fmt.Errorf("%w: %s; use %s", ErrBackendRetired, backend, BackendJSON)
}

type Config struct {
	Backend Backend `json:"backend"`
	Path    string  `json:"path,omitempty"`
	DSN     string  `json:"dsn,omitempty"`
	// SessionStorage 是 §11 存储运行参数覆盖（零值 = 用默认/limits 层）。
	SessionStorage storageSettings `json:"session_storage,omitempty"`
}

func (config Config) Normalize(defaultPath string) (Config, error) {
	config.Backend = Backend(strings.ToLower(strings.TrimSpace(string(config.Backend))))
	if config.Backend == "" {
		config.Backend = BackendJSON
	}
	switch config.Backend {
	case BackendJSON:
		if strings.TrimSpace(config.Path) == "" {
			config.Path = filepath.Join(defaultPath, "sessions-json")
		}
	case BackendSQLite, BackendPostgreSQL, BackendRedis:
		return Config{}, retiredBackendError(config.Backend)
	default:
		return Config{}, fmt.Errorf("session storage: unsupported backend %q", config.Backend)
	}
	// 覆盖链的最后一层：默认值 ← limits（由 NewRouter 注入）← 本配置。
	config.SessionStorage = resolveStorageSettings(config.SessionStorage)
	if err := validateStorageSettings(config.SessionStorage); err != nil {
		return Config{}, err
	}
	return config, nil
}

// Safe returns a configuration suitable for the GUI. Credentials are never
// returned to the renderer; leaving DSN empty means "keep current DSN".
func (config Config) Safe() Config {
	copy := config
	if copy.DSN != "" {
		copy.DSN = "configured"
	}
	return copy
}

type Key struct {
	ProjectID string
	SessionID string
}

type EventToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// EventKind 是 transcript 事件的显式类别，供轨迹多线谱与有序日志直接区分
// 工具调用/LLM/用户输入等，避免仅按 role 启发式判定。
const (
	EventKindUserInput  = "user_input"
	EventKindInternal   = "internal"
	EventKindLLM        = "llm"
	EventKindToolCall   = "tool_call"
	EventKindToolOutput = "tool_output"
	EventKindSystem     = "system"
	EventKindError      = "error"
	EventKindNotice     = "notice"
)

// EventKindOf 返回事件的显式类别；旧数据 Kind 为空时按 Role/ToolCalls 回退。
func EventKindOf(event Event) string {
	if event.Kind != "" {
		return event.Kind
	}
	switch event.Role {
	case "user":
		return EventKindUserInput
	case "assistant":
		if len(event.ToolCalls) > 0 {
			return EventKindToolCall
		}
		return EventKindLLM
	case "tool":
		return EventKindToolOutput
	case "system":
		return EventKindSystem
	case "error":
		return EventKindError
	default:
		return EventKindNotice
	}
}

// Event is the append-only transcript representation shared by every
// backend. TokenCount is recorded at event creation time so reverse reads do
// not need to retokenize the complete archive.
type Event struct {
	Seq    uint64 `json:"seq"`
	TaskID string `json:"task_id,omitempty"`
	// MessageID 是同一逻辑单元的 UI 会话消息定位键（event-to-message 索引；
	// 模块化方案 §3.2：禁止按数组位置临时推导）。无法稳定配对时为空。
	MessageID string `json:"message_id,omitempty"`
	// Kind 是轨迹可见的显式类别（tool_call/llm/user_input/…）；空 = 旧数据，
	// 用 EventKindOf 回退。
	Kind             string `json:"kind,omitempty"`
	Role             string `json:"role"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
	Content          string `json:"content,omitempty"`
	// ProviderContent 是该事件在 provider wire 上**实际发出**的正文（仅当它
	// 与 Content 不同才落盘；空 = Content 即 wire 字节）。Content 是视图/轨迹
	// 呈现（工具失败分类文本、超限警告的应用归档引用），ProviderContent 是
	// 「已发出字节」。provider 投影出口（eventsToMessages / 会话 wire 装配 /
	// 应用侧 transcript 投影）必须取它，否则跨轮重投影改写该消息、前缀缓存
	// 自该点起失效（实测 63 B → 181 B，见
	// docs/research/2026-09-11-seelex-vs-codex-context-strategy-control-group.md §8）。
	ProviderContent string          `json:"provider_content,omitempty"`
	ToolCallID      string          `json:"tool_call_id,omitempty"`
	Name            string          `json:"name,omitempty"`
	ToolCalls       []EventToolCall `json:"tool_calls,omitempty"`
	ResultRef       string          `json:"result_ref,omitempty"`
	TokenCount      int             `json:"token_count"`
	CreatedAt       time.Time       `json:"created_at"`
	// CommitID 是 会话存储布局中"一次持久提交"的标识：同一提交内的多行事件共享
	// 同一 commit_id；重复持久化同 commit_id 幂等（行不重复、head 不双跳）。
	// 旧布局 transcript.log / rollout.jsonl 已随 D2/S12 退役，不再消费该
	// 字段。
	CommitID string `json:"commit_id,omitempty"`
	// InOutJSON 是 message 行的"最终成功载荷"（最终成功 in/out 原样）。
	InOutJSON json.RawMessage `json:"in_out_json,omitempty"`
	// WireMaterial 只对 internal_user/context 行有意义：true = 可作为
	// 内部 user 材料进入 wire（R2-FILTER），false/空 = 只服务前端/历史。
	WireMaterial bool `json:"wire_material,omitempty"`
	// RoleName / RoleSessionID 是群聊角色归属（§8.3）：user/main/tl/
	// agent-team；message 行必须可识别归属，UI/审计/恢复按此区分。
	RoleName      string `json:"role_name,omitempty"`
	RoleSessionID string `json:"role_session_id,omitempty"`
	// RoundID 是一条 user 输入开启的群聊因果轮次；UnitSeq 是角色内单元序。
	// 两者与 message 全局 seq 分离，是 sequencer 排序键（§8.3）。
	RoundID uint64 `json:"round_id,omitempty"`
	UnitSeq uint64 `json:"unit_seq,omitempty"`
}

type ToolResult struct {
	Ref        string    `json:"ref"`
	Tool       string    `json:"tool"`
	Content    string    `json:"content"`
	Digest     string    `json:"digest"`
	Size       int       `json:"size"`
	TokenCount int       `json:"token_count"`
	CreatedAt  time.Time `json:"created_at"`
}

// Commit replaces the bounded provider cache and append-only transcript
// snapshot together with the latest projection. Tool results are immutable
// additions referenced by the committed state.
type Commit struct {
	ProviderHistory []types.Message
	Events          []Event
	State           []byte
	ToolResults     []ToolResult
}

func (key Key) validate() error {
	if strings.TrimSpace(key.SessionID) == "" {
		return errors.New("session storage: session ID is required")
	}
	return nil
}

// validateProjectID 校验项目级记录（WriteProjectRecord/ReadProjectRecord）的
// 项目作用域；项目记录不挂会话，只按 projectID 独立存储。
func validateProjectID(projectID string) error {
	if strings.TrimSpace(projectID) == "" {
		return errors.New("session storage: project ID is required")
	}
	return nil
}

// Repository is the single persistence contract used by session.Manager.
// WriteAtomic replaces one complete logical history. Reads therefore observe
// either the preceding committed history or the next committed history, never
// a mix of shards from both.
type Repository interface {
	WriteCommit(context.Context, Key, Commit) error
	WriteAtomic(context.Context, Key, []types.Message) error
	Read(context.Context, Key) ([]types.Message, error)
	ReadRange(context.Context, Key, int, int) ([]types.Message, int, error)
	ReadEventTail(context.Context, Key, int, int) ([]Event, error)
	// ReadEventRange 按 EventSeq 范围（含端点）读取事件流，保持 Seq 连续性。
	ReadEventRange(context.Context, Key, uint64, uint64) ([]Event, error)
	// ReadConversationRange 只解析 state blob 的 conversation 模块（不解析
	// Plan/Execution/Projection 等非 conversation 子树），offset/limit 语义
	// 与 ReadRange 一致（limit <= 0 返回窗口尾段；总数为完整消息数）。
	ReadConversationRange(context.Context, Key, int, int) ([]ConversationMessage, int, error)
	ReadToolResult(context.Context, Key, string) (ToolResult, error)
	// ListToolResults 返回会话 tool-results 通道的全部不可变结果（含
	// compressed:<segment_id> 压缩原文归档）。fork 深拷贝需要物理复制整个
	// 通道：先枚举，再随子会话快照逐条写入。
	ListToolResults(context.Context, Key) ([]ToolResult, error)
	// CurrentGeneration 返回会话当前已发布 generation（不可变快照版本；
	// fork 血缘的 forked_from_generation 来源）。会话不存在时返回
	// fs.ErrNotExist / sql.ErrNoRows。
	CurrentGeneration(context.Context, Key) (string, error)
	WriteState(context.Context, Key, []byte) error
	ReadState(context.Context, Key) ([]byte, error)
	// WriteContextState/ReadContextState 是会话 context 模块的独立存储通道
	// （system prompt + Plan/Task/Skill/Compact 四栈；模块化方案 architecture.md
	// §4）。与 state.json（SessionRecord）物理隔离，避免不同 schema 互相覆盖。
	WriteContextState(context.Context, Key, []byte) error
	ReadContextState(context.Context, Key) ([]byte, error)
	// WriteProjectRecord/ReadProjectRecord 是项目级模块语义记录（plan.md §3.7.1）。
	// 与会话记录不同，它按 projectID 独立存储、跨会话共享；会话只读（read-only
	// contract），只有 project_refresh 工具调用写入。
	WriteProjectRecord(context.Context, string, ProjectRecord) error
	ReadProjectRecord(context.Context, string) (ProjectRecord, error)
	// AppendFrameworkEvent/ReadFrameworkEvents 是会话级执行事实事件库
	// （双轨事件的事实轨，slice 8；event.Sink → sessionstore 事件库）。
	// 追加顺序 = 落库顺序；读取按 Seq 排序。
	AppendFrameworkEvent(context.Context, Key, EventLogEntry) error
	ReadFrameworkEvents(context.Context, Key) ([]EventLogEntry, error)
	// stackJournal 返回 plan/task/goal 栈通道（my_design §2.4）的后端实现。
	// 接口方法不可在包外实现：每个后端必须显式给出栈通道，不存在「某个后端
	// 没有栈通道」的运行期分支。
	stackJournal() stackJournal
	List(context.Context, string) ([]frameworkStorage.SessionMeta, error)
	Delete(context.Context, Key) error
	Ping(context.Context) error
	Close() error
}

// Router serializes access to the selected backend and supplies the active
// project scope. Changing storage is atomic with respect to all repository
// calls: existing operations finish on the old backend, then future calls use
// the fully initialized replacement.
type Router struct {
	mu          sync.RWMutex
	repository  Repository
	config      Config
	configPath  string
	defaultPath string
	projectID   string
	// opsMu/activeOps/opsCond 统计在途 repository 操作：Repository 切换与
	// Close 只在活跃操作归零后关闭旧后端；数据操作本身不再持有 router.mu，
	// 不同会话的读写可并行（跨会话锁竞争消除）。
	opsMu     sync.Mutex
	activeOps int
	opsCond   *sync.Cond
	opsOnce   sync.Once
}

func NewRouter(configPath, defaultPath string, limits ...storageSettings) (*Router, error) {
	config, err := loadConfig(configPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if errors.Is(err, fs.ErrNotExist) {
		config = Config{Backend: BackendJSON}
	}
	// §11 覆盖链：默认值 ← seele.yaml limits.session_storage ← session-storage.json。
	config.SessionStorage = resolveStorageSettings(limits...)
	config, err = config.Normalize(defaultPath)
	if err != nil {
		return nil, err
	}
	repository, err := Open(context.Background(), config)
	if err != nil {
		return nil, err
	}
	router := &Router{repository: repository, config: config, configPath: configPath, defaultPath: defaultPath}
	router.opsCond = sync.NewCond(&router.opsMu)
	return router, nil
}

func (router *Router) SetWorkspace(projectID string) {
	router.mu.Lock()
	router.projectID = strings.TrimSpace(projectID)
	router.mu.Unlock()
}

func (router *Router) Workspace() string {
	router.mu.RLock()
	defer router.mu.RUnlock()
	return router.projectID
}

func (router *Router) Save(sessionID string, messages []types.Message) error {
	return router.withRepository(func(repository Repository, projectID string) error {
		return repository.WriteAtomic(context.Background(), Key{ProjectID: projectID, SessionID: sessionID}, messages)
	})
}

// SaveWorkspace 在显式项目作用域下原子写 provider 历史（framework
// DurableHistory 按会话 workspace 落盘用；不改变 active write scope）。
func (router *Router) SaveWorkspace(projectID, sessionID string, messages []types.Message) error {
	return router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		return repository.WriteAtomic(context.Background(), Key{ProjectID: projectID, SessionID: sessionID}, messages)
	})
}

func (router *Router) SaveCommit(sessionID string, commit Commit) error {
	return router.withRepository(func(repository Repository, projectID string) error {
		return repository.WriteCommit(context.Background(), Key{ProjectID: projectID, SessionID: sessionID}, commit)
	})
}

func (router *Router) SaveCommitWorkspace(projectID, sessionID string, commit Commit) error {
	return router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		return repository.WriteCommit(context.Background(), Key{ProjectID: projectID, SessionID: sessionID}, commit)
	})
}

func (router *Router) Load(sessionID string) ([]types.Message, error) {
	return router.LoadWorkspace(router.Workspace(), sessionID)
}

// LoadWorkspace reads a session from an explicit project scope without
// changing the router's active write scope.
func (router *Router) LoadWorkspace(projectID, sessionID string) ([]types.Message, error) {
	var messages []types.Message
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		var err error
		messages, err = repository.Read(context.Background(), Key{ProjectID: projectID, SessionID: sessionID})
		return err
	})
	return messages, err
}

func (router *Router) LoadRange(sessionID string, offset, limit int) ([]types.Message, int, error) {
	return router.LoadRangeWorkspace(router.Workspace(), sessionID, offset, limit)
}

// LoadRangeWorkspace reads a history window from an explicit project scope
// without changing the router's active write scope.
func (router *Router) LoadRangeWorkspace(projectID, sessionID string, offset, limit int) ([]types.Message, int, error) {
	var messages []types.Message
	var total int
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		var err error
		messages, total, err = repository.ReadRange(context.Background(), Key{ProjectID: projectID, SessionID: sessionID}, offset, limit)
		return err
	})
	return messages, total, err
}

// LoadEventTail reads newest complete protocol units within a token budget.
func (router *Router) LoadEventTail(sessionID string, tokenBudget, maxUnits int) ([]Event, error) {
	return router.LoadEventTailWorkspace(router.Workspace(), sessionID, tokenBudget, maxUnits)
}

func (router *Router) LoadEventTailWorkspace(projectID, sessionID string, tokenBudget, maxUnits int) ([]Event, error) {
	var events []Event
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		var err error
		events, err = repository.ReadEventTail(context.Background(), Key{ProjectID: projectID, SessionID: sessionID}, tokenBudget, maxUnits)
		return err
	})
	return events, err
}

// LoadConversationRangeWorkspace 读取会话 conversation 模块的指定窗口
// （只解析 state blob 的 conversation 子树，不解析执行/计划模块）。
func (router *Router) LoadConversationRangeWorkspace(projectID, sessionID string, offset, limit int) ([]ConversationMessage, int, error) {
	var messages []ConversationMessage
	var total int
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		var err error
		messages, total, err = repository.ReadConversationRange(context.Background(), Key{ProjectID: projectID, SessionID: sessionID}, offset, limit)
		return err
	})
	return messages, total, err
}

// LoadEventRangeWorkspace 按 EventSeq 范围（含端点）读取事件流
// （诊断/流式重放的有界范围读取）。
func (router *Router) LoadEventRangeWorkspace(projectID, sessionID string, fromSeq, toSeq uint64) ([]Event, error) {
	var events []Event
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		var err error
		events, err = repository.ReadEventRange(context.Background(), Key{ProjectID: projectID, SessionID: sessionID}, fromSeq, toSeq)
		return err
	})
	return events, err
}

func (router *Router) LoadToolResult(sessionID, resultRef string) (ToolResult, error) {
	return router.LoadToolResultWorkspace(router.Workspace(), sessionID, resultRef)
}

func (router *Router) LoadToolResultWorkspace(projectID, sessionID, resultRef string) (ToolResult, error) {
	var result ToolResult
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		var err error
		result, err = repository.ReadToolResult(context.Background(), Key{ProjectID: projectID, SessionID: sessionID}, resultRef)
		return err
	})
	return result, err
}

// ListToolResultsWorkspace 读取会话 tool-results 通道全部结果（显式项目
// 作用域；fork 深拷贝物理复制的枚举来源）。
func (router *Router) ListToolResultsWorkspace(projectID, sessionID string) ([]ToolResult, error) {
	var results []ToolResult
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		var err error
		results, err = repository.ListToolResults(context.Background(), Key{ProjectID: projectID, SessionID: sessionID})
		return err
	})
	return results, err
}

// CurrentGenerationWorkspace 读取会话当前已发布 generation（显式项目
// 作用域；fork 血缘快照版本绑定）。
func (router *Router) CurrentGenerationWorkspace(projectID, sessionID string) (string, error) {
	var generation string
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		var err error
		generation, err = repository.CurrentGeneration(context.Background(), Key{ProjectID: projectID, SessionID: sessionID})
		return err
	})
	return generation, err
}

// SaveState stores application-owned session state next to the engine history.
// The blob is opaque to sessionstore so JSON, SQLite, and PostgreSQL share the
// same persistence contract without importing application packages.
func (router *Router) SaveState(sessionID string, state []byte) error {
	return router.SaveStateWorkspace(router.Workspace(), sessionID, state)
}

func (router *Router) SaveStateWorkspace(projectID, sessionID string, state []byte) error {
	return router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		return repository.WriteState(context.Background(), Key{ProjectID: projectID, SessionID: sessionID}, state)
	})
}

// SaveContextState 保存会话 context 模块（system prompt + 四栈）到独立
// 存储通道，与 SessionRecord 的 state blob 物理隔离。
func (router *Router) SaveContextState(sessionID string, state []byte) error {
	return router.SaveContextStateWorkspace(router.Workspace(), sessionID, state)
}

func (router *Router) SaveContextStateWorkspace(projectID, sessionID string, state []byte) error {
	return router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		return repository.WriteContextState(context.Background(), Key{ProjectID: projectID, SessionID: sessionID}, state)
	})
}

// LoadContextState 读取会话 context 模块；不存在时返回 fs.ErrNotExist。
func (router *Router) LoadContextState(sessionID string) ([]byte, error) {
	return router.LoadContextStateWorkspace(router.Workspace(), sessionID)
}

func (router *Router) LoadContextStateWorkspace(projectID, sessionID string) ([]byte, error) {
	var state []byte
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		var err error
		state, err = repository.ReadContextState(context.Background(), Key{ProjectID: projectID, SessionID: sessionID})
		return err
	})
	return state, err
}

func (router *Router) LoadState(sessionID string) ([]byte, error) {
	return router.LoadStateWorkspace(router.Workspace(), sessionID)
}

func (router *Router) LoadStateWorkspace(projectID, sessionID string) ([]byte, error) {
	var state []byte
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		var err error
		state, err = repository.ReadState(context.Background(), Key{ProjectID: projectID, SessionID: sessionID})
		return err
	})
	return state, err
}

// SaveCheckpointWorkspace 写入引擎续跑快照（S24：JSON v8 布局落
// metadata/checkpoint.json；SQL/Redis 在 v8 化搁置前沿用 state 通道）。
func (router *Router) SaveCheckpointWorkspace(projectID, sessionID string, payload []byte) error {
	return router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		if jsonRepository, ok := repository.(*jsonRepository); ok {
			return jsonRepository.writeCheckpointLayout(Key{ProjectID: projectID, SessionID: sessionID}, payload)
		}
		return repository.WriteState(context.Background(), Key{ProjectID: projectID, SessionID: sessionID}, payload)
	})
}

// LoadCheckpointWorkspace 读取引擎续跑快照；不存在返回 fs.ErrNotExist。
func (router *Router) LoadCheckpointWorkspace(projectID, sessionID string) ([]byte, error) {
	var payload []byte
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		if jsonRepository, ok := repository.(*jsonRepository); ok {
			loaded, err := jsonRepository.readCheckpointLayout(Key{ProjectID: projectID, SessionID: sessionID})
			payload = loaded
			return err
		}
		loaded, err := repository.ReadState(context.Background(), Key{ProjectID: projectID, SessionID: sessionID})
		payload = loaded
		return err
	})
	return payload, err
}

// SaveSystemPromptWorkspace 把会话 system prompt 快照写进 metadata/system.json
// （S19）。返回 handled=false 表示当前后端未 v8 化（调用方保留 context blob
// 承载字段）。
func (router *Router) SaveSystemPromptWorkspace(projectID, sessionID, prompt, version string) (bool, error) {
	handled := false
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		jsonRepository, ok := repository.(*jsonRepository)
		if !ok {
			return nil
		}
		handled = true
		return jsonRepository.writeSystemPromptLayout(Key{ProjectID: projectID, SessionID: sessionID},
			systemPromptSnapshot{
				SessionID: sessionID, Prompt: prompt, Version: version, UpdatedAt: time.Now().UTC(),
			})
	})
	return handled, err
}

// LoadSystemPromptWorkspace 读取会话 system prompt 快照；handled=false 表示
// 当前后端未 v8 化（调用方回退 context blob）。
func (router *Router) LoadSystemPromptWorkspace(projectID, sessionID string) (systemPromptSnapshot, bool, error) {
	var snapshot systemPromptSnapshot
	handled := false
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		jsonRepository, ok := repository.(*jsonRepository)
		if !ok {
			return nil
		}
		handled = true
		loaded, err := jsonRepository.readSystemPromptLayout(Key{ProjectID: projectID, SessionID: sessionID})
		if err != nil {
			return err
		}
		snapshot = loaded
		return nil
	})
	return snapshot, handled, err
}

// CompactFramesWorkspace 读取 compact 通道已发布帧（S19：CompactStack 权威
// 在 session/compact.jsonl，不再依赖 context blob 双写）。handled=false 表示
// 当前后端未 v8 化（调用方保留 blob 承载）。
func (router *Router) CompactFramesWorkspace(projectID, sessionID string) ([]CompactFrame, bool, error) {
	var frames []CompactFrame
	handled := false
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		jsonRepository, ok := repository.(*jsonRepository)
		if !ok {
			return nil
		}
		handled = true
		key := Key{ProjectID: projectID, SessionID: sessionID}
		rows, err := readCompactFrameRows(jsonRepository.layout.compactFilePath(key))
		if err != nil {
			return err
		}
		for _, row := range rows {
			frames = append(frames, CompactFrame{
				SegmentID: row.FrameID, PrevSegmentID: row.PrevID,
				MessageFrom: row.MessageFrom, MessageTo: row.MessageTo,
				Summary: row.Summary, CompressedAt: row.CompressedAt,
			})
		}
		return nil
	})
	return frames, handled, err
}

// LayoutV8 报告当前后端是否 v8 JSON 布局（S19 消费面迁移判据）。
func (router *Router) LayoutV8() bool {
	handled := false
	_ = router.withRepositoryAt("", func(repository Repository, _ string) error {
		_, handled = repository.(*jsonRepository)
		return nil
	})
	return handled
}

// SessionMetaWorkspace 返回会话枚举 meta（message head.Meta）；handled=false
// 表示后端未 v8 化（调用方回退 record 通道）。
func (router *Router) SessionMetaWorkspace(projectID, sessionID string) (frameworkStorage.SessionMeta, bool, error) {
	var meta frameworkStorage.SessionMeta
	handled := false
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		jsonRepository, ok := repository.(*jsonRepository)
		if !ok {
			return nil
		}
		handled = true
		loaded, ok := jsonRepository.sessionMeta(Key{ProjectID: projectID, SessionID: sessionID})
		if ok {
			meta = loaded
		}
		return nil
	})
	return meta, handled, err
}

// SetSessionArchivedWorkspace 写/清 lifecycle archived_at（S20：已归档判据）。
func (router *Router) SetSessionArchivedWorkspace(projectID, sessionID string, archived bool) (bool, error) {
	handled := false
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		jsonRepository, ok := repository.(*jsonRepository)
		if !ok {
			return nil
		}
		handled = true
		return jsonRepository.layout.setLifecycleArchived(Key{ProjectID: projectID, SessionID: sessionID}, archived)
	})
	return handled, err
}

// SessionArchivedWorkspace 返回会话归档时间（零值 = 未归档）。
func (router *Router) SessionArchivedWorkspace(projectID, sessionID string) (time.Time, bool, error) {
	var archivedAt time.Time
	handled := false
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		jsonRepository, ok := repository.(*jsonRepository)
		if !ok {
			return nil
		}
		handled = true
		at, err := jsonRepository.layout.lifecycleArchivedAt(Key{ProjectID: projectID, SessionID: sessionID})
		archivedAt = at
		return err
	})
	return archivedAt, handled, err
}

// SaveSessionDisplayMetaWorkspace / LoadSessionDisplayMetaWorkspace：项目级
// 展示元数据（S20：JSON v8 落 project-*/session-meta.json；其余后端沿用
// state 通道）。
func (router *Router) SaveSessionDisplayMetaWorkspace(projectID string, payload []byte) error {
	return router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		if jsonRepository, ok := repository.(*jsonRepository); ok {
			return jsonRepository.writeSessionDisplayMetaLayout(projectID, payload)
		}
		return repository.WriteState(context.Background(), Key{ProjectID: projectID, SessionID: sessionMetaKey}, payload)
	})
}

func (router *Router) LoadSessionDisplayMetaWorkspace(projectID string) ([]byte, error) {
	var payload []byte
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		if jsonRepository, ok := repository.(*jsonRepository); ok {
			loaded, err := jsonRepository.readSessionDisplayMetaLayout(projectID)
			payload = loaded
			return err
		}
		loaded, err := repository.ReadState(context.Background(), Key{ProjectID: projectID, SessionID: sessionMetaKey})
		payload = loaded
		return err
	})
	return payload, err
}

// DerivedRecordWorkspace 返回派生 SessionRecord JSON（S20：record 通道退役后
// 由 message head.Meta + message 行 + lifecycle 派生）。handled=false 表示
// 后端未 v8 化；payload 为空 = 会话不存在。
func (router *Router) DerivedRecordWorkspace(projectID, sessionID string) ([]byte, bool, error) {
	var payload []byte
	handled := false
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		jsonRepository, ok := repository.(*jsonRepository)
		if !ok {
			return nil
		}
		handled = true
		derived, err := jsonRepository.derivedRecordPayload(Key{ProjectID: projectID, SessionID: sessionID})
		payload = derived
		return err
	})
	return payload, handled, err
}

// AppendGoalAuditEvent 把 goal 审计条目写进 EVENT 通道（S19：GoalAudit 的
// 权威从 context blob 迁到 EVENT goal.*）。handled=false 表示后端未 v8 化。
func (router *Router) AppendGoalAuditEvent(projectID, sessionID string, entry GoalAuditEntry) (bool, error) {
	handled := false
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		jsonRepository, ok := repository.(*jsonRepository)
		if !ok {
			return nil
		}
		handled = true
		return jsonRepository.appendGoalAuditLayout(Key{ProjectID: projectID, SessionID: sessionID}, entry)
	})
	return handled, err
}

// GoalAuditEvents 读取 EVENT 通道里的 goal 审计条目（按事件序，Seq 顺序编号）。
func (router *Router) GoalAuditEvents(projectID, sessionID string) ([]GoalAuditEntry, bool, error) {
	var entries []GoalAuditEntry
	handled := false
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		jsonRepository, ok := repository.(*jsonRepository)
		if !ok {
			return nil
		}
		handled = true
		loaded, err := jsonRepository.readGoalAuditLayout(Key{ProjectID: projectID, SessionID: sessionID})
		entries = loaded
		return err
	})
	return entries, handled, err
}

// SaveProjectRecord 写入项目级模块语义记录（project_refresh 工具的落盘路径）。
// 只读契约：会话不写项目记录；本方法只被 project_refresh 工具调用（plan.md §3.7.1）。
func (router *Router) SaveProjectRecord(projectID string, record ProjectRecord) error {
	return router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		return repository.WriteProjectRecord(context.Background(), projectID, record)
	})
}

// AppendFrameworkEvent 追加执行事实事件到会话级事件库
// （双轨事件的事实轨：event.Sink → sessionstore 事件库；slice 8）。
func (router *Router) AppendFrameworkEvent(ctx context.Context, sessionID string, entry EventLogEntry) error {
	return router.AppendFrameworkEventWorkspace(ctx, router.Workspace(), sessionID, entry)
}

// AppendFrameworkEventWorkspace 在显式项目作用域下追加执行事实事件。
func (router *Router) AppendFrameworkEventWorkspace(ctx context.Context, projectID, sessionID string, entry EventLogEntry) error {
	return router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		return repository.AppendFrameworkEvent(ctx, Key{ProjectID: projectID, SessionID: sessionID}, entry)
	})
}

// ReadFrameworkEvents 读取会话级执行事实事件库（按 Seq 排序）。
func (router *Router) ReadFrameworkEvents(ctx context.Context, sessionID string) ([]EventLogEntry, error) {
	return router.ReadFrameworkEventsWorkspace(ctx, router.Workspace(), sessionID)
}

// ReadFrameworkEventsWorkspace 在显式项目作用域下读取执行事实事件库。
func (router *Router) ReadFrameworkEventsWorkspace(ctx context.Context, projectID, sessionID string) ([]EventLogEntry, error) {
	var entries []EventLogEntry
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		var err error
		entries, err = repository.ReadFrameworkEvents(ctx, Key{ProjectID: projectID, SessionID: sessionID})
		return err
	})
	return entries, err
}

// LoadProjectRecord 读取项目级模块语义记录（会话开始前即可读）。
// 记录尚未构建时返回 fs.ErrNotExist / sql.ErrNoRows，由调用方按「未构建」处理。
func (router *Router) LoadProjectRecord(projectID string) (ProjectRecord, error) {
	var record ProjectRecord
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		var err error
		record, err = repository.ReadProjectRecord(context.Background(), projectID)
		return err
	})
	return record, err
}

func (router *Router) List() []frameworkStorage.SessionMeta {
	return router.ListWorkspace(router.Workspace())
}

// ListWorkspace lists sessions from an explicit project scope without
// changing the router's active write scope.
func (router *Router) ListWorkspace(projectID string) []frameworkStorage.SessionMeta {
	var result []frameworkStorage.SessionMeta
	_ = router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		var err error
		result, err = repository.List(context.Background(), projectID)
		return err
	})
	return result
}

func (router *Router) Delete(sessionID string) error {
	return router.DeleteWorkspace(router.Workspace(), sessionID)
}

// DeleteWorkspace deletes a session from an explicit project scope without
// changing the router's active write scope.
func (router *Router) DeleteWorkspace(projectID, sessionID string) error {
	return router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		return repository.Delete(context.Background(), Key{ProjectID: projectID, SessionID: sessionID})
	})
}

func (router *Router) MessageCount(sessionID string) (int, error) {
	messages, err := router.Load(sessionID)
	return len(messages), err
}

func (router *Router) Config() Config {
	router.mu.RLock()
	defer router.mu.RUnlock()
	return router.config.Safe()
}

func (router *Router) Test(ctx context.Context, config Config) error {
	router.mu.RLock()
	defaultPath := router.defaultPath
	router.mu.RUnlock()
	normalized, err := config.Normalize(defaultPath)
	if err != nil {
		return err
	}
	repository, err := Open(ctx, normalized)
	if err != nil {
		return err
	}
	defer repository.Close()
	return repository.Ping(ctx)
}

func (router *Router) Configure(ctx context.Context, config Config) error {
	router.mu.RLock()
	defaultPath := router.defaultPath
	router.mu.RUnlock()
	normalized, err := config.Normalize(defaultPath)
	if err != nil {
		return err
	}
	replacement, err := Open(ctx, normalized)
	if err != nil {
		return err
	}
	if err := replacement.Ping(ctx); err != nil {
		replacement.Close()
		return err
	}
	if err := saveConfig(router.configPath, normalized); err != nil {
		replacement.Close()
		return err
	}
	router.mu.Lock()
	router.waitOpsIdleLocked()
	old := router.repository
	router.repository = replacement
	router.config = normalized
	router.mu.Unlock()
	return old.Close()
}

func (router *Router) Close() error {
	router.mu.Lock()
	router.waitOpsIdleLocked()
	repository := router.repository
	router.repository = nil
	router.mu.Unlock()
	if repository == nil {
		return nil
	}
	return repository.Close()
}

func (router *Router) withRepository(fn func(Repository, string) error) error {
	repository, projectID, ok := router.acquireRepository(router.projectID)
	if !ok {
		return errors.New("session storage: repository is closed")
	}
	defer router.releaseRepository()
	return fn(repository, projectID)
}

func (router *Router) withRepositoryAt(projectID string, fn func(Repository, string) error) error {
	repository, resolvedProjectID, ok := router.acquireRepository(projectID)
	if !ok {
		return errors.New("session storage: repository is closed")
	}
	defer router.releaseRepository()
	return fn(repository, strings.TrimSpace(resolvedProjectID))
}

// acquireRepository 短暂取锁获取当前 repository 并登记在途操作；数据操作在
// 锁外执行（跨会话并行），Configure/Close 会在活跃归零后再关闭旧后端。
func (router *Router) acquireRepository(projectID string) (Repository, string, bool) {
	router.mu.RLock()
	repository := router.repository
	projectID = strings.TrimSpace(projectID)
	if repository != nil {
		router.ensureOpsCond()
		router.opsMu.Lock()
		router.activeOps++
		router.opsMu.Unlock()
	}
	router.mu.RUnlock()
	return repository, projectID, repository != nil
}

func (router *Router) releaseRepository() {
	router.opsMu.Lock()
	router.activeOps--
	if router.activeOps == 0 {
		router.opsCond.Broadcast()
	}
	router.opsMu.Unlock()
}

func (router *Router) ensureOpsCond() {
	router.opsOnce.Do(func() {
		router.opsCond = sync.NewCond(&router.opsMu)
	})
}

// waitOpsIdleLocked 等待在途操作归零（调用方持 router.mu）。
func (router *Router) waitOpsIdleLocked() {
	router.ensureOpsCond()
	router.opsMu.Lock()
	for router.activeOps > 0 {
		router.opsCond.Wait()
	}
	router.opsMu.Unlock()
}

func Open(ctx context.Context, config Config) (Repository, error) {
	switch config.Backend {
	case BackendJSON:
		return newJSONRepository(config.Path, config.SessionStorage)
	case BackendSQLite, BackendPostgreSQL, BackendRedis:
		return nil, retiredBackendError(config.Backend)
	default:
		return nil, fmt.Errorf("session storage: unsupported backend %q", config.Backend)
	}
}

type jsonRepository struct {
	root     string
	settings storageSettings
	mu       sync.RWMutex
	// v8 是 会话存储布局引擎（新建会话消息/模块 head；见 对应模块文件）。
	layout *storeEngine
	// attempts 是 会话共享的运行期尝试缓存（wire 最近 K 条装配）。
	attempts *attemptCache
}

func newJSONRepository(root string, settings storageSettings) (*jsonRepository, error) {
	return newJSONRepositoryWithLayout(root, settings)
}

func (repository *jsonRepository) WriteAtomic(_ context.Context, key Key, messages []types.Message) error {
	return repository.WriteCommit(context.Background(), key, Commit{ProviderHistory: messages})
}

func (repository *jsonRepository) WriteCommit(_ context.Context, key Key, commit Commit) error {
	if err := key.validate(); err != nil {
		return err
	}
	for _, result := range commit.ToolResults {
		if strings.TrimSpace(result.Ref) == "" {
			return errors.New("session storage: result ref is required")
		}
	}
	return repository.writeCommitLayout(key, commit)
}

// Read 只服务 v8 布局会话（D2/S12：旧 manifest 布局会话不再打开，读接口
// 一律返回 fs.ErrNotExist，由目录枚举在 List 层跳过并告警）。
func (repository *jsonRepository) Read(_ context.Context, key Key) ([]types.Message, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	if !repository.active(key) {
		return nil, fs.ErrNotExist
	}
	return repository.readAllLayoutMessages(key)
}

func (repository *jsonRepository) ReadRange(ctx context.Context, key Key, offset, limit int) ([]types.Message, int, error) {
	// limit <= 0 是"只取总数"语义（会话切换先探 total 再尾部窗口读）。
	if offset < 0 {
		return nil, 0, errors.New("session storage: invalid range")
	}
	if !repository.active(key) {
		return nil, 0, fs.ErrNotExist
	}
	return repository.readRangeLayout(key, offset, limit)
}

func (repository *jsonRepository) ReadEventTail(_ context.Context, key Key, tokenBudget, maxUnits int) ([]Event, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	if repository.active(key) {
		return repository.readEventTailLayout(key, tokenBudget, maxUnits)
	}
	// D2/S12：旧 manifest 布局会话不再打开。
	return nil, fs.ErrNotExist
}

func (repository *jsonRepository) ReadToolResult(_ context.Context, key Key, resultRef string) (ToolResult, error) {
	if err := key.validate(); err != nil {
		return ToolResult{}, err
	}
	if strings.TrimSpace(resultRef) == "" {
		return ToolResult{}, errors.New("session storage: result ref is required")
	}
	if !repository.active(key) {
		return ToolResult{}, fs.ErrNotExist
	}
	if repository.active(key) {
		if refs, refsErr := repository.layout.readToolResultRefs(key); refsErr == nil && !containsValue(refs, resultRef) {
			return ToolResult{}, fs.ErrNotExist
		}
		data, err := os.ReadFile(repository.toolResultPath(key, resultRef))
		if err != nil {
			return ToolResult{}, err
		}
		var result ToolResult
		if err := json.Unmarshal(data, &result); err != nil {
			return ToolResult{}, err
		}
		if result.Ref != resultRef {
			return ToolResult{}, errors.New("session storage: tool result reference mismatch")
		}
		return result, nil
	}
	return ToolResult{}, fs.ErrNotExist
}

func (repository *jsonRepository) ListToolResults(_ context.Context, key Key) ([]ToolResult, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	if !repository.active(key) {
		return nil, fs.ErrNotExist
	}
	if repository.active(key) {
		refs, refsErr := repository.layout.readToolResultRefs(key)
		var results []ToolResult
		if refsErr == nil {
			results = make([]ToolResult, 0, len(refs))
			for _, ref := range refs {
				data, readErr := os.ReadFile(repository.toolResultPath(key, ref))
				if readErr != nil {
					return nil, readErr
				}
				var result ToolResult
				if err := json.Unmarshal(data, &result); err != nil {
					return nil, err
				}
				if result.Ref != ref {
					return nil, errors.New("session storage: tool result reference mismatch")
				}
				results = append(results, result)
			}
			return results, nil
		}
		if !errors.Is(refsErr, fs.ErrNotExist) {
			return nil, refsErr
		}
		// head 缺失 = 尚无已发布 refs（含首次提交的「文件已写、head 未发布」
		// 过渡现场）。禁止回退目录扫描：blob 文件先于 refs head 落盘，扫描
		// 会把一次 commit 的半对 ref 暴露成撕裂快照（T-FK torn）。发布点是
		// refs head 的原子替换，head 缺失时按空返回。
		return []ToolResult{}, nil
	}
	return nil, fs.ErrNotExist
}

func (repository *jsonRepository) CurrentGeneration(_ context.Context, key Key) (string, error) {
	if err := key.validate(); err != nil {
		return "", err
	}
	if repository.active(key) {
		return repository.currentGenerationLayout(key)
	}
	// D2/S12：旧 manifest 布局会话不再打开。
	return "", fs.ErrNotExist
}

func (repository *jsonRepository) WriteState(_ context.Context, key Key, state []byte) error {
	if err := key.validate(); err != nil {
		return err
	}
	// D9/S20：state.json 停写（dev 阶段丢字段已接受）。
	return nil
}

func (repository *jsonRepository) ReadState(_ context.Context, key Key) ([]byte, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	// D9/S20：state.json 停读。
	return nil, fs.ErrNotExist
}

func (repository *jsonRepository) WriteProjectRecord(_ context.Context, projectID string, record ProjectRecord) error {
	if err := validateProjectID(projectID); err != nil {
		return err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	directory := repository.projectDir(projectID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("session storage: marshal project record: %w", err)
	}
	return writeAtomic(filepath.Join(directory, "project-record.json"), data, 0o600)
}

func (repository *jsonRepository) AppendFrameworkEvent(_ context.Context, key Key, entry EventLogEntry) error {
	if err := key.validate(); err != nil {
		return err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	directory := repository.sessionDir(key)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	path := filepath.Join(directory, frameworkEventLogFile)
	var entries []EventLogEntry
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		// 首次写入：迁移 v1 布局遗留的 events.json（所有 generation），
		// 独立 append-only 事实轨不随 generation rollover 失效。
		legacy, legacyErr := repository.legacyFrameworkEventEntriesLocked(directory)
		if legacyErr != nil {
			return legacyErr
		}
		entries = legacy
	} else if err != nil {
		return err
	} else {
		existing, readErr := repository.readEventLogLocked(path)
		if readErr != nil {
			return readErr
		}
		entries = existing
	}
	// merge 而非直接 append：同 Seq 重试幂等，乱序追加也保持 Seq 排序。
	entries = mergeEventLogEntries(entries, []EventLogEntry{entry})
	data, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("session storage: marshal event log: %w", err)
	}
	return writeAtomic(path, data, 0o600)
}

func (repository *jsonRepository) ReadFrameworkEvents(_ context.Context, key Key) ([]EventLogEntry, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	directory := repository.sessionDir(key)
	path := filepath.Join(directory, frameworkEventLogFile)
	if _, err := os.Stat(path); err == nil {
		return repository.readEventLogLocked(path)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	// v1 回退：旧布局未迁移的会话按 generation 扫描读取。
	return repository.legacyFrameworkEventEntriesLocked(directory)
}

// legacyFrameworkEventEntriesLocked 扫描 v1 布局遗留的 events.json（所有
// generation 目录及 session 根目录），按 Seq 去重合并。调用方必须持有
// jsonRepository.mu（读锁或写锁）。
func (repository *jsonRepository) legacyFrameworkEventEntriesLocked(directory string) ([]EventLogEntry, error) {
	var merged []EventLogEntry
	entries, err := os.ReadDir(directory)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []EventLogEntry{}, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "generation-") {
			continue
		}
		legacy, readErr := repository.readEventLogLocked(filepath.Join(directory, entry.Name(), frameworkEventLegacyFile))
		if readErr != nil {
			return nil, readErr
		}
		merged = mergeEventLogEntries(merged, legacy)
	}
	legacy, err := repository.readEventLogLocked(filepath.Join(directory, frameworkEventLegacyFile))
	if err != nil {
		return nil, err
	}
	return mergeEventLogEntries(merged, legacy), nil
}

// mergeEventLogEntries 合并两个按 Seq 升序的事件库切片，同 Seq 保留先出现的
// 条目（幂等：迁移与追加的重复落库不会产生重复事实）。
func mergeEventLogEntries(primary, extra []EventLogEntry) []EventLogEntry {
	if len(extra) == 0 {
		return primary
	}
	if len(primary) == 0 {
		return append([]EventLogEntry(nil), extra...)
	}
	merged := make([]EventLogEntry, 0, len(primary)+len(extra))
	seen := make(map[uint64]struct{}, len(primary)+len(extra))
	appendUnique := func(entry EventLogEntry) {
		if _, ok := seen[entry.Seq]; ok {
			return
		}
		seen[entry.Seq] = struct{}{}
		merged = append(merged, entry)
	}
	index := 0
	for _, entry := range extra {
		for index < len(primary) && primary[index].Seq < entry.Seq {
			appendUnique(primary[index])
			index++
		}
		appendUnique(entry)
	}
	for ; index < len(primary); index++ {
		appendUnique(primary[index])
	}
	return merged
}

// readEventLogLocked 读取事件库 JSON（不存在 → 空库；损坏 → 显式错误）。
// 调用方必须持有 jsonRepository.mu（读锁或写锁）。
func (repository *jsonRepository) readEventLogLocked(path string) ([]EventLogEntry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return []EventLogEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []EventLogEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("session storage: decode event log: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Seq < entries[j].Seq })
	return entries, nil
}

func (repository *jsonRepository) ReadProjectRecord(_ context.Context, projectID string) (ProjectRecord, error) {
	if err := validateProjectID(projectID); err != nil {
		return ProjectRecord{}, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	data, err := os.ReadFile(filepath.Join(repository.projectDir(projectID), "project-record.json"))
	if err != nil {
		return ProjectRecord{}, err
	}
	var record ProjectRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return ProjectRecord{}, err
	}
	return record, nil
}

func (repository *jsonRepository) List(_ context.Context, projectID string) ([]frameworkStorage.SessionMeta, error) {
	entries, err := os.ReadDir(repository.projectDir(projectID))
	if os.IsNotExist(err) {
		return []frameworkStorage.SessionMeta{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]frameworkStorage.SessionMeta, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(repository.projectDir(projectID), entry.Name())
		if meta, ok := readMetaFromDir(directory); ok {
			result = append(result, meta)
			continue
		}
		// D2/S12：只有 manifest.json 的旧布局目录不再打开——跳过（内容保留
		// 在磁盘上，由用户/运维处置），避免把旧会话当成 v8 会话载入。
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UpdatedAt.After(result[j].UpdatedAt) })
	return result, nil
}

func (repository *jsonRepository) Delete(_ context.Context, key Key) error {
	if err := key.validate(); err != nil {
		return err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if err := removeAllWithBackoff(repository.sessionDir(key)); err != nil {
		return err
	}
	return nil
}

// removeAllWithBackoff 对目录删除做与 rename 发布同一量级的有界退避：
// Windows 上瞬时外部句柄（扫描器/索引器）会让 RemoveAll 报 “being used”。
func removeAllWithBackoff(path string) error {
	var err error
	for _, wait := range renameBackoff {
		if wait > 0 {
			time.Sleep(wait)
		}
		err = os.RemoveAll(path)
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			return nil
		}
	}
	return err
}

func (repository *jsonRepository) Ping(context.Context) error { return nil }
func (repository *jsonRepository) Close() error {
	releaseDataRootLock(repository.root)
	return nil
}

func (repository *jsonRepository) writeToolResultLocked(key Key, result ToolResult) error {
	if strings.TrimSpace(result.Ref) == "" {
		return errors.New("session storage: result ref is required")
	}
	directory := filepath.Dir(repository.toolResultPath(key, result.Ref))
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return writeAtomic(repository.toolResultPath(key, result.Ref), data, 0o600)
}

func (repository *jsonRepository) projectDir(projectID string) string {
	return filepath.Join(repository.root, "project-"+hash(projectID))
}
func (repository *jsonRepository) sessionDir(key Key) string {
	return filepath.Join(repository.projectDir(key.ProjectID), "session-"+hash(key.SessionID))
}
func (repository *jsonRepository) toolResultPath(key Key, resultRef string) string {
	// S13：结果记录与超大输出同走 big_tool_result 单通道（独立后缀，避开
	// blob GC 的 .jsonl 枚举；旧 tool-results/ 目录不再产生）。
	return filepath.Join(repository.layout.blobDir(key), hash(resultRef)+".result.json")
}

func eventTokenCount(events []Event) int {
	total := 0
	for _, event := range events {
		total += event.TokenCount
	}
	return total
}

func selectEventTail(events []Event, tokenBudget, maxUnits int) []Event {
	if tokenBudget <= 0 || maxUnits <= 0 {
		return []Event{}
	}
	units := CompleteEventUnits(events)
	capacity := maxUnits
	if capacity > len(units) {
		capacity = len(units) // MaxInt 全量读等大上限不得触发 makeslice panic
	}
	selected := make([][]Event, 0, capacity)
	tokens := 0
	for index := len(units) - 1; index >= 0 && len(selected) < maxUnits; index-- {
		unitTokens := eventTokenCount(units[index])
		if tokens+unitTokens > tokenBudget {
			break
		}
		selected = append(selected, units[index])
		tokens += unitTokens
	}
	result := make([]Event, 0)
	for index := len(selected) - 1; index >= 0; index-- {
		result = append(result, selected[index]...)
	}
	return result
}

// CompleteEventUnits 把事件流切分为可见协议轮次单元（轮）：user 轮、
// assistant 文本轮、assistant 工具链轮（按调用 ID 配对 tool 结果）。孤儿
// tool 事件、未知角色与控制块不构成单元。中断（残缺）工具链轮与未回复的
// user 请求仍构成开放单元 —— UI 可见的轮次不得因单元切分而从冷加载
// provider 上下文消失；缺失的 tool 结果由装配层请求前补齐。
func CompleteEventUnits(events []Event) [][]Event {
	units := make([][]Event, 0, len(events))
	for index := 0; index < len(events); {
		event := events[index]
		switch {
		case event.Role == "user":
			unit, next := userEventUnit(events, index)
			if len(unit) > 0 {
				units = append(units, unit)
			}
			index = next
		case event.Role == "assistant" && len(event.ToolCalls) == 0:
			units = append(units, append([]Event(nil), event))
			index++
		case event.Role == "assistant" && len(event.ToolCalls) > 0:
			unit, next, complete := toolEventUnit(events, index)
			if complete {
				units = append(units, unit)
			} else if len(unit) > 0 {
				// 残缺（中断）工具链：保留已记录部分为开放单元，从链断裂点
				// 续扫 —— 不整体作废、不连坐跳到下一个 user。
				units = append(units, unit)
			}
			index = next
		default:
			// Orphan tool results and unknown roles are archive evidence but
			// never become provider context by themselves.
			index++
		}
	}
	return units
}

func userEventUnit(events []Event, start int) ([]Event, int) {
	unit := []Event{events[start]}
	index := start + 1
	for index < len(events) && events[index].Role != "user" {
		event := events[index]
		if event.Role != "assistant" {
			// 孤儿 tool / 异常角色：轮在此终止，产出已保留内容；孤儿消息由
			// 外层 default 跳过。
			return unit, index
		}
		if len(event.ToolCalls) == 0 {
			unit = append(unit, event)
			return unit, index + 1
		}
		toolUnit, next, _ := toolEventUnit(events, index)
		// 工具链完整或残缺（中断）都并入该轮；残缺链的缺失结果由装配层
		// 补齐。next 落在链断裂点，后续同轮文本/下一 user 不会被跳转丢弃。
		unit = append(unit, toolUnit...)
		index = next
	}
	// 到达下一个 user 或流末：产出开放单元。覆盖残缺工具链收尾、无回复的
	// user 请求（会话关闭/取消/进程在首个模型响应前退出）等可见轮次 ——
	// 丢弃会让持久化会话在 UI 可见但冷加载 provider 上下文缺失。
	return unit, index
}

func toolEventUnit(events []Event, start int) ([]Event, int, bool) {
	assistant := events[start]
	wanted := make(map[string]struct{}, len(assistant.ToolCalls))
	for _, call := range assistant.ToolCalls {
		if call.ID == "" {
			return nil, start + 1, false
		}
		if _, duplicate := wanted[call.ID]; duplicate {
			return nil, start + 1, false
		}
		wanted[call.ID] = struct{}{}
	}
	unit := []Event{assistant}
	seen := make(map[string]struct{}, len(wanted))
	index := start + 1
	for index < len(events) && len(seen) < len(wanted) {
		event := events[index]
		if event.Role != "tool" {
			break
		}
		if _, ok := wanted[event.ToolCallID]; !ok {
			break
		}
		if _, duplicate := seen[event.ToolCallID]; duplicate {
			break
		}
		seen[event.ToolCallID] = struct{}{}
		unit = append(unit, event)
		index++
	}
	return unit, index, len(seen) == len(wanted)
}

func appendUnique(values []string, value string) []string {
	if containsValue(values, value) {
		return values
	}
	return append(values, value)
}
func containsValue(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}
func randomID() string {
	data := make([]byte, 8)
	if _, err := rand.Read(data); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(data)
}

// renameBackoff 是原子发布 rename 的重试预算（累计 ≈77 ms）。
//
// Windows 下目标文件只要被任何句柄打开（实测为外部扫描器，非本进程读者），
// rename 立即返回 Access denied；实测持柄者松开耗时 0–18 ms，故短退避即可。
// 重试只能落在 rename 本身：数据 append 已经发生，提交级重试会重复追加。
var renameBackoff = []time.Duration{0, 2 * time.Millisecond, 5 * time.Millisecond,
	10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond}

func writeAtomic(path string, data []byte, permission os.FileMode) error {
	temp := path + "." + randomID() + ".tmp"
	if err := os.WriteFile(temp, data, permission); err != nil {
		return err
	}
	var lastErr error
	for _, wait := range renameBackoff {
		if wait > 0 {
			time.Sleep(wait)
		}
		if err := os.Rename(temp, path); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	_ = os.Remove(temp)
	return lastErr
}
func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var config Config
	err = json.Unmarshal(data, &config)
	return config, err
}
func saveConfig(path string, config Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, data, 0o600)
}
