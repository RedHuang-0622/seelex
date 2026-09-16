package sessionstore

import (
	"encoding/json"
	"sync"
)

// SessionDisplayMeta 是项目级会话元数据（置顶、别名、手动排序位），外加**会话级
// 用户设置**（当前唯一一项：主会话权限档位 PermissionTier）。它们属于"用户怎么看
// 这个会话 / 用户给这个会话选了什么"，不是执行事实，因此不进 SessionRecord
// （Record 会被回合结束的整体重建覆盖），而是落在项目级 blob 里。
//
// 展示字段与会话级设置共用一份 blob（一次读拿到一个项目的全部条目），但**写入面
// 分开**：Set 只替换展示字段、SetPermissionTier 只写档位，互不覆盖——否则"取消
// 置顶"会顺手抹掉用户的权限档位。
type SessionDisplayMeta struct {
	Pinned    bool   `json:"pinned,omitempty"`
	Alias     string `json:"alias,omitempty"`
	SortOrder int    `json:"sort_order,omitempty"`
	// PermissionTier 是主会话在本会话的权限档位（dto.PermissionTier* 的 id；
	// 空 = 该会话从未选择 → 运行时回退进程默认档位）。它是用户选择，跨重启必须
	// 记住（2026-09-17 需求变更：档位从内存槽改为会话级设置）。
	PermissionTier string `json:"permission_tier,omitempty"`
}

// Empty 报告元数据是否已无信息量（调用方据此删除条目而不是写零值）。
func (meta SessionDisplayMeta) Empty() bool {
	return !meta.Pinned && meta.Alias == "" && meta.SortOrder == 0 && meta.PermissionTier == ""
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

// Set 写入/清除单个会话的**展示**元数据（置顶/别名/排序位），返回更新后的整个项目
// 映射。meta 的三项展示字段为零值即清除这三项；条目是否整条删除取决于合并后的结果
// （会话级设置字段由 SetPermissionTier 单独维护，本方法不触碰——否则"取消置顶"会
// 顺手把用户的权限档位抹掉）。
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
	merged := current[sessionID]
	merged.Pinned, merged.Alias, merged.SortOrder = meta.Pinned, meta.Alias, meta.SortOrder
	if merged.Empty() || sessionID == "" {
		delete(current, sessionID)
	} else {
		current[sessionID] = merged
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

// SetPermissionTier 写入单个会话的**权限档位**（会话级用户设置；不影响置顶/别名/
// 排序位），返回写入后的该会话条目。tier 为空即清除档位选择。
func (store *SessionMetaStore) SetPermissionTier(projectID, sessionID, tier string) (SessionDisplayMeta, error) {
	if store == nil || store.router == nil || sessionID == "" {
		return SessionDisplayMeta{}, nil
	}
	store.mu.Lock()
	defer store.mu.Unlock()

	current, err := store.load(projectID)
	if err != nil {
		return SessionDisplayMeta{}, err
	}
	entry := current[sessionID]
	entry.PermissionTier = tier
	if entry.Empty() {
		delete(current, sessionID)
		entry = SessionDisplayMeta{}
	} else {
		current[sessionID] = entry
	}
	payload, err := json.Marshal(current)
	if err != nil {
		return SessionDisplayMeta{}, err
	}
	if err := store.router.SaveSessionDisplayMetaWorkspace(projectID, payload); err != nil {
		return SessionDisplayMeta{}, err
	}
	return entry, nil
}

// PermissionTier 读取单个会话的权限档位设置（空 = 从未选择，运行时回退进程默认）。
func (store *SessionMetaStore) PermissionTier(projectID, sessionID string) (string, error) {
	metas, err := store.Meta(projectID)
	if err != nil {
		return "", err
	}
	return metas[sessionID].PermissionTier, nil
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
