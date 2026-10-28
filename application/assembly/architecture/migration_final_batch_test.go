package architecture

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAssemblyRootHasNoRemainingTests(t *testing.T) {
	files := []string{
		"input_unknown_command_notice_test.go",
		"permission_command_test.go",
		"service_fakes_test.go",
		"service_helpers_test.go",
		"service_interaction_guard_test.go",
		"service_scheduler_test.go",
		"session_archive_test.go",
		"session_draft_shell_test.go",
		"session_draft_snapshot_test.go",
		"session_effort_test.go",
		"session_pending_tail_test.go",
		"session_permission_tier_persist_test.go",
		"session_running_not_rerooted_test.go",
		"session_scope_test.go",
		"session_state_display_test.go",
		"task_execution_test.go",
		"tool_hooks_truncation_test.go",
		"tool_record_isolation_test.go",
		"tool_record_test.go",
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
