package sessionstore

import (
	"encoding/json"
	"errors"
	"fmt"

	workplanTypes "github.com/RedHuang-0622/Seele/workplan/core/types"
)

// ErrCheckpointNotFound 表示 checkpoint 不存在。恢复方按“无续跑点”处理，
// 而不是把损坏的会话当作空历史。
var ErrCheckpointNotFound = errors.New("session storage: checkpoint not found")

// CheckpointStore 把 workplan runner 的 checkpoint.Store 契约映射到
// sessionstore 的会话状态通道（SaveState/LoadState，opaque blob）。
//
// 存储键是 `checkpoint-<hash(id)>`，位于 (projectID, 键) 分片下；WriteState
// 不创建会话 manifest，因此 checkpoint 不会出现在会话目录（Router.List）里，
// 与正常会话历史物理隔离。
type CheckpointStore struct {
	router    *Router
	projectID string
}

// NewCheckpointStore 创建绑定到指定项目作用域的 checkpoint 存储。
// projectID 为空时使用 Router 当前的 active workspace。
func NewCheckpointStore(router *Router, projectID string) *CheckpointStore {
	return &CheckpointStore{router: router, projectID: projectID}
}

// Save 序列化并持久化 workplan 快照（幂等：同 id 覆盖为最新快照）。
func (store *CheckpointStore) Save(id string, snap *workplanTypes.Snapshot) error {
	if store == nil || store.router == nil {
		return errors.New("session storage: checkpoint router is unavailable")
	}
	if snap == nil {
		return errors.New("session storage: checkpoint snapshot is nil")
	}
	payload, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("session storage: encode checkpoint %q: %w", id, err)
	}
	key := checkpointStorageKey(id)
	if store.projectID == "" {
		if err := store.router.SaveCheckpointWorkspace("", key, payload); err != nil {
			return fmt.Errorf("session storage: save checkpoint %q: %w", id, err)
		}
		return nil
	}
	if err := store.router.SaveCheckpointWorkspace(store.projectID, key, payload); err != nil {
		return fmt.Errorf("session storage: save checkpoint %q: %w", id, err)
	}
	return nil
}

// Load 读取 workplan 快照；不存在时返回 ErrCheckpointNotFound。
func (store *CheckpointStore) Load(id string) (*workplanTypes.Snapshot, error) {
	if store == nil || store.router == nil {
		return nil, errors.New("session storage: checkpoint router is unavailable")
	}
	key := checkpointStorageKey(id)
	var payload []byte
	var err error
	if store.projectID == "" {
		payload, err = store.router.LoadCheckpointWorkspace("", key)
	} else {
		payload, err = store.router.LoadCheckpointWorkspace(store.projectID, key)
	}
	if err != nil {
		if isSessionNotFound(err) {
			return nil, fmt.Errorf("%w: %q", ErrCheckpointNotFound, id)
		}
		return nil, fmt.Errorf("session storage: load checkpoint %q: %w", id, err)
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("%w: %q", ErrCheckpointNotFound, id)
	}
	var snap workplanTypes.Snapshot
	if err := json.Unmarshal(payload, &snap); err != nil {
		return nil, fmt.Errorf("session storage: decode checkpoint %q: %w", id, err)
	}
	return &snap, nil
}

// checkpointStorageKey 把调用方 checkpoint ID 收敛为安全的会话状态键。
// 相同 ID 恒映射到相同键；空 ID 保留字面量（调用方契约禁止空 ID）。
func checkpointStorageKey(id string) string {
	if id == "" {
		return "checkpoint"
	}
	return "checkpoint-" + hash(id)
}
