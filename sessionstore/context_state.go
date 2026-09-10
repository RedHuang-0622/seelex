package sessionstore

import (
	"context"
	"os"
	"path/filepath"
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
