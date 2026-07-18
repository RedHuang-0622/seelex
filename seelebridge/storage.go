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

// SessionStore adapts Seele storage without exposing it to Seelex business packages.
type SessionStore struct {
	store   *storage.Store
	baseDir string
}

func NewSessionStore(path string) (*SessionStore, error) {
	store, err := storage.NewStore(path)
	if err != nil {
		return nil, err
	}
	return &SessionStore{store: store, baseDir: path}, nil
}

func (s *SessionStore) FrameworkStore() *storage.Store { return s.store }

func (s *SessionStore) Save(sessionID string, messages []Message) error {
	return s.store.Save(sessionID, messages)
}

func (s *SessionStore) Load(sessionID string) ([]Message, error) {
	return s.store.Load(sessionID)
}

func (s *SessionStore) List() []SessionMeta { return s.store.List() }

func (s *SessionStore) Delete(sessionID string) error { return s.store.Delete(sessionID) }

// LoadRange 直接读取 Seele Store 写出的 shard 文件，只加载覆盖 [offset, offset+limit) 的分片。
// offset 是 0-based 绝对位置。返回范围内的消息和总消息数。
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
	cumulative := 0 // 当前 shard 的起始绝对位置

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

// ── 内部：复制 Seele Store 的分片文件结构读取 ──────────────────────

// shardDir 计算 sessionID 对应的分片目录（与 Seele storage.Store 的 SHA-256 hash prefix 一致）。
func (s *SessionStore) shardDir(sessionID string) string {
	// 与 Seele 的 sessionDir 逻辑一致
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

// sha256Sum 计算字符串的 SHA-256 哈希（与 Seele storage.Store 的 sessionDir 前缀逻辑一致）。
func sha256Sum(s string) [32]byte { return sha256.Sum256([]byte(s)) }
