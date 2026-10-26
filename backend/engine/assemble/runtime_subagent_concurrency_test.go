package assemble

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/kernel/limit"
)

// TestSubagentRouteConcurrencyNarrowsThePolicy 钉住 limits.concurrency.subagent：
// 关闭时策略原样；开启时只做更窄的 min，不放大既有策略或框架兜底。
func TestSubagentRouteConcurrencyNarrowsThePolicy(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.SetSubagentPolicy(dto.SubagentPolicy{MaxForkConcurrency: 0})
	if got := runtime.currentSubagentPolicy(context.Background()).MaxForkConcurrency; got != 0 {
		t.Fatalf("disabled route limit changed policy: %d", got)
	}
	runtime.limits.Concurrency.Subagent = limit.RouteConcurrencyLimit{Enabled: true, Max: 2}
	if got := runtime.currentSubagentPolicy(context.Background()).MaxForkConcurrency; got != 2 {
		t.Fatalf("enabled route limit = %d, want 2", got)
	}
	runtime.SetSubagentPolicy(dto.SubagentPolicy{MaxForkConcurrency: 1})
	if got := runtime.currentSubagentPolicy(context.Background()).MaxForkConcurrency; got != 1 {
		t.Fatalf("narrower existing policy = %d, want 1", got)
	}
}
