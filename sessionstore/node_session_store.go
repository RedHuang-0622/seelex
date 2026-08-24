package sessionstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/types"
)

// 子代理会话记录（sub-session）的持久化约定：
//
// 每个子代理会话一条 JSON 记录，文件名 = `<mainSessionID>-<subSessionID>.json`，
// 存放在主会话存储目录的 `subagents/` 子目录下：
//
//	sessions-json/<project>/session-<mainID>/subagents/<mainID>-<subID>.json
//
// 因此从主会话索引（会话目录列举 / Router.List）即可直接定位全部子会话记录，
// 无需全局扫描。内容格式参考主会话 state blob（opaque JSON，含 schema 版本），
// 由调用方（seelebridge/session）写入 History / ContextSnapshot / 阶段日志 /
// 语义结果 / worktree 现场。

// NodeSessionSchemaVersion 是 NodeSessionRecord 的当前 schema 版本。
const NodeSessionSchemaVersion = 1

// NodeWorktreeRecord 是节点 worktree 现场的持久化摘要（恢复数据面）。
type NodeWorktreeRecord struct {
	Path       string `json:"path,omitempty"`
	Branch     string `json:"branch,omitempty"`
	MainBranch string `json:"main_branch,omitempty"`
	BaseCommit string `json:"base_commit,omitempty"`
}

// NodeSessionRecord 是单个子代理会话的持久化记录。
// History/ContextJSON/StagesJSON/ResultJSON 均为可独立解析的 JSON 载荷，
// 避免 sessionstore 依赖 seelebridge/internal/model 或 seelexctx/snapshot。
type NodeSessionRecord struct {
	SchemaVersion int                `json:"schema_version"`
	NodeID        string             `json:"node_id"`
	SessionID     string             `json:"session_id"` // 子会话 ID（node-<hash>）
	MainSessionID string             `json:"main_session_id"`
	Goal          string             `json:"goal"`
	Status        string             `json:"status"` // queued | running | done | failed
	Summary       string             `json:"summary,omitempty"`
	Error         string             `json:"error,omitempty"`
	History       []types.Message    `json:"history"`
	ContextJSON   []byte             `json:"context_json,omitempty"`
	StagesJSON    []byte             `json:"stages_json,omitempty"`
	ResultJSON    []byte             `json:"result_json,omitempty"`
	Worktree      NodeWorktreeRecord `json:"worktree,omitempty"`
	StartedAt     time.Time          `json:"started_at,omitempty"`
	EndedAt       time.Time          `json:"ended_at,omitempty"`
	UpdatedAt     time.Time          `json:"updated_at"`
}

// ErrNodeSessionNotFound 表示子代理会话记录不存在。
var ErrNodeSessionNotFound = errors.New("session storage: node session record not found")

// NodeSessionStore 读写子代理会话记录：绑定 Router，按显式项目作用域操作，
// 不改变 Router 的 active write scope。
type NodeSessionStore struct {
	router *Router
}

// NewNodeSessionStore 构造子代理会话记录存储。
func NewNodeSessionStore(router *Router) *NodeSessionStore {
	return &NodeSessionStore{router: router}
}

// ProjectID 返回 Router 当前的 active workspace（空 = 未绑定项目作用域，
// Save/Load 时按 Router 默认 scope 落盘）。
func (store *NodeSessionStore) ProjectID() string {
	if store == nil || store.router == nil {
		return ""
	}
	return store.router.Workspace()
}

// Save 写入（覆盖）一条子代理会话记录。
func (store *NodeSessionStore) Save(projectID, mainSessionID string, record NodeSessionRecord) error {
	if store == nil || store.router == nil {
		return errors.New("session storage: node session router is unavailable")
	}
	if strings.TrimSpace(mainSessionID) == "" || strings.TrimSpace(record.SessionID) == "" {
		return errors.New("session storage: node session requires main and sub session IDs")
	}
	if record.MainSessionID == "" {
		record.MainSessionID = mainSessionID
	}
	if record.SchemaVersion == 0 {
		record.SchemaVersion = NodeSessionSchemaVersion
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = time.Now().UTC()
	}
	return store.router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		repo, ok := repository.(nodeSessionRepository)
		if !ok {
			return errors.New("session storage: node session records require the JSON backend")
		}
		return repo.WriteNodeSession(context.Background(), Key{ProjectID: projectID, SessionID: mainSessionID}, record)
	})
}

// Load 读取一条子代理会话记录；不存在返回 ErrNodeSessionNotFound。
func (store *NodeSessionStore) Load(projectID, mainSessionID, subSessionID string) (NodeSessionRecord, error) {
	if store == nil || store.router == nil {
		return NodeSessionRecord{}, errors.New("session storage: node session router is unavailable")
	}
	var record NodeSessionRecord
	err := store.router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		repo, ok := repository.(nodeSessionRepository)
		if !ok {
			return errors.New("session storage: node session records require the JSON backend")
		}
		var err error
		record, err = repo.ReadNodeSession(context.Background(), Key{ProjectID: projectID, SessionID: mainSessionID}, subSessionID)
		return err
	})
	return record, err
}

// List 从主会话索引列举全部子代理会话记录（按 NodeID 排序）。
func (store *NodeSessionStore) List(projectID, mainSessionID string) ([]NodeSessionRecord, error) {
	if store == nil || store.router == nil {
		return nil, errors.New("session storage: node session router is unavailable")
	}
	var records []NodeSessionRecord
	err := store.router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		repo, ok := repository.(nodeSessionRepository)
		if !ok {
			return errors.New("session storage: node session records require the JSON backend")
		}
		var err error
		records, err = repo.ListNodeSessions(context.Background(), Key{ProjectID: projectID, SessionID: mainSessionID})
		return err
	})
	return records, err
}

// Delete 删除一条子代理会话记录。
func (store *NodeSessionStore) Delete(projectID, mainSessionID, subSessionID string) error {
	if store == nil || store.router == nil {
		return errors.New("session storage: node session router is unavailable")
	}
	return store.router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		repo, ok := repository.(nodeSessionRepository)
		if !ok {
			return errors.New("session storage: node session records require the JSON backend")
		}
		return repo.DeleteNodeSession(context.Background(), Key{ProjectID: projectID, SessionID: mainSessionID}, subSessionID)
	})
}

// nodeSessionRepository 是可选实现（当前仅 JSON backend；SQL/Redis 接入后
// 实现同一契约，文件布局按各自后端语义）。
type nodeSessionRepository interface {
	WriteNodeSession(context.Context, Key, NodeSessionRecord) error
	ReadNodeSession(context.Context, Key, string) (NodeSessionRecord, error)
	ListNodeSessions(context.Context, Key) ([]NodeSessionRecord, error)
	DeleteNodeSession(context.Context, Key, string) error
}

const nodeSessionDirName = "subagents"

func (repository *jsonRepository) WriteNodeSession(_ context.Context, key Key, record NodeSessionRecord) error {
	if err := key.validate(); err != nil {
		return err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	directory := filepath.Join(repository.sessionDir(key), nodeSessionDirName)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("session storage: create node session dir: %w", err)
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("session storage: encode node session %q: %w", record.SessionID, err)
	}
	if err := writeAtomic(filepath.Join(directory, nodeSessionFileName(record.MainSessionID, record.SessionID)), payload, 0o600); err != nil {
		return fmt.Errorf("session storage: write node session %q: %w", record.SessionID, err)
	}
	return nil
}

func (repository *jsonRepository) ReadNodeSession(_ context.Context, key Key, subSessionID string) (NodeSessionRecord, error) {
	if err := key.validate(); err != nil {
		return NodeSessionRecord{}, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	path := filepath.Join(repository.sessionDir(key), nodeSessionDirName, nodeSessionFileName(key.SessionID, subSessionID))
	payload, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NodeSessionRecord{}, fmt.Errorf("%w: %q", ErrNodeSessionNotFound, subSessionID)
		}
		return NodeSessionRecord{}, fmt.Errorf("session storage: read node session %q: %w", subSessionID, err)
	}
	var record NodeSessionRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return NodeSessionRecord{}, fmt.Errorf("session storage: decode node session %q: %w", subSessionID, err)
	}
	return record, nil
}

func (repository *jsonRepository) ListNodeSessions(_ context.Context, key Key) ([]NodeSessionRecord, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	directory := filepath.Join(repository.sessionDir(key), nodeSessionDirName)
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return []NodeSessionRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	records := make([]NodeSessionRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		payload, readErr := os.ReadFile(filepath.Join(directory, entry.Name()))
		if readErr != nil {
			continue // 单条损坏不阻断主索引列举
		}
		var record NodeSessionRecord
		if json.Unmarshal(payload, &record) != nil {
			continue
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].NodeID < records[j].NodeID })
	return records, nil
}

func (repository *jsonRepository) DeleteNodeSession(_ context.Context, key Key, subSessionID string) error {
	if err := key.validate(); err != nil {
		return err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	path := filepath.Join(repository.sessionDir(key), nodeSessionDirName, nodeSessionFileName(key.SessionID, subSessionID))
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("session storage: delete node session %q: %w", subSessionID, err)
	}
	return nil
}

// nodeSessionFileName 生成 `<mainSessionID>-<subSessionID>.json` 文件名
// （用户约定：从主会话索引直接定位子会话）。ID 中的路径分隔符被替换，
// 防止构造逃逸路径。
func nodeSessionFileName(mainSessionID, subSessionID string) string {
	sanitize := func(id string) string {
		return strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(id)
	}
	return sanitize(mainSessionID) + "-" + sanitize(subSessionID) + ".json"
}
