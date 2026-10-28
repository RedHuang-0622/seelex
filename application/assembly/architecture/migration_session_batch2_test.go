package architecture

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionMigrationBatch2FilesAreGone(t *testing.T) {
	files := []string{
		"session_catalog_grid_test.go",
		"session_catalog_test.go",
		"session_cold_read_test.go",
		"session_concurrent_content_isolation_test.go",
		"session_conversation_history_query_test.go",
		"session_decoupling_test.go",
		"session_directory_visibility_test.go",
		"session_draft_context_attach_test.go",
		"session_draft_context_scope_test.go",
		"session_fork_store_regression_test.go",
		"session_g5_lock_test.go",
		"session_history_browsing_submit_repro_test.go",
		"session_history_coldload_latest_repro_test.go",
		"session_history_hot_tail_test.go",
		"session_pollution_s0_test.go",
		"session_project_root_test.go",
		"session_resource_isolation_test.go",
		"session_s0_events_test.go",
		"session_status_test.go",
		"session_stress_test.go",
	}
	for _, name := range files {
		path := filepath.Join("..", name)
		if _, err := os.Stat(path); err == nil {
			t.Errorf("旧位置仍存在：application/assembly/%s", name)
			continue
		} else if !os.IsNotExist(err) {
			t.Errorf("检查 %s: %v", path, err)
		}
	}
}
