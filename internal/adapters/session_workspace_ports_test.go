package adapters

import (
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// TestAdaptGranularInfosCarriesTimelineFields（左侧栏日期占位回归）：目录
// 枚举摘要从 sessionstore 适配到 model.SessionInfo 时，updated_at 与
// token_count 必须透传。适配层把它们置零会让快照里的 updated_at 恒为
// 0001-01-01T00:00:00Z，前端侧栏每条会话都渲染成同一个占位日期。
func TestAdaptGranularInfosCarriesTimelineFields(t *testing.T) {
	now := time.Date(2026, 9, 2, 10, 30, 0, 0, time.UTC)
	rows := []sessionstore.SessionInfo{
		{ID: "s1", Title: "会话一", Status: sessionstore.StatusIdle, UpdatedAt: now, TokenCount: 42},
	}
	out := adaptGranularInfos(rows)
	if len(out) != 1 {
		t.Fatalf("adapt len = %d, want 1", len(out))
	}
	row := out[0]
	if row.ID != "s1" || row.Name != "会话一" || string(row.Status) != "idle" {
		t.Fatalf("identity fields lost: %+v", row)
	}
	if row.UpdatedAt.IsZero() || !row.UpdatedAt.Equal(now) {
		t.Fatalf("updated_at = %v, want %v（不得置零成占位日期）", row.UpdatedAt, now)
	}
	if row.TokenCount != 42 {
		t.Fatalf("token_count = %d, want 42", row.TokenCount)
	}
}
