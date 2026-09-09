package sessionstore

import (
	"path/filepath"
	"testing"
)

// newLegacyJSONRepository 构造强制旧布局的 JSON repository（旧布局专属回归
// 测试用：新会话仍写 manifest/generation/transcript/rollout）。
func newLegacyJSONRepository(root string, shardSize int) (*jsonRepository, error) {
	return newJSONRepositoryWithV8(root, shardSize, true)
}

// newLegacyTestRouter 构造默认 JSON + 强制旧布局的 Router。
func newLegacyTestRouter(t *testing.T) *Router {
	t.Helper()
	root := t.TempDir()
	router, err := NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := newLegacyJSONRepository(filepath.Join(root, "sessions-json"), 0)
	if err != nil {
		t.Fatal(err)
	}
	router.mu.Lock()
	router.repository = repository
	router.config = Config{Backend: BackendJSON, Path: filepath.Join(root, "sessions-json")}
	router.mu.Unlock()
	return router
}
