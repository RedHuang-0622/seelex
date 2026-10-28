package architecture

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootMigrationBatchFilesAreGone(t *testing.T) {
	files := []string{
		"agentteam_fixture_test.go",
		"chat_hot_attach_reasoning_repro_test.go",
		"close_semantics_test.go",
		"command_registry_test.go",
		"environment_context_test.go",
		"fault_guard_test.go",
		"fixture_concurrency_test.go",
		"goal_team_wiring_test.go",
		"history_safety_test.go",
		"hot_attach_running_test.go",
		"interrupted_continue_test.go",
		"review_files_test.go",
		"service_effort_wire_test.go",
		"shutdown_concurrent_test.go",
		"snapshot_budget_judgement_test.go",
		"snapshot_budget_test.go",
		"visible_output_test.go",
		"workspace_file_usecase_test.go",
		"workspace_gitrepos_usecase_test.go",
		"workspace_tree_usecase_test.go",
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
