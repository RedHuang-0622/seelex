package prompt

import (
	"testing"

	"github.com/RedHuang-0622/seelex/backend/limit/effort"
	"github.com/RedHuang-0622/seelex/backend/limit/loop"
)

// TestEffortEntryPointsForwardToPolicy 是跨实现一致性用例：prompt 侧的三个既有
// 入口（MaxLoops / ReActBudgetFor / ReasoningEffortFor）必须与判据层同解，
// 防止宿主管理器将来又抄一份数字或词表。
func TestEffortEntryPointsForwardToPolicy(t *testing.T) {
	for _, level := range loop.Levels() {
		if got, want := MaxLoops(level), loop.MaxLoops(level); got != want {
			t.Errorf("MaxLoops(%q) = %d, want %d", level, got, want)
		}
		if got, want := ReActBudgetFor(level), loop.BudgetFor(level); got != want {
			t.Errorf("ReActBudgetFor(%q) = %+v, want %+v", level, got, want)
		}
		if got, want := ReasoningEffortFor(level), effort.ReasoningEffortFor(level); got != want {
			t.Errorf("ReasoningEffortFor(%q) = %q, want %q", level, got, want)
		}
	}
}
