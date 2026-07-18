package seelebridge

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/RedHuang-0622/Seele/seelectx/storage"
	"github.com/RedHuang-0622/Seele/types"
)

type Message = types.Message
type SessionMeta = storage.SessionMeta

// SessionStore wraps a Storage implementation without exposing framework types.
type SessionStore struct {
	store   storage.Storage
	baseDir string
}

func NewSessionStore(path string) (*SessionStore, error) {
	if path == "" {
		return &SessionStore{baseDir: ""}, nil
	}
	s, err := storage.NewFileStore(path)
	if err != nil {
		return nil, err
	}
	return &SessionStore{store: s, baseDir: path}, nil
}

func (s *SessionStore) FrameworkStore() storage.Storage { return s.store }

func (s *SessionStore) Save(sessionID string, messages []Message) error {
	if s.store == nil {
		return nil
	}
	return s.store.Save(sessionID, messages)
}

func (s *SessionStore) Load(sessionID string) ([]Message, error) {
	if s.store == nil {
		return nil, fmt.Errorf("seelebridge: no storage configured")
	}
	return s.store.Load(sessionID)
}

func (s *SessionStore) List() []SessionMeta {
	if s.store == nil {
		return nil
	}
	return s.store.List()
}

func (s *SessionStore) Delete(sessionID string) error {
	if s.store == nil {
		return nil
	}
	return s.store.Delete(sessionID)
}

// LoadRange 读取 shard 文件，只加载覆盖 [offset, offset+limit) 的分片。
func (s *SessionStore) LoadRange(sessionID string, offset, limit int) ([]Message, int, error) {
	if limit <= 0 {
		return nil, 0, fmt.Errorf("seelebridge: limit must be positive")
	}
	if offset < 0 {
		offset = 0
	}

	shardFiles, err := s.listShardFiles(sessionID)
	if err != nil {
		return nil, 0, err
	}

	var result []Message
	cumulative := 0

	for _, f := range shardFiles {
		b, err := os.ReadFile(filepath.Join(s.shardDir(sessionID), f))
		if err != nil {
			return nil, 0, fmt.Errorf("seelebridge: read shard %s: %w", f, err)
		}
		var shard []Message
		if err := json.Unmarshal(b, &shard); err != nil {
			return nil, 0, fmt.Errorf("seelebridge: unmarshal shard %s: %w", f, err)
		}

		shardStart := cumulative
		shardEnd := cumulative + len(shard)

		if shardEnd <= offset {
			cumulative = shardEnd
			continue
		}
		if shardStart >= offset+limit {
			break
		}

		localStart := max(offset-shardStart, 0)
		localEnd := min(offset+limit-shardStart, len(shard))
		result = append(result, shard[localStart:localEnd]...)

		cumulative = shardEnd
	}

	total := cumulative
	return result, total, nil
}

// MessageCount 返回会话总消息数（从 shard 文件计算）。
func (s *SessionStore) MessageCount(sessionID string) (int, error) {
	_, total, err := s.LoadRange(sessionID, 0, 1)
	return total, err
}

func (s *SessionStore) shardDir(sessionID string) string {
	h := sha256Sum(sessionID)
	prefix := fmt.Sprintf("%x", h[:4])
	return filepath.Join(s.baseDir, prefix)
}

func (s *SessionStore) listShardFiles(sessionID string) ([]string, error) {
	dir := s.shardDir(sessionID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("seelebridge: session %q not found", sessionID)
		}
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if matched, _ := filepath.Match("history.*.json", e.Name()); matched {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	return files, nil
}

func sha256Sum(s string) [32]byte { return sha256.Sum256([]byte(s)) }
