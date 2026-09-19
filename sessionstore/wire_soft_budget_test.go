package sessionstore

import (
	"testing"
	"time"
)

// TestApplyWireBudgetDerivesSoftThreshold 钉住 §5.2 配置的装配侧消费：
// 绝对预算按 WireSoftRatio 缩放到软阈值；未给预算时用配置绝对值推导。
// （回归前：wireBudget() 只有测试断言，装配侧硬编码单一 Budget，软阈值不生效。）
func TestApplyWireBudgetDerivesSoftThreshold(t *testing.T) {
	settings := defaultStorageSettings()

	scaled := settings.applyWireBudget(wireParams{Budget: 100_000, K: 3})
	if scaled.Budget != 75_000 {
		t.Fatalf("applyWireBudget(100000) = %d, want 75000 (soft ratio 0.75)", scaled.Budget)
	}

	fallback := settings.applyWireBudget(wireParams{})
	if fallback.Budget != 150_000 {
		t.Fatalf("applyWireBudget(no budget) = %d, want 150000 (200000 × 0.75)", fallback.Budget)
	}
}

// TestWireAssemblyStopsAtSoftThreshold 钉住「软阈值触发压缩」：装配在软阈值处
// 收口到完整协议单元边界并置 need_compact，而不是等到全量预算。
func TestWireAssemblyStopsAtSoftThreshold(t *testing.T) {
	rows := make([]Event, 0, 8)
	for index := 1; index <= 8; index++ {
		rows = append(rows, Event{
			Seq: uint64(index), Role: "user", Kind: EventKindUserInput,
			Content: "材料", TokenCount: 100, CreatedAt: time.Now(),
		})
	}

	// 全量预算 400：不设软阈值时装配 4 条才越界。
	full := assembleWireRows(nil, rows, nil, wireParams{Budget: 400})
	if len(full.Messages) != 4 || !full.NeedCompact {
		t.Fatalf("full budget wire len=%d needCompact=%v, want 4/true", len(full.Messages), full.NeedCompact)
	}

	// 软阈值 300（= 400 × 0.75）：300/100 = 3 条即收口，不越过软阈值。
	soft := assembleWireRows(nil, rows, nil, defaultStorageSettings().applyWireBudget(wireParams{Budget: 400}))
	if len(soft.Messages) != 3 {
		t.Fatalf("soft threshold wire len=%d, want 3 (300 soft budget / 100 per row)", len(soft.Messages))
	}
	if !soft.NeedCompact {
		t.Fatal("need_compact missing when assembly stops at the soft threshold")
	}
}
