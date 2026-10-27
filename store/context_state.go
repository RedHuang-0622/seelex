package store

import (
	"context"
)

// contextStateFile 是 JSON 后端 context 模块的独立文件（session 根目录，
// 不随 generation rollover 失效；与 state.json 物理隔离）。
const contextStateFile = "context.json"

func (repository *jsonRepository) WriteContextState(_ context.Context, key Key, state []byte) error {
	if err := key.Validate(); err != nil {
		return err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.layout.contexts.WriteState(layoutScope(key), state)
}

func (repository *jsonRepository) ReadContextState(_ context.Context, key Key) ([]byte, error) {
	if err := key.Validate(); err != nil {
		return nil, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	return repository.layout.contexts.ReadState(layoutScope(key))
}
