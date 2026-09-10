package sessionstore

import (
	"encoding/json"
	"sync"
)

// SessionDisplayMeta 是会话展示元数据（置顶、别名、手动排序位）。它属于"用户给会话
// 起的名字与排序"，不是执行事实，因此不进 SessionRecord（Record 会被回合结束
// 的整体重建覆盖），而是落在项目级 blob 里。
type SessionDisplayMeta struct {
	Pinned    bool   `json:"pinned,omitempty"`
	Alias     string `json:"alias,omitempty"`
	SortOrder int    `json:"sort_order,omitempty"`
}

// Empty 报告元数据是否已无信息量（调用方据此删除条目而不是写零值）。
func (meta SessionDisplayMeta) Empty() bool {
	return !meta.Pinned && meta.Alias == "" && meta.SortOrder == 0
}

// sessionMetaKey 是项目级元数据 blob 借用的伪会话键。JSON/SQLite 后端按
// (project, session) 键存 state 通道，且目录枚举只认有 manifest 的会话目录，
// 因此这个键不会作为会话出现在目录里（不造幽灵会话）。
const sessionMetaKey = "seelex:project:session-meta"

// SessionMetaStore 读写项目级会话展示元数据 blob。
//
// 一个项目一份 blob：读一次即拿到该项目所有会话的元数据（目录富化只需一次读），
// 写是读-改-写，因此进程内用 mu 串行化（桌面单进程即足够，跨进程写竞争不在
// 当前形态范围内）。
type SessionMetaStore struct {
	router *Router
	mu     sync.Mutex
}

// NewSessionMetaStore 构造项目级元数据存储；router 为 nil 时读写退化为空操作。
func NewSessionMetaStore(router *Router) *SessionMetaStore {
	return &SessionMetaStore{router: router}
}

// Meta 返回指定项目下 sessionID → 元数据 的映射（无记录时返回空映射）。
func (store *SessionMetaStore) Meta(projectID string) (map[string]SessionDisplayMeta, error) {
	result := map[string]SessionDisplayMeta{}
	if store == nil || store.router == nil {
		return result, nil
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.load(projectID)
}

// Set 写入/清除单个会话的展示元数据（meta 为空语义即删除条目），返回更新后的
// 整个项目映射。
func (store *SessionMetaStore) Set(projectID, sessionID string, meta SessionDisplayMeta) (map[string]SessionDisplayMeta, error) {
	if store == nil || store.router == nil {
		return map[string]SessionDisplayMeta{}, nil
	}
	store.mu.Lock()
	defer store.mu.Unlock()

	current, err := store.load(projectID)
	if err != nil {
		return nil, err
	}
	if meta.Empty() || sessionID == "" {
		delete(current, sessionID)
	} else {
		current[sessionID] = meta
	}
	payload, err := json.Marshal(current)
	if err != nil {
		return nil, err
	}
	if err := store.router.SaveSessionDisplayMetaWorkspace(projectID, payload); err != nil {
		return nil, err
	}
	return current, nil
}

// load 读取 blob；调用方持有 mu。缺键/反序列化失败都按"无元数据"处理：展示
// 元数据缺失只影响排序与标题，不得让目录整体失败。
func (store *SessionMetaStore) load(projectID string) (map[string]SessionDisplayMeta, error) {
	result := map[string]SessionDisplayMeta{}
	payload, err := store.router.LoadSessionDisplayMetaWorkspace(projectID)
	if err != nil || len(payload) == 0 {
		return result, nil
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return map[string]SessionDisplayMeta{}, nil
	}
	if result == nil {
		result = map[string]SessionDisplayMeta{}
	}
	return result, nil
}
