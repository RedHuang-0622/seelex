package observability

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestPerfStatsHoldsReadLockOnlyAroundInputs(t *testing.T) {
	locked := 0
	application := NewPerfApplication(PerfDeps{
		LockView:   func() { locked++ },
		UnlockView: func() { locked-- },
		Snapshot: func() model.Snapshot {
			if locked != 1 {
				t.Fatalf("snapshot read outside view lock: locked=%d", locked)
			}
			return model.Snapshot{}
		},
		HistoryWindow: func() int {
			if locked != 1 {
				t.Fatalf("history window read outside view lock: locked=%d", locked)
			}
			return 12
		},
		ToolResultRefs: func() []model.ToolResultRef {
			if locked != 1 {
				t.Fatalf("tool references read outside view lock: locked=%d", locked)
			}
			return nil
		},
	})

	_ = application.PerfStats()
	if locked != 0 {
		t.Fatalf("view lock leaked: locked=%d", locked)
	}
}
