package architecture

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServiceSessionMigrationBatchFilesAreGone(t *testing.T) {
	files := []string{
		"archive_reasoning_persist_test.go",
		"async_completion_trigger_test.go",
		"chat_delegation_test.go",
		"chat_state_event_test.go",
		"compressed_turn_test.go",
		"content_lru_test.go",
		"durable_queue_wire_test.go",
		"resident_lru_test.go",
		"service_input_fork_queue_test.go",
		"service_knobs_smoke_test.go",
		"service_stop_flush_test.go",
		"session_binding_regression_test.go",
		"session_input_index_test.go",
		"session_live_content_probe_test.go",
		"session_runtime_slot_integration_test.go",
		"session_submit_restoring_test.go",
		"session_switch_failed_resume_test.go",
		"subagent_evidence_test.go",
		"teamwork_board_session_switch_test.go",
		"teamwork_completion_trigger_test.go",
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
