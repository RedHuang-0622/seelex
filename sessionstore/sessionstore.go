// Package sessionstore provides atomic, project-scoped persistence for chat
// sessions. All backends implement the same logical snapshot contract.
package sessionstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
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
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

type Backend string

const (
	BackendJSON       Backend = "json"
	BackendSQLite     Backend = "sqlite"
	BackendPostgreSQL Backend = "postgres"
	BackendRedis      Backend = "redis"
	// defaultMessageShardSize 是 JSON 存储分片条数的默认值
	// （seele.yaml limits 段 message_shard_size 可调，0 = 默认）。
	defaultMessageShardSize = 100
)

type Config struct {
	Backend          Backend `json:"backend"`
	Path             string  `json:"path,omitempty"`
	DSN              string  `json:"dsn,omitempty"`
	MessageShardSize int     `json:"message_shard_size,omitempty"` // 分片条数（0 = 默认 100）
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
	case BackendSQLite:
		if strings.TrimSpace(config.Path) == "" {
			config.Path = filepath.Join(defaultPath, "sessions.db")
		}
	case BackendPostgreSQL, BackendRedis:
		if strings.TrimSpace(config.DSN) == "" {
			return Config{}, fmt.Errorf("session storage: %s DSN is required", config.Backend)
		}
	default:
		return Config{}, fmt.Errorf("session storage: unsupported backend %q", config.Backend)
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

// Event is the append-only transcript representation shared by every
// backend. TokenCount is recorded at event creation time so reverse reads do
// not need to retokenize the complete archive.
type Event struct {
	Seq              uint64          `json:"seq"`
	TaskID           string          `json:"task_id,omitempty"`
	Role             string          `json:"role"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
	Content          string          `json:"content,omitempty"`
	ToolCallID       string          `json:"tool_call_id,omitempty"`
	Name             string          `json:"name,omitempty"`
	ToolCalls        []EventToolCall `json:"tool_calls,omitempty"`
	ResultRef        string          `json:"result_ref,omitempty"`
	TokenCount       int             `json:"token_count"`
	CreatedAt        time.Time       `json:"created_at"`
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
	ReadToolResult(context.Context, Key, string) (ToolResult, error)
	WriteState(context.Context, Key, []byte) error
	ReadState(context.Context, Key) ([]byte, error)
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
}

func NewRouter(configPath, defaultPath string) (*Router, error) {
	config, err := loadConfig(configPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if errors.Is(err, fs.ErrNotExist) {
		config = Config{Backend: BackendJSON}
	}
	config, err = config.Normalize(defaultPath)
	if err != nil {
		return nil, err
	}
	repository, err := Open(context.Background(), config)
	if err != nil {
		return nil, err
	}
	return &Router{repository: repository, config: config, configPath: configPath, defaultPath: defaultPath}, nil
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
	current := router.config
	defaultPath := router.defaultPath
	router.mu.RUnlock()
	if requiresDSN(config.Backend) && strings.TrimSpace(config.DSN) == "" && current.Backend == config.Backend {
		config.DSN = current.DSN
	}
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
	current := router.config
	defaultPath := router.defaultPath
	router.mu.RUnlock()
	if requiresDSN(config.Backend) && strings.TrimSpace(config.DSN) == "" && current.Backend == config.Backend {
		config.DSN = current.DSN
	}
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
	old := router.repository
	router.repository = replacement
	router.config = normalized
	router.mu.Unlock()
	return old.Close()
}

func (router *Router) Close() error {
	router.mu.Lock()
	repository := router.repository
	router.repository = nil
	router.mu.Unlock()
	if repository == nil {
		return nil
	}
	return repository.Close()
}

func (router *Router) withRepository(fn func(Repository, string) error) error {
	router.mu.RLock()
	defer router.mu.RUnlock()
	if router.repository == nil {
		return errors.New("session storage: repository is closed")
	}
	return fn(router.repository, router.projectID)
}

func (router *Router) withRepositoryAt(projectID string, fn func(Repository, string) error) error {
	router.mu.RLock()
	defer router.mu.RUnlock()
	if router.repository == nil {
		return errors.New("session storage: repository is closed")
	}
	return fn(router.repository, strings.TrimSpace(projectID))
}

func Open(ctx context.Context, config Config) (Repository, error) {
	switch config.Backend {
	case BackendJSON:
		return newJSONRepository(config.Path, config.MessageShardSize)
	case BackendSQLite:
		return newSQLRepository(ctx, "sqlite", config.Path, "?")
	case BackendPostgreSQL:
		return newSQLRepository(ctx, "pgx", config.DSN, "$")
	case BackendRedis:
		return newRedisRepository(ctx, config.DSN)
	default:
		return nil, fmt.Errorf("session storage: unsupported backend %q", config.Backend)
	}
}

func requiresDSN(backend Backend) bool {
	return backend == BackendPostgreSQL || backend == BackendRedis
}

type jsonManifest struct {
	Generation string `json:"generation"`
	// HistoryShardCounts 每 history shard 的消息条数（窗口读优化：
	// ReadRange 据此只解析需要的尾部 shard；旧数据缺失时回退全量读）。
	HistoryShardCounts []int                        `json:"history_shard_counts,omitempty"`
	EventShardCount    int                          `json:"event_shard_count,omitempty"`
	ToolResultRefs     []string                     `json:"tool_result_refs,omitempty"`
	Meta               frameworkStorage.SessionMeta `json:"meta"`
}

type jsonRepository struct {
	root         string
	shardSize    int
	mu           sync.RWMutex
	legacyMu     sync.Mutex
	legacyCounts map[string][]int
}

func newJSONRepository(root string, shardSize int) (*jsonRepository, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("session storage: create JSON root: %w", err)
	}
	if shardSize <= 0 {
		shardSize = defaultMessageShardSize
	}
	return &jsonRepository{root: filepath.Clean(root), shardSize: shardSize, legacyCounts: make(map[string][]int)}, nil
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
	repository.mu.Lock()
	defer repository.mu.Unlock()
	directory := repository.sessionDir(key)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	previous, previousErr := repository.readManifest(directory)
	if previousErr != nil && !errors.Is(previousErr, fs.ErrNotExist) {
		return previousErr
	}
	events := commit.Events
	if previousErr == nil {
		existing, err := repository.readEventShards(key, previous)
		if err != nil {
			return err
		}
		events = mergeEvents(existing, commit.Events)
	}
	generation := "generation-" + randomID()
	generationDir := filepath.Join(directory, generation)
	if err := os.MkdirAll(generationDir, 0o700); err != nil {
		return err
	}
	shards := split(commit.ProviderHistory, repository.shardSize)
	for index, shard := range shards {
		data, err := json.Marshal(shard)
		if err != nil {
			return fmt.Errorf("session storage: marshal shard: %w", err)
		}
		if err := writeAtomic(filepath.Join(generationDir, fmt.Sprintf("history.%03d.json", index)), data, 0o600); err != nil {
			return err
		}
	}
	eventShards := splitEvents(events, repository.shardSize)
	for index, shard := range eventShards {
		data, err := json.Marshal(shard)
		if err != nil {
			return fmt.Errorf("session storage: marshal event shard: %w", err)
		}
		if err := writeAtomic(filepath.Join(generationDir, fmt.Sprintf("events.%03d.json", index)), data, 0o600); err != nil {
			return err
		}
	}
	state := commit.State
	if state == nil {
		state, _ = repository.readCurrentStateLocked(directory)
	}
	if state != nil {
		if err := writeAtomic(filepath.Join(generationDir, "state.json"), state, 0o600); err != nil {
			return err
		}
	}
	for _, result := range commit.ToolResults {
		if err := repository.writeToolResultLocked(key, result); err != nil {
			return err
		}
	}
	toolResultRefs := []string(nil)
	if previousErr == nil {
		toolResultRefs = append(toolResultRefs, previous.ToolResultRefs...)
	}
	for _, result := range commit.ToolResults {
		toolResultRefs = appendUnique(toolResultRefs, result.Ref)
	}
	now := time.Now().UTC()
	meta := frameworkStorage.SessionMeta{SessionID: key.SessionID, TokenCount: eventTokenCount(events), ShardCount: len(shards), UpdatedAt: now}
	if len(events) == 0 {
		meta.TokenCount = tokenCount(commit.ProviderHistory)
		meta.ShardCount = len(shards)
	}
	if previousErr == nil {
		meta.CreatedAt = previous.Meta.CreatedAt
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	data, err := json.Marshal(jsonManifest{
		Generation:         generation,
		HistoryShardCounts: shardCounts(shards),
		EventShardCount:    len(eventShards),
		ToolResultRefs:     toolResultRefs,
		Meta:               meta,
	})
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(directory, "manifest.json"), data, 0o600)
}

func (repository *jsonRepository) Read(_ context.Context, key Key) ([]types.Message, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	return repository.readAll(key)
}

func (repository *jsonRepository) ReadRange(ctx context.Context, key Key, offset, limit int) ([]types.Message, int, error) {
	// limit <= 0 是"只取总数"语义（会话切换先探 total 再尾部窗口读）。
	if offset < 0 {
		return nil, 0, errors.New("session storage: invalid range")
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	// 窗口读优化：manifest 记录每 shard 条数时，只解析覆盖 [offset, end)
	// 的 shard（尾部窗口通常只需最后 1-2 个 shard 文件），避免全量解析
	// 大会话（8MB+ 会话的 LoadHistoryRange 恢复路径）。旧数据无计数时
	// 回退全量读。
	directory := repository.sessionDir(key)
	manifest, err := repository.readManifest(directory)
	if err == nil && len(manifest.HistoryShardCounts) == 0 && manifest.Meta.ShardCount > 0 {
		counts, countErr := repository.legacyHistoryShardCounts(directory, manifest)
		if countErr != nil {
			return nil, 0, countErr
		}
		manifest.HistoryShardCounts = counts
	}
	if err == nil && len(manifest.HistoryShardCounts) > 0 {
		messages, total, err := repository.readRangeWindowed(directory, manifest, offset, limit)
		if err != nil {
			return nil, 0, err
		}
		if offset > total {
			return nil, total, errors.New("session storage: range offset exceeds history")
		}
		return messages, total, nil
	}
	messages, err := repository.readAll(key)
	if err != nil {
		return nil, 0, err
	}
	if offset > len(messages) {
		return nil, len(messages), errors.New("session storage: range offset exceeds history")
	}
	end := offset + limit
	if end > len(messages) {
		end = len(messages)
	}
	return append([]types.Message(nil), messages[offset:end]...), len(messages), nil
}

// readRangeWindowed 按 manifest shard 计数只解析覆盖目标范围的 shard。
// legacyHistoryShardCounts derives per-shard message counts for manifests
// written before history_shard_counts existed. Counting each JSON array with a
// decoder keeps the total-only probe bounded; the generation-keyed cache lets
// the immediately following tail read avoid parsing every shard again.
func (repository *jsonRepository) legacyHistoryShardCounts(directory string, manifest jsonManifest) ([]int, error) {
	key := filepath.Join(directory, manifest.Generation)
	repository.legacyMu.Lock()
	if counts, ok := repository.legacyCounts[key]; ok {
		copyCounts := append([]int(nil), counts...)
		repository.legacyMu.Unlock()
		return copyCounts, nil
	}
	repository.legacyMu.Unlock()

	counts := make([]int, manifest.Meta.ShardCount)
	for index := range counts {
		path := filepath.Join(directory, manifest.Generation, fmt.Sprintf("history.%03d.json", index))
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(file)
		token, err := decoder.Token()
		if err == nil {
			if delimiter, ok := token.(json.Delim); !ok || delimiter != '[' {
				err = fmt.Errorf("session storage: history shard %d is not an array", index)
			}
		}
		for err == nil && decoder.More() {
			var raw json.RawMessage
			if decodeErr := decoder.Decode(&raw); decodeErr != nil {
				err = decodeErr
				break
			}
			counts[index]++
		}
		if err == nil {
			_, err = decoder.Token()
		}
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return nil, fmt.Errorf("session storage: count legacy history shard %d: %w", index, err)
		}
	}

	repository.legacyMu.Lock()
	if len(repository.legacyCounts) >= 128 {
		for cachedKey := range repository.legacyCounts {
			delete(repository.legacyCounts, cachedKey)
			break
		}
	}
	repository.legacyCounts[key] = append([]int(nil), counts...)
	repository.legacyMu.Unlock()
	return counts, nil
}

func (repository *jsonRepository) readRangeWindowed(directory string, manifest jsonManifest, offset, limit int) ([]types.Message, int, error) {
	counts := manifest.HistoryShardCounts
	total := 0
	for _, count := range counts {
		total += count
	}
	if offset > total {
		return nil, total, nil
	}
	// limit <= 0 = 只取总数（会话切换先探 total 再尾部窗口读，不解析任何 shard）。
	if limit <= 0 {
		return nil, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	// 定位第一个需要解析的 shard（累计条数越过 offset）。
	startShard, seen := 0, 0
	for index, count := range counts {
		if offset < seen+count {
			startShard = index
			break
		}
		seen += count
	}
	// 从 startShard 读起，直到累计条数 >= end。
	var messages []types.Message
	accumulated := seen
	for index := startShard; index < len(counts); index++ {
		data, err := os.ReadFile(filepath.Join(directory, manifest.Generation, fmt.Sprintf("history.%03d.json", index)))
		if err != nil {
			return nil, 0, err
		}
		var shard []types.Message
		if err := json.Unmarshal(data, &shard); err != nil {
			return nil, 0, err
		}
		accumulated += counts[index]
		if accumulated > offset {
			start := offset - (accumulated - counts[index])
			if start < 0 {
				start = 0
			}
			stop := end - (accumulated - counts[index])
			if stop > counts[index] {
				stop = counts[index]
			}
			messages = append(messages, shard[start:stop]...)
		}
		if accumulated >= end {
			break
		}
	}
	return messages, total, nil
}

func (repository *jsonRepository) ReadEventTail(_ context.Context, key Key, tokenBudget, maxUnits int) ([]Event, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	manifest, err := repository.readManifest(repository.sessionDir(key))
	if err != nil {
		return nil, err
	}
	events, err := repository.readEventShards(key, manifest)
	if err != nil {
		return nil, err
	}
	return selectEventTail(events, tokenBudget, maxUnits), nil
}

func (repository *jsonRepository) ReadToolResult(_ context.Context, key Key, resultRef string) (ToolResult, error) {
	if err := key.validate(); err != nil {
		return ToolResult{}, err
	}
	if strings.TrimSpace(resultRef) == "" {
		return ToolResult{}, errors.New("session storage: result ref is required")
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	manifest, err := repository.readManifest(repository.sessionDir(key))
	if err != nil {
		return ToolResult{}, err
	}
	if !containsValue(manifest.ToolResultRefs, resultRef) {
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

func (repository *jsonRepository) WriteState(_ context.Context, key Key, state []byte) error {
	if err := key.validate(); err != nil {
		return err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	directory := repository.sessionDir(key)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if manifest, err := repository.readManifest(directory); err == nil {
		if err := writeAtomic(filepath.Join(directory, manifest.Generation, "state.json"), state, 0o600); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return writeAtomic(filepath.Join(directory, "state.json"), state, 0o600)
}

func (repository *jsonRepository) ReadState(_ context.Context, key Key) ([]byte, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	directory := repository.sessionDir(key)
	manifest, err := repository.readManifest(directory)
	if err == nil {
		state, readErr := os.ReadFile(filepath.Join(directory, manifest.Generation, "state.json"))
		if readErr == nil || !errors.Is(readErr, fs.ErrNotExist) {
			return state, readErr
		}
	}
	return os.ReadFile(filepath.Join(directory, "state.json"))
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
	path := filepath.Join(directory, "events.json")
	if manifest, err := repository.readManifest(directory); err == nil {
		path = filepath.Join(directory, manifest.Generation, "events.json")
	}
	entries, err := repository.readEventLogLocked(path)
	if err != nil {
		return err
	}
	entries = append(entries, entry)
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
	path := filepath.Join(directory, "events.json")
	if manifest, err := repository.readManifest(directory); err == nil {
		if _, statErr := os.Stat(filepath.Join(directory, manifest.Generation, "events.json")); statErr == nil {
			path = filepath.Join(directory, manifest.Generation, "events.json")
		}
	}
	return repository.readEventLogLocked(path)
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
	repository.mu.RLock()
	defer repository.mu.RUnlock()
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
		manifest, err := repository.readManifest(filepath.Join(repository.projectDir(projectID), entry.Name()))
		if err == nil {
			result = append(result, manifest.Meta)
		}
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
	if err := os.RemoveAll(repository.sessionDir(key)); err != nil {
		return err
	}
	return nil
}

func (repository *jsonRepository) Ping(context.Context) error { return nil }
func (repository *jsonRepository) Close() error               { return nil }

func (repository *jsonRepository) readAll(key Key) ([]types.Message, error) {
	directory := repository.sessionDir(key)
	manifest, err := repository.readManifest(directory)
	if err != nil {
		return nil, err
	}
	result := make([]types.Message, 0)
	for index := 0; index < manifest.Meta.ShardCount; index++ {
		data, err := os.ReadFile(filepath.Join(directory, manifest.Generation, fmt.Sprintf("history.%03d.json", index)))
		if err != nil {
			return nil, err
		}
		var shard []types.Message
		if err := json.Unmarshal(data, &shard); err != nil {
			return nil, err
		}
		result = append(result, shard...)
	}
	return result, nil
}

func (repository *jsonRepository) readEventShards(key Key, manifest jsonManifest) ([]Event, error) {
	if manifest.EventShardCount == 0 {
		return []Event{}, nil
	}
	directory := repository.sessionDir(key)
	events := make([]Event, 0, manifest.EventShardCount*repository.shardSize)
	for index := 0; index < manifest.EventShardCount; index++ {
		data, err := os.ReadFile(filepath.Join(directory, manifest.Generation, fmt.Sprintf("events.%03d.json", index)))
		if err != nil {
			return nil, err
		}
		var shard []Event
		if err := json.Unmarshal(data, &shard); err != nil {
			return nil, err
		}
		events = append(events, shard...)
	}
	return events, nil
}

func (repository *jsonRepository) readCurrentStateLocked(directory string) ([]byte, error) {
	manifest, err := repository.readManifest(directory)
	if err == nil {
		state, readErr := os.ReadFile(filepath.Join(directory, manifest.Generation, "state.json"))
		if readErr == nil || !errors.Is(readErr, fs.ErrNotExist) {
			return state, readErr
		}
	}
	return os.ReadFile(filepath.Join(directory, "state.json"))
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

func (repository *jsonRepository) readManifest(directory string) (jsonManifest, error) {
	data, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		return jsonManifest{}, err
	}
	var manifest jsonManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return jsonManifest{}, err
	}
	return manifest, nil
}

func (repository *jsonRepository) projectDir(projectID string) string {
	return filepath.Join(repository.root, "project-"+hash(projectID))
}
func (repository *jsonRepository) sessionDir(key Key) string {
	return filepath.Join(repository.projectDir(key.ProjectID), "session-"+hash(key.SessionID))
}
func (repository *jsonRepository) toolResultPath(key Key, resultRef string) string {
	return filepath.Join(repository.sessionDir(key), "tool-results", hash(resultRef)+".json")
}

// redisRepository uses the same immutable generation + fixed-size shard
// contract as the file and SQL implementations. All keys for a project share
// one Redis hash tag, so MULTI/EXEC commits a session manifest and its shards
// atomically even when Redis Cluster is enabled.
type redisRepository struct {
	client    *redis.Client
	namespace string
}

type redisManifest struct {
	SessionID          string `json:"session_id"`
	Generation         string `json:"generation"`
	TokenCount         int    `json:"token_count"`
	ShardCount         int    `json:"shard_count"`
	EventShardCount    int    `json:"event_shard_count,omitempty"`
	HistoryShardCounts []int  `json:"history_shard_counts,omitempty"` // 每 shard 消息条数（total-only 探测免全量读）
	CreatedAt          int64  `json:"created_at"`
	UpdatedAt          int64  `json:"updated_at"`
}

func newRedisRepository(ctx context.Context, dsn string) (*redisRepository, error) {
	options, err := redis.ParseURL(dsn)
	if err != nil {
		return nil, fmt.Errorf("session storage: parse Redis DSN: %w", err)
	}
	client := redis.NewClient(options)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return &redisRepository{client: client, namespace: "seelex"}, nil
}

func (repository *redisRepository) WriteAtomic(ctx context.Context, key Key, messages []types.Message) error {
	return repository.WriteCommit(ctx, key, Commit{ProviderHistory: messages})
}

func (repository *redisRepository) WriteCommit(ctx context.Context, key Key, commit Commit) error {
	if err := key.validate(); err != nil {
		return err
	}
	previous, err := repository.readManifest(ctx, key)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	events := commit.Events
	if previous != nil {
		existing, readErr := repository.readEvents(ctx, key, previous)
		if readErr != nil {
			return readErr
		}
		events = mergeEvents(existing, commit.Events)
	}
	shards := split(commit.ProviderHistory, defaultMessageShardSize)
	encoded := make([]string, len(shards))
	shardCounts := make([]int, len(shards))
	for index, shard := range shards {
		data, err := json.Marshal(shard)
		if err != nil {
			return fmt.Errorf("session storage: marshal shard: %w", err)
		}
		encoded[index] = string(data)
		shardCounts[index] = len(shard)
	}
	eventShards := splitEvents(events, defaultMessageShardSize)
	encodedEvents := make([]string, len(eventShards))
	for index, shard := range eventShards {
		data, err := json.Marshal(shard)
		if err != nil {
			return fmt.Errorf("session storage: marshal event shard: %w", err)
		}
		encodedEvents[index] = string(data)
	}
	now := time.Now().UTC().UnixNano()
	count := eventTokenCount(events)
	if len(events) == 0 {
		count = tokenCount(commit.ProviderHistory)
	}
	manifest := redisManifest{SessionID: key.SessionID, Generation: "generation-" + randomID(), TokenCount: count, ShardCount: len(encoded), EventShardCount: len(encodedEvents), HistoryShardCounts: shardCounts, CreatedAt: now, UpdatedAt: now}
	if previous != nil {
		manifest.CreatedAt = previous.CreatedAt
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	_, err = repository.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		if previous != nil {
			for index := 0; index < previous.ShardCount; index++ {
				pipe.Del(ctx, repository.shardKey(key, previous.Generation, index))
			}
			for index := 0; index < previous.EventShardCount; index++ {
				pipe.Del(ctx, repository.eventShardKey(key, previous.Generation, index))
			}
		}
		for index, shard := range encoded {
			pipe.Set(ctx, repository.shardKey(key, manifest.Generation, index), shard, 0)
		}
		for index, shard := range encodedEvents {
			pipe.Set(ctx, repository.eventShardKey(key, manifest.Generation, index), shard, 0)
		}
		if commit.State != nil {
			pipe.Set(ctx, repository.stateKey(key), commit.State, 0)
		}
		for _, result := range commit.ToolResults {
			if strings.TrimSpace(result.Ref) == "" {
				return errors.New("session storage: result ref is required")
			}
			payload, marshalErr := json.Marshal(result)
			if marshalErr != nil {
				return marshalErr
			}
			pipe.Set(ctx, repository.toolResultKey(key, result.Ref), payload, 0)
			pipe.SAdd(ctx, repository.toolResultIndexKey(key), result.Ref)
		}
		pipe.Set(ctx, repository.manifestKey(key), string(data), 0)
		pipe.ZAdd(ctx, repository.projectIndexKey(key.ProjectID), redis.Z{Score: float64(now), Member: key.SessionID})
		return nil
	})
	return err
}

func (repository *redisRepository) Read(ctx context.Context, key Key) ([]types.Message, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	manifest, err := repository.readManifest(ctx, key)
	if err != nil {
		return nil, err
	}
	keys := make([]string, manifest.ShardCount)
	for index := range keys {
		keys[index] = repository.shardKey(key, manifest.Generation, index)
	}
	values, err := repository.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	messages := make([]types.Message, 0, manifest.ShardCount*defaultMessageShardSize)
	for index, value := range values {
		data, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("session storage: missing shard %d", index)
		}
		var shard []types.Message
		if err := json.Unmarshal([]byte(data), &shard); err != nil {
			return nil, err
		}
		messages = append(messages, shard...)
	}
	return messages, nil
}

func (repository *redisRepository) ReadRange(ctx context.Context, key Key, offset, limit int) ([]types.Message, int, error) {
	// limit <= 0 = 只取总数（与 jsonRepository 语义一致）。
	// total-only 从 manifest 的 HistoryShardCounts 求和（只读 manifest，
	// 不解析任何 shard）；旧 manifest 无计数时回退全量读。
	if offset < 0 {
		return nil, 0, errors.New("session storage: invalid range")
	}
	if limit <= 0 {
		manifest, err := repository.readManifest(ctx, key)
		if err != nil {
			return nil, 0, err
		}
		if total := sumShardCounts(manifest.HistoryShardCounts); total >= 0 {
			return nil, total, nil
		}
		messages, err := repository.Read(ctx, key)
		if err != nil {
			return nil, 0, err
		}
		return nil, len(messages), nil
	}
	messages, err := repository.Read(ctx, key)
	if err != nil {
		return nil, 0, err
	}
	if offset > len(messages) {
		return nil, len(messages), errors.New("session storage: range offset exceeds history")
	}
	end := offset + limit
	if end > len(messages) {
		end = len(messages)
	}
	return append([]types.Message(nil), messages[offset:end]...), len(messages), nil
}

func (repository *redisRepository) ReadEventTail(ctx context.Context, key Key, tokenBudget, maxUnits int) ([]Event, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	manifest, err := repository.readManifest(ctx, key)
	if err != nil {
		return nil, err
	}
	events, err := repository.readEvents(ctx, key, manifest)
	if err != nil {
		return nil, err
	}
	return selectEventTail(events, tokenBudget, maxUnits), nil
}

func (repository *redisRepository) readEvents(ctx context.Context, key Key, manifest *redisManifest) ([]Event, error) {
	keys := make([]string, manifest.EventShardCount)
	for index := range keys {
		keys[index] = repository.eventShardKey(key, manifest.Generation, index)
	}
	if len(keys) == 0 {
		return []Event{}, nil
	}
	values, err := repository.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	events := make([]Event, 0, manifest.EventShardCount*defaultMessageShardSize)
	for index, value := range values {
		data, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("session storage: missing event shard %d", index)
		}
		var shard []Event
		if err := json.Unmarshal([]byte(data), &shard); err != nil {
			return nil, err
		}
		events = append(events, shard...)
	}
	return events, nil
}

func (repository *redisRepository) ReadToolResult(ctx context.Context, key Key, resultRef string) (ToolResult, error) {
	if err := key.validate(); err != nil {
		return ToolResult{}, err
	}
	data, err := repository.client.Get(ctx, repository.toolResultKey(key, resultRef)).Bytes()
	if errors.Is(err, redis.Nil) {
		return ToolResult{}, fs.ErrNotExist
	}
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

func (repository *redisRepository) WriteState(ctx context.Context, key Key, state []byte) error {
	if err := key.validate(); err != nil {
		return err
	}
	return repository.client.Set(ctx, repository.stateKey(key), state, 0).Err()
}

func (repository *redisRepository) ReadState(ctx context.Context, key Key) ([]byte, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	value, err := repository.client.Get(ctx, repository.stateKey(key)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, fs.ErrNotExist
	}
	return value, err
}

func (repository *redisRepository) WriteProjectRecord(ctx context.Context, projectID string, record ProjectRecord) error {
	if err := validateProjectID(projectID); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("session storage: marshal project record: %w", err)
	}
	return repository.client.Set(ctx, repository.projectRecordKey(projectID), data, 0).Err()
}

func (repository *redisRepository) AppendFrameworkEvent(ctx context.Context, key Key, entry EventLogEntry) error {
	if err := key.validate(); err != nil {
		return err
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("session storage: marshal event log entry: %w", err)
	}
	return repository.client.RPush(ctx, repository.eventLogKey(key), data).Err()
}

func (repository *redisRepository) ReadFrameworkEvents(ctx context.Context, key Key) ([]EventLogEntry, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	values, err := repository.client.LRange(ctx, repository.eventLogKey(key), 0, -1).Result()
	if errors.Is(err, redis.Nil) {
		return []EventLogEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	entries := make([]EventLogEntry, 0, len(values))
	for _, value := range values {
		var entry EventLogEntry
		if err := json.Unmarshal([]byte(value), &entry); err != nil {
			return nil, fmt.Errorf("session storage: decode event log entry: %w", err)
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Seq < entries[j].Seq })
	return entries, nil
}

func (repository *redisRepository) ReadProjectRecord(ctx context.Context, projectID string) (ProjectRecord, error) {
	if err := validateProjectID(projectID); err != nil {
		return ProjectRecord{}, err
	}
	data, err := repository.client.Get(ctx, repository.projectRecordKey(projectID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return ProjectRecord{}, fs.ErrNotExist
	}
	if err != nil {
		return ProjectRecord{}, err
	}
	var record ProjectRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return ProjectRecord{}, err
	}
	return record, nil
}

func (repository *redisRepository) List(ctx context.Context, projectID string) ([]frameworkStorage.SessionMeta, error) {
	ids, err := repository.client.ZRevRange(ctx, repository.projectIndexKey(projectID), 0, -1).Result()
	if err != nil {
		return nil, err
	}
	result := make([]frameworkStorage.SessionMeta, 0, len(ids))
	for _, sessionID := range ids {
		manifest, err := repository.readManifest(ctx, Key{ProjectID: projectID, SessionID: sessionID})
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		result = append(result, manifest.meta())
	}
	return result, nil
}

func (repository *redisRepository) Delete(ctx context.Context, key Key) error {
	if err := key.validate(); err != nil {
		return err
	}
	manifest, err := repository.readManifest(ctx, key)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	resultRefs, refsErr := repository.client.SMembers(ctx, repository.toolResultIndexKey(key)).Result()
	if refsErr != nil && !errors.Is(refsErr, redis.Nil) {
		return refsErr
	}
	_, err = repository.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Del(ctx, repository.manifestKey(key), repository.stateKey(key), repository.eventLogKey(key), repository.toolResultIndexKey(key))
		if manifest != nil {
			for index := 0; index < manifest.ShardCount; index++ {
				pipe.Del(ctx, repository.shardKey(key, manifest.Generation, index))
			}
			for index := 0; index < manifest.EventShardCount; index++ {
				pipe.Del(ctx, repository.eventShardKey(key, manifest.Generation, index))
			}
		}
		for _, resultRef := range resultRefs {
			pipe.Del(ctx, repository.toolResultKey(key, resultRef))
		}
		pipe.ZRem(ctx, repository.projectIndexKey(key.ProjectID), key.SessionID)
		return nil
	})
	return err
}

func (repository *redisRepository) Ping(ctx context.Context) error {
	return repository.client.Ping(ctx).Err()
}
func (repository *redisRepository) Close() error { return repository.client.Close() }

func (repository *redisRepository) readManifest(ctx context.Context, key Key) (*redisManifest, error) {
	data, err := repository.client.Get(ctx, repository.manifestKey(key)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, fs.ErrNotExist
	}
	if err != nil {
		return nil, err
	}
	var manifest redisManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func (manifest redisManifest) meta() frameworkStorage.SessionMeta {
	return frameworkStorage.SessionMeta{SessionID: manifest.SessionID, TokenCount: manifest.TokenCount, ShardCount: manifest.ShardCount, CreatedAt: time.Unix(0, manifest.CreatedAt).UTC(), UpdatedAt: time.Unix(0, manifest.UpdatedAt).UTC()}
}

func (repository *redisRepository) projectKey(projectID string) string {
	return repository.namespace + ":project:{" + hash(projectID) + "}"
}
func (repository *redisRepository) projectIndexKey(projectID string) string {
	return repository.projectKey(projectID) + ":sessions"
}
func (repository *redisRepository) projectRecordKey(projectID string) string {
	return repository.projectKey(projectID) + ":project-record"
}
func (repository *redisRepository) sessionKey(key Key) string {
	return repository.projectKey(key.ProjectID) + ":session:" + hash(key.SessionID)
}
func (repository *redisRepository) manifestKey(key Key) string {
	return repository.sessionKey(key) + ":manifest"
}
func (repository *redisRepository) stateKey(key Key) string {
	return repository.sessionKey(key) + ":state"
}
func (repository *redisRepository) eventLogKey(key Key) string {
	return repository.sessionKey(key) + ":framework-events"
}
func (repository *redisRepository) shardKey(key Key, generation string, index int) string {
	return fmt.Sprintf("%s:history:%s:%03d", repository.sessionKey(key), generation, index)
}
func (repository *redisRepository) eventShardKey(key Key, generation string, index int) string {
	return fmt.Sprintf("%s:events:%s:%03d", repository.sessionKey(key), generation, index)
}
func (repository *redisRepository) toolResultIndexKey(key Key) string {
	return repository.sessionKey(key) + ":tool-results"
}
func (repository *redisRepository) toolResultKey(key Key, resultRef string) string {
	return repository.sessionKey(key) + ":tool-result:" + hash(resultRef)
}

type sqlRepository struct {
	db          *sql.DB
	placeholder string
}

func newSQLRepository(ctx context.Context, driver, dsn, placeholder string) (*sqlRepository, error) {
	if driver == "sqlite" {
		if err := os.MkdirAll(filepath.Dir(dsn), 0o700); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	repository := &sqlRepository{db: db, placeholder: placeholder}
	if err := repository.Ping(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return repository, nil
}

func (repository *sqlRepository) Ping(ctx context.Context) error {
	if err := repository.db.PingContext(ctx); err != nil {
		return err
	}
	_, err := repository.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS seelex_sessions (
project_id TEXT NOT NULL, session_id TEXT NOT NULL, messages_json TEXT NOT NULL,
token_count INTEGER NOT NULL, shard_count INTEGER NOT NULL, created_at BIGINT NOT NULL,
updated_at BIGINT NOT NULL, PRIMARY KEY (project_id, session_id))`)
	if err != nil {
		return err
	}
	_, err = repository.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS seelex_session_manifest (
project_id TEXT NOT NULL, session_id TEXT NOT NULL, generation TEXT NOT NULL,
token_count INTEGER NOT NULL, shard_count INTEGER NOT NULL, created_at BIGINT NOT NULL,
updated_at BIGINT NOT NULL, PRIMARY KEY (project_id, session_id))`)
	if err != nil {
		return err
	}
	_, err = repository.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS seelex_session_shard (
project_id TEXT NOT NULL, session_id TEXT NOT NULL, generation TEXT NOT NULL,
shard_index INTEGER NOT NULL, messages_json TEXT NOT NULL,
PRIMARY KEY (project_id, session_id, generation, shard_index))`)
	if err != nil {
		return err
	}
	_, err = repository.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS seelex_session_manifest_project_updated
ON seelex_session_manifest (project_id, updated_at)`)
	if err != nil {
		return err
	}
	_, err = repository.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS seelex_session_state (
project_id TEXT NOT NULL, session_id TEXT NOT NULL, state_json TEXT NOT NULL,
updated_at BIGINT NOT NULL, PRIMARY KEY (project_id, session_id))`)
	if err != nil {
		return err
	}
	_, err = repository.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS seelex_session_event_shard (
project_id TEXT NOT NULL, session_id TEXT NOT NULL, generation TEXT NOT NULL,
shard_index INTEGER NOT NULL, events_json TEXT NOT NULL,
PRIMARY KEY (project_id, session_id, generation, shard_index))`)
	if err != nil {
		return err
	}
	_, err = repository.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS seelex_tool_result (
project_id TEXT NOT NULL, session_id TEXT NOT NULL, result_ref TEXT NOT NULL,
tool_name TEXT NOT NULL, content TEXT NOT NULL, digest TEXT NOT NULL,
size_bytes INTEGER NOT NULL, token_count INTEGER NOT NULL, created_at BIGINT NOT NULL,
PRIMARY KEY (project_id, session_id, result_ref))`)
	if err != nil {
		return err
	}
	_, err = repository.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS seelex_project_record (
project_id TEXT NOT NULL, record_json TEXT NOT NULL, updated_at BIGINT NOT NULL,
PRIMARY KEY (project_id))`)
	if err != nil {
		return err
	}
	_, err = repository.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS seelex_framework_event (
project_id TEXT NOT NULL, session_id TEXT NOT NULL, seq BIGINT NOT NULL,
event_json TEXT NOT NULL, created_at BIGINT NOT NULL,
PRIMARY KEY (project_id, session_id, seq))`)
	return err
}

func (repository *sqlRepository) WriteAtomic(ctx context.Context, key Key, messages []types.Message) error {
	return repository.WriteCommit(ctx, key, Commit{ProviderHistory: messages})
}

func (repository *sqlRepository) WriteCommit(ctx context.Context, key Key, commit Commit) error {
	if err := key.validate(); err != nil {
		return err
	}
	existingEvents, existingErr := repository.readAllEvents(ctx, key)
	if existingErr != nil && !errors.Is(existingErr, sql.ErrNoRows) {
		return existingErr
	}
	events := mergeEvents(existingEvents, commit.Events)
	shards := split(commit.ProviderHistory, defaultMessageShardSize)
	encoded := make([]string, len(shards))
	for index, shard := range shards {
		data, err := json.Marshal(shard)
		if err != nil {
			return fmt.Errorf("session storage: marshal shard: %w", err)
		}
		encoded[index] = string(data)
	}
	eventShards := splitEvents(events, defaultMessageShardSize)
	encodedEvents := make([]string, len(eventShards))
	for index, shard := range eventShards {
		data, err := json.Marshal(shard)
		if err != nil {
			return fmt.Errorf("session storage: marshal event shard: %w", err)
		}
		encodedEvents[index] = string(data)
	}
	now := time.Now().UTC().UnixNano()
	transaction, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	createdAt := now
	query := `SELECT created_at FROM seelex_session_manifest WHERE project_id=` + repository.arg(1) + ` AND session_id=` + repository.arg(2)
	if err := transaction.QueryRowContext(ctx, query, key.ProjectID, key.SessionID).Scan(&createdAt); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	generation := "generation-" + randomID()
	insertShard := `INSERT INTO seelex_session_shard (project_id,session_id,generation,shard_index,messages_json) VALUES (` + repository.placeholders(5) + `)`
	for index, shard := range encoded {
		if _, err := transaction.ExecContext(ctx, insertShard, key.ProjectID, key.SessionID, generation, index, shard); err != nil {
			return err
		}
	}
	insertEventShard := `INSERT INTO seelex_session_event_shard (project_id,session_id,generation,shard_index,events_json) VALUES (` + repository.placeholders(5) + `)`
	for index, shard := range encodedEvents {
		if _, err := transaction.ExecContext(ctx, insertEventShard, key.ProjectID, key.SessionID, generation, index, shard); err != nil {
			return err
		}
	}
	count := eventTokenCount(events)
	if len(events) == 0 {
		count = tokenCount(commit.ProviderHistory)
	}
	manifest := `INSERT INTO seelex_session_manifest (project_id,session_id,generation,token_count,shard_count,created_at,updated_at) VALUES (` + repository.placeholders(7) + `)
ON CONFLICT (project_id,session_id) DO UPDATE SET generation=excluded.generation, token_count=excluded.token_count, shard_count=excluded.shard_count, updated_at=excluded.updated_at`
	if _, err := transaction.ExecContext(ctx, manifest, key.ProjectID, key.SessionID, generation, count, len(encoded), createdAt, now); err != nil {
		return err
	}
	cleanup := `DELETE FROM seelex_session_shard WHERE project_id=` + repository.arg(1) + ` AND session_id=` + repository.arg(2) + ` AND generation<>` + repository.arg(3)
	if _, err := transaction.ExecContext(ctx, cleanup, key.ProjectID, key.SessionID, generation); err != nil {
		return err
	}
	cleanupEvents := `DELETE FROM seelex_session_event_shard WHERE project_id=` + repository.arg(1) + ` AND session_id=` + repository.arg(2) + ` AND generation<>` + repository.arg(3)
	if _, err := transaction.ExecContext(ctx, cleanupEvents, key.ProjectID, key.SessionID, generation); err != nil {
		return err
	}
	if commit.State != nil {
		stateQuery := `INSERT INTO seelex_session_state (project_id,session_id,state_json,updated_at) VALUES (` + repository.placeholders(4) + `)
ON CONFLICT (project_id,session_id) DO UPDATE SET state_json=excluded.state_json, updated_at=excluded.updated_at`
		if _, err := transaction.ExecContext(ctx, stateQuery, key.ProjectID, key.SessionID, string(commit.State), now); err != nil {
			return err
		}
	}
	toolResultQuery := `INSERT INTO seelex_tool_result (project_id,session_id,result_ref,tool_name,content,digest,size_bytes,token_count,created_at) VALUES (` + repository.placeholders(9) + `)
ON CONFLICT (project_id,session_id,result_ref) DO NOTHING`
	for _, result := range commit.ToolResults {
		if strings.TrimSpace(result.Ref) == "" {
			return errors.New("session storage: result ref is required")
		}
		if _, err := transaction.ExecContext(ctx, toolResultQuery, key.ProjectID, key.SessionID, result.Ref, result.Tool, result.Content, result.Digest, result.Size, result.TokenCount, result.CreatedAt.UTC().UnixNano()); err != nil {
			return err
		}
	}
	legacy := `DELETE FROM seelex_sessions WHERE project_id=` + repository.arg(1) + ` AND session_id=` + repository.arg(2)
	if _, err := transaction.ExecContext(ctx, legacy, key.ProjectID, key.SessionID); err != nil {
		return err
	}
	return transaction.Commit()
}

func (repository *sqlRepository) Read(ctx context.Context, key Key) ([]types.Message, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	manifest, err := repository.readManifest(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return repository.readLegacy(ctx, key)
	}
	if err != nil {
		return nil, err
	}
	return repository.readGeneration(ctx, key, manifest.generation, manifest.shardCount)
}

func (repository *sqlRepository) ReadRange(ctx context.Context, key Key, offset, limit int) ([]types.Message, int, error) {
	// limit <= 0 = 只取总数（三后端契约统一；会话切换先探 total 再尾部窗口读）。
	if offset < 0 {
		return nil, 0, errors.New("session storage: invalid range")
	}
	if limit <= 0 {
		total, err := repository.messageCount(ctx, key)
		if err != nil {
			return nil, 0, err
		}
		return nil, total, nil
	}
	messages, err := repository.Read(ctx, key)
	if err != nil {
		return nil, 0, err
	}
	if offset > len(messages) {
		return nil, len(messages), errors.New("session storage: range offset exceeds history")
	}
	end := offset + limit
	if end > len(messages) {
		end = len(messages)
	}
	return append([]types.Message(nil), messages[offset:end]...), len(messages), nil
}

// messageCount 返回会话消息总数：manifest 存在时按 shard 的 JSON 数组长度
// 求和（SQL 计数查询，不解析消息体）；legacy 无 manifest 时回退全量读。
func (repository *sqlRepository) messageCount(ctx context.Context, key Key) (int, error) {
	manifest, err := repository.readManifest(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		messages, legacyErr := repository.readLegacy(ctx, key)
		if legacyErr != nil {
			return 0, legacyErr
		}
		return len(messages), nil
	}
	if err != nil {
		return 0, err
	}
	var total int
	query := `SELECT COALESCE(SUM(json_array_length(messages_json)), 0) FROM seelex_session_shard WHERE project_id=` + repository.arg(1) + ` AND session_id=` + repository.arg(2) + ` AND generation=` + repository.arg(3)
	if err := repository.db.QueryRowContext(ctx, query, key.ProjectID, key.SessionID, manifest.generation).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (repository *sqlRepository) ReadEventTail(ctx context.Context, key Key, tokenBudget, maxUnits int) ([]Event, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	events, err := repository.readAllEvents(ctx, key)
	if err != nil {
		return nil, err
	}
	return selectEventTail(events, tokenBudget, maxUnits), nil
}

func (repository *sqlRepository) readAllEvents(ctx context.Context, key Key) ([]Event, error) {
	manifest, err := repository.readManifest(ctx, key)
	if err != nil {
		return nil, err
	}
	query := `SELECT shard_index,events_json FROM seelex_session_event_shard WHERE project_id=` + repository.arg(1) + ` AND session_id=` + repository.arg(2) + ` AND generation=` + repository.arg(3) + ` ORDER BY shard_index ASC`
	rows, err := repository.db.QueryContext(ctx, query, key.ProjectID, key.SessionID, manifest.generation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]Event, 0)
	expected := 0
	for rows.Next() {
		var index int
		var data string
		if err := rows.Scan(&index, &data); err != nil {
			return nil, err
		}
		if index != expected {
			return nil, fmt.Errorf("session storage: missing event shard %d", expected)
		}
		var shard []Event
		if err := json.Unmarshal([]byte(data), &shard); err != nil {
			return nil, err
		}
		events = append(events, shard...)
		expected++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func (repository *sqlRepository) ReadToolResult(ctx context.Context, key Key, resultRef string) (ToolResult, error) {
	if err := key.validate(); err != nil {
		return ToolResult{}, err
	}
	query := `SELECT tool_name,content,digest,size_bytes,token_count,created_at FROM seelex_tool_result WHERE project_id=` + repository.arg(1) + ` AND session_id=` + repository.arg(2) + ` AND result_ref=` + repository.arg(3)
	result := ToolResult{Ref: resultRef}
	var createdAt int64
	if err := repository.db.QueryRowContext(ctx, query, key.ProjectID, key.SessionID, resultRef).Scan(&result.Tool, &result.Content, &result.Digest, &result.Size, &result.TokenCount, &createdAt); err != nil {
		return ToolResult{}, err
	}
	result.CreatedAt = time.Unix(0, createdAt).UTC()
	return result, nil
}

func (repository *sqlRepository) WriteState(ctx context.Context, key Key, state []byte) error {
	if err := key.validate(); err != nil {
		return err
	}
	query := `INSERT INTO seelex_session_state (project_id,session_id,state_json,updated_at) VALUES (` + repository.placeholders(4) + `)
ON CONFLICT (project_id,session_id) DO UPDATE SET state_json=excluded.state_json, updated_at=excluded.updated_at`
	_, err := repository.db.ExecContext(ctx, query, key.ProjectID, key.SessionID, string(state), time.Now().UTC().UnixNano())
	return err
}

func (repository *sqlRepository) AppendFrameworkEvent(ctx context.Context, key Key, entry EventLogEntry) error {
	if err := key.validate(); err != nil {
		return err
	}
	query := `INSERT INTO seelex_framework_event (project_id,session_id,seq,event_json,created_at) VALUES (` + repository.placeholders(5) + `)
ON CONFLICT (project_id,session_id,seq) DO UPDATE SET event_json=excluded.event_json, created_at=excluded.created_at`
	_, err := repository.db.ExecContext(ctx, query, key.ProjectID, key.SessionID, int64(entry.Seq), string(entry.Payload), time.Now().UTC().UnixNano())
	return err
}

func (repository *sqlRepository) ReadFrameworkEvents(ctx context.Context, key Key) ([]EventLogEntry, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	query := `SELECT seq,event_json FROM seelex_framework_event WHERE project_id=` + repository.arg(1) + ` AND session_id=` + repository.arg(2) + ` ORDER BY seq`
	rows, err := repository.db.QueryContext(ctx, query, key.ProjectID, key.SessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]EventLogEntry, 0)
	for rows.Next() {
		var entry EventLogEntry
		var payload string
		if err := rows.Scan(&entry.Seq, &payload); err != nil {
			return nil, err
		}
		entry.Payload = json.RawMessage(payload)
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func (repository *sqlRepository) ReadState(ctx context.Context, key Key) ([]byte, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	query := `SELECT state_json FROM seelex_session_state WHERE project_id=` + repository.arg(1) + ` AND session_id=` + repository.arg(2)
	var state string
	if err := repository.db.QueryRowContext(ctx, query, key.ProjectID, key.SessionID).Scan(&state); err != nil {
		return nil, err
	}
	return []byte(state), nil
}

func (repository *sqlRepository) WriteProjectRecord(ctx context.Context, projectID string, record ProjectRecord) error {
	if err := validateProjectID(projectID); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("session storage: marshal project record: %w", err)
	}
	query := `INSERT INTO seelex_project_record (project_id,record_json,updated_at) VALUES (` + repository.placeholders(3) + `)
ON CONFLICT (project_id) DO UPDATE SET record_json=excluded.record_json, updated_at=excluded.updated_at`
	_, err = repository.db.ExecContext(ctx, query, projectID, string(data), time.Now().UTC().UnixNano())
	return err
}

func (repository *sqlRepository) ReadProjectRecord(ctx context.Context, projectID string) (ProjectRecord, error) {
	if err := validateProjectID(projectID); err != nil {
		return ProjectRecord{}, err
	}
	query := `SELECT record_json FROM seelex_project_record WHERE project_id=` + repository.arg(1)
	var data string
	if err := repository.db.QueryRowContext(ctx, query, projectID).Scan(&data); err != nil {
		return ProjectRecord{}, err
	}
	var record ProjectRecord
	if err := json.Unmarshal([]byte(data), &record); err != nil {
		return ProjectRecord{}, err
	}
	return record, nil
}

func (repository *sqlRepository) List(ctx context.Context, projectID string) ([]frameworkStorage.SessionMeta, error) {
	query := `SELECT session_id,token_count,shard_count,created_at,updated_at FROM seelex_session_manifest WHERE project_id=` + repository.arg(1) + ` ORDER BY updated_at DESC`
	rows, err := repository.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []frameworkStorage.SessionMeta{}
	for rows.Next() {
		var meta frameworkStorage.SessionMeta
		var createdAt, updatedAt int64
		if err := rows.Scan(&meta.SessionID, &meta.TokenCount, &meta.ShardCount, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		meta.CreatedAt, meta.UpdatedAt = time.Unix(0, createdAt).UTC(), time.Unix(0, updatedAt).UTC()
		result = append(result, meta)
	}
	return result, rows.Err()
}

func (repository *sqlRepository) Delete(ctx context.Context, key Key) error {
	if err := key.validate(); err != nil {
		return err
	}
	transaction, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	for _, table := range []string{"seelex_tool_result", "seelex_session_state", "seelex_session_event_shard", "seelex_session_shard", "seelex_session_manifest", "seelex_sessions", "seelex_framework_event"} {
		query := `DELETE FROM ` + table + ` WHERE project_id=` + repository.arg(1) + ` AND session_id=` + repository.arg(2)
		if _, err := transaction.ExecContext(ctx, query, key.ProjectID, key.SessionID); err != nil {
			return err
		}
	}
	return transaction.Commit()
}

type sqlManifest struct {
	generation string
	shardCount int
}

func (repository *sqlRepository) readManifest(ctx context.Context, key Key) (sqlManifest, error) {
	query := `SELECT generation,shard_count FROM seelex_session_manifest WHERE project_id=` + repository.arg(1) + ` AND session_id=` + repository.arg(2)
	var manifest sqlManifest
	err := repository.db.QueryRowContext(ctx, query, key.ProjectID, key.SessionID).Scan(&manifest.generation, &manifest.shardCount)
	return manifest, err
}

func (repository *sqlRepository) readGeneration(ctx context.Context, key Key, generation string, shardCount int) ([]types.Message, error) {
	query := `SELECT shard_index,messages_json FROM seelex_session_shard WHERE project_id=` + repository.arg(1) + ` AND session_id=` + repository.arg(2) + ` AND generation=` + repository.arg(3) + ` ORDER BY shard_index ASC`
	rows, err := repository.db.QueryContext(ctx, query, key.ProjectID, key.SessionID, generation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := make([]types.Message, 0, shardCount*100)
	count := 0
	for rows.Next() {
		var index int
		var data string
		if err := rows.Scan(&index, &data); err != nil {
			return nil, err
		}
		if index != count {
			return nil, fmt.Errorf("session storage: missing shard %d", count)
		}
		var shard []types.Message
		if err := json.Unmarshal([]byte(data), &shard); err != nil {
			return nil, err
		}
		messages = append(messages, shard...)
		count++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if count != shardCount {
		return nil, fmt.Errorf("session storage: shard count = %d, want %d", count, shardCount)
	}
	return messages, nil
}

func (repository *sqlRepository) readLegacy(ctx context.Context, key Key) ([]types.Message, error) {
	query := `SELECT messages_json FROM seelex_sessions WHERE project_id=` + repository.arg(1) + ` AND session_id=` + repository.arg(2)
	var data string
	if err := repository.db.QueryRowContext(ctx, query, key.ProjectID, key.SessionID).Scan(&data); err != nil {
		return nil, err
	}
	var messages []types.Message
	if err := json.Unmarshal([]byte(data), &messages); err != nil {
		return nil, err
	}
	return messages, nil
}
func (repository *sqlRepository) Close() error { return repository.db.Close() }
func (repository *sqlRepository) arg(index int) string {
	if repository.placeholder == "$" {
		return fmt.Sprintf("$%d", index)
	}
	return "?"
}
func (repository *sqlRepository) placeholders(count int) string {
	values := make([]string, count)
	for i := range values {
		values[i] = repository.arg(i + 1)
	}
	return strings.Join(values, ",")
}

func split(messages []types.Message, shardSize int) [][]types.Message {
	if len(messages) == 0 {
		return [][]types.Message{{}}
	}
	result := make([][]types.Message, 0, (len(messages)+shardSize-1)/shardSize)
	for start := 0; start < len(messages); start += shardSize {
		end := start + shardSize
		if end > len(messages) {
			end = len(messages)
		}
		result = append(result, messages[start:end])
	}
	return result
}

// shardCounts 返回每 shard 的消息条数（manifest 窗口读索引）。
func shardCounts(shards [][]types.Message) []int {
	counts := make([]int, len(shards))
	for index, shard := range shards {
		counts[index] = len(shard)
	}
	return counts
}

func splitEvents(events []Event, shardSize int) [][]Event {
	if len(events) == 0 {
		return nil
	}
	result := make([][]Event, 0, (len(events)+shardSize-1)/shardSize)
	for start := 0; start < len(events); start += shardSize {
		end := start + shardSize
		if end > len(events) {
			end = len(events)
		}
		result = append(result, events[start:end])
	}
	return result
}

func eventTokenCount(events []Event) int {
	total := 0
	for _, event := range events {
		total += event.TokenCount
	}
	return total
}

func mergeEvents(existing, incoming []Event) []Event {
	if len(existing) == 0 {
		return append([]Event(nil), incoming...)
	}
	merged := append([]Event(nil), existing...)
	positions := make(map[uint64]int, len(merged))
	for index, event := range merged {
		if event.Seq != 0 {
			positions[event.Seq] = index
		}
	}
	for _, event := range incoming {
		if index, ok := positions[event.Seq]; ok && event.Seq != 0 {
			merged[index] = event
			continue
		}
		merged = append(merged, event)
		if event.Seq != 0 {
			positions[event.Seq] = len(merged) - 1
		}
	}
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].Seq == 0 && merged[j].Seq == 0 {
			return false
		}
		if merged[i].Seq == 0 {
			return true
		}
		if merged[j].Seq == 0 {
			return false
		}
		return merged[i].Seq < merged[j].Seq
	})
	return merged
}

func selectEventTail(events []Event, tokenBudget, maxUnits int) []Event {
	if tokenBudget <= 0 || maxUnits <= 0 {
		return []Event{}
	}
	units := completeEventUnits(events)
	selected := make([][]Event, 0, maxUnits)
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

func completeEventUnits(events []Event) [][]Event {
	units := make([][]Event, 0, len(events))
	for index := 0; index < len(events); {
		event := events[index]
		switch {
		case event.Role == "user":
			unit, next, complete := userEventUnit(events, index)
			if complete {
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
				index = next
			} else {
				index = nextUserEventIndex(events, next)
			}
		default:
			// Orphan tool results and unknown roles are archive evidence but
			// never become provider context by themselves.
			index++
		}
	}
	return units
}

func userEventUnit(events []Event, start int) ([]Event, int, bool) {
	unit := []Event{events[start]}
	index := start + 1
	hasAssistant := false
	for index < len(events) && events[index].Role != "user" {
		event := events[index]
		if event.Role != "assistant" {
			return nil, nextUserEventIndex(events, index+1), false
		}
		hasAssistant = true
		if len(event.ToolCalls) == 0 {
			unit = append(unit, event)
			return unit, index + 1, true
		}
		toolUnit, next, complete := toolEventUnit(events, index)
		if !complete {
			return nil, nextUserEventIndex(events, next), false
		}
		unit = append(unit, toolUnit...)
		index = next
	}
	return unit, index, hasAssistant
}

func nextUserEventIndex(events []Event, start int) int {
	for start < len(events) && events[start].Role != "user" {
		start++
	}
	return start
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

func tokenCount(messages []types.Message) int {
	total := 0
	for _, message := range messages {
		if message.Content != nil {
			total += (len(*message.Content) + 3) / 4
		}
	}
	return total
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
func writeAtomic(path string, data []byte, permission os.FileMode) error {
	temp := path + "." + randomID() + ".tmp"
	if err := os.WriteFile(temp, data, permission); err != nil {
		return err
	}
	return os.Rename(temp, path)
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

// sumShardCounts 对 manifest 分片计数求和；无计数（旧 manifest）返回 -1。
func sumShardCounts(counts []int) int {
	if len(counts) == 0 {
		return -1
	}
	total := 0
	for _, count := range counts {
		total += count
	}
	return total
}
