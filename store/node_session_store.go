package store

import (
	"errors"

	"github.com/RedHuang-0622/seelex/store/workunit"
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
// 由调用方（backend/multiagent/subagent）写入 History / ContextSnapshot / 阶段日志 /
// 语义结果 / worktree 现场。
//
// M9 之后落盘实现归 `store/workunit`（`Record` 形状、原子写、缺失即 `ErrNotFound`）；
// 本文件只剩**单层转发**：把项目作用域解析成主会话存储目录，再交给 workunit 的 owner。

// NodeSessionSchemaVersion 是 NodeSessionRecord 的当前 schema 版本。
const NodeSessionSchemaVersion = workunit.SchemaVersion

// 记录形状的同身份别名：wire（JSON tag / schema 版本 / 文件名）归 `store/workunit`，
// 根包不再持有第二份形状定义。
type (
	// NodeWorktreeRecord 是节点 worktree 现场的持久化摘要（恢复数据面）。
	NodeWorktreeRecord = workunit.WorktreeRecord
	// NodeUnitRecord 是记录快照里的**身份**那一格（哪一类执行单元 + 该类的配置摘要）。
	NodeUnitRecord = workunit.UnitRecord
	// NodeSessionRecord 是单个子代理会话的持久化记录。
	NodeSessionRecord = workunit.Record
)

// ErrNodeSessionNotFound 表示子代理会话记录不存在。
var ErrNodeSessionNotFound = workunit.ErrNotFound

// 编译期钉住：根存储满足写链的写面与完整面端口（结构满足 + 断言 > 适配层）。
var (
	_ workunit.Saver    = (*NodeSessionStore)(nil)
	_ workunit.Recorder = (*NodeSessionStore)(nil)
)

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
	if record.MainSessionID == "" {
		record.MainSessionID = mainSessionID
	}
	return store.router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		repo, ok := repository.(*jsonRepository)
		if !ok {
			return errors.New("session storage: node session records require the JSON backend")
		}
		return workunit.New(repo.sessionDir(Key{ProjectID: projectID, SessionID: mainSessionID})).Write(record)
	})
}

// Load 读取一条子代理会话记录；不存在返回 ErrNodeSessionNotFound。
func (store *NodeSessionStore) Load(projectID, mainSessionID, subSessionID string) (NodeSessionRecord, error) {
	if store == nil || store.router == nil {
		return NodeSessionRecord{}, errors.New("session storage: node session router is unavailable")
	}
	var record NodeSessionRecord
	err := store.router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		repo, ok := repository.(*jsonRepository)
		if !ok {
			return errors.New("session storage: node session records require the JSON backend")
		}
		key := Key{ProjectID: projectID, SessionID: mainSessionID}
		if err := key.Validate(); err != nil {
			return err
		}
		var err error
		record, err = workunit.New(repo.sessionDir(key)).Read(mainSessionID, subSessionID)
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
		repo, ok := repository.(*jsonRepository)
		if !ok {
			return errors.New("session storage: node session records require the JSON backend")
		}
		key := Key{ProjectID: projectID, SessionID: mainSessionID}
		if err := key.Validate(); err != nil {
			return err
		}
		var err error
		records, err = workunit.New(repo.sessionDir(key)).List()
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
		repo, ok := repository.(*jsonRepository)
		if !ok {
			return errors.New("session storage: node session records require the JSON backend")
		}
		key := Key{ProjectID: projectID, SessionID: mainSessionID}
		if err := key.Validate(); err != nil {
			return err
		}
		return workunit.New(repo.sessionDir(key)).Delete(mainSessionID, subSessionID)
	})
}
