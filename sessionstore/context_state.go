package sessionstore

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/redis/go-redis/v9"
)

// contextStateFile 是 JSON 后端 context 模块的独立文件（session 根目录，
// 不随 generation rollover 失效；与 state.json 物理隔离）。
const contextStateFile = "context.json"

func (repository *jsonRepository) WriteContextState(_ context.Context, key Key, state []byte) error {
	if err := key.validate(); err != nil {
		return err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	directory := repository.sessionDir(key)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	return writeAtomic(filepath.Join(directory, contextStateFile), state, 0o600)
}

func (repository *jsonRepository) ReadContextState(_ context.Context, key Key) ([]byte, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	return os.ReadFile(filepath.Join(repository.sessionDir(key), contextStateFile))
}

func (repository *sqlRepository) WriteContextState(ctx context.Context, key Key, state []byte) error {
	if err := key.validate(); err != nil {
		return err
	}
	query := `INSERT INTO seelex_session_context (project_id,session_id,context_json,updated_at) VALUES (` + repository.placeholders(4) + `)
ON CONFLICT (project_id,session_id) DO UPDATE SET context_json=excluded.context_json, updated_at=excluded.updated_at`
	_, err := repository.db.ExecContext(ctx, query, key.ProjectID, key.SessionID, string(state), time.Now().UTC().UnixNano())
	return err
}

func (repository *sqlRepository) ReadContextState(ctx context.Context, key Key) ([]byte, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	var payload string
	query := `SELECT context_json FROM seelex_session_context WHERE project_id=` + repository.arg(1) + ` AND session_id=` + repository.arg(2)
	if err := repository.db.QueryRowContext(ctx, query, key.ProjectID, key.SessionID).Scan(&payload); err != nil {
		return nil, err
	}
	return []byte(payload), nil
}

func (repository *redisRepository) WriteContextState(ctx context.Context, key Key, state []byte) error {
	if err := key.validate(); err != nil {
		return err
	}
	return repository.client.Set(ctx, repository.contextKey(key), state, 0).Err()
}

func (repository *redisRepository) ReadContextState(ctx context.Context, key Key) ([]byte, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	value, err := repository.client.Get(ctx, repository.contextKey(key)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, fs.ErrNotExist
	}
	return value, err
}

// contextKey 返回 Redis 中会话 context 模块的独立 key。
func (repository *redisRepository) contextKey(key Key) string {
	return repository.sessionKey(key) + ":context"
}
