package goal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Store 是 goal 栈的持久化接口：每次变更后 Controller 全量保存当前栈；
// 弹栈（finish/abort）即从栈文件消失。审计历史（History）为进程内态。
// 设计落位：未来由 sessionstore state blob 的第五栈实现（design §3.7）。
type Store interface {
	// Load 读取当前会话 goal 栈（空栈返回空切片，nil 等价空）。
	Load(ctx context.Context) ([]*GoalRecord, error)
	// Save 全量保存当前会话 goal 栈。
	Save(ctx context.Context, records []*GoalRecord) error
}

// MemoryStore 是进程内 Store（测试/无持久化运行）。
type MemoryStore struct {
	mu      sync.Mutex
	records []*GoalRecord
}

// NewMemoryStore 构造空内存存储。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

// Load 实现 Store。
func (m *MemoryStore) Load(_ context.Context) ([]*GoalRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*GoalRecord, 0, len(m.records))
	for _, record := range m.records {
		out = append(out, record.Clone())
	}
	return out, nil
}

// Save 实现 Store（原子替换快照）。
func (m *MemoryStore) Save(_ context.Context, records []*GoalRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = nil
	for _, record := range records {
		m.records = append(m.records, record.Clone())
	}
	return nil
}

// JSONFileStore 是单文件原子持久化 Store（原型；与 sessionstore JSON backend
// 的 generation 原子切换理念一致：temp + rename）。
type JSONFileStore struct {
	Path string
	mu   sync.Mutex
}

// NewJSONFileStore 构造文件存储（父目录需存在；测试用 t.TempDir()）。
func NewJSONFileStore(path string) *JSONFileStore {
	return &JSONFileStore{Path: path}
}

type jsonFilePayload struct {
	Records []*GoalRecord `json:"records"`
}

// Load 实现 Store；文件不存在视为空栈。
func (s *JSONFileStore) Load(_ context.Context) ([]*GoalRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: 读 %s: %v", ErrStoreUnavailable, s.Path, err)
	}
	var payload jsonFilePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("%w: 解析 %s: %v", ErrStoreUnavailable, s.Path, err)
	}
	return payload.Records, nil
}

// Save 实现 Store（原子写：同目录 temp + rename）。
func (s *JSONFileStore) Save(_ context.Context, records []*GoalRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload := jsonFilePayload{Records: records}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: 编码: %v", ErrStoreUnavailable, err)
	}
	temp := filepath.Join(filepath.Dir(s.Path), fmt.Sprintf(".goal-%d.tmp", os.Getpid()))
	if err := os.WriteFile(temp, raw, 0o600); err != nil {
		return fmt.Errorf("%w: 写 %s: %v", ErrStoreUnavailable, temp, err)
	}
	if err := os.Rename(temp, s.Path); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("%w: 原子替换 %s: %v", ErrStoreUnavailable, s.Path, err)
	}
	return nil
}
