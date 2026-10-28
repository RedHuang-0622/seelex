package architecture

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContextMigrationBatchFilesAreGone(t *testing.T) {
	files := []string{
		"context_budget_frequency_test.go",
		"context_budget_margin_idempotency_test.go",
		"context_compact_across_rounds_repro_test.go",
		"context_compact_gate_test.go",
		"context_compact_index_test.go",
		"context_compact_no_summary_test.go",
		"context_compact_selfdeadlock_repro_test.go",
		"context_compact_viewmu_hold_repro_test.go",
		"context_compaction_body_missing_repro_test.go",
		"context_compaction_facts_restore_repro_test.go",
		"context_compaction_restart_refill_repro_test.go",
		"context_compactions_restore_repro_test.go",
		"context_controller_test.go",
		"context_narration_attribution_test.go",
		"context_prefix_invariant_test.go",
		"context_probe_harness_test.go",
		"context_restore_prefix_order_test.go",
		"context_retain_floor_test.go",
		"context_strategy_ab_probe_test.go",
		"context_window_rule_test.go",
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
