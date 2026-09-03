package core

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/RedHuang-0622/seelex/application/core/internal/limits"
)

// TestToolOutputPreviewTruncates 验证快照预览截断：超过阈值只留前 N 字符
// + 可读省略说明，且不切坏 UTF-8 多字节字符。
func TestToolOutputPreviewTruncates(t *testing.T) {
	limit := snapshotToolOutputLimit()
	big := strings.Repeat("甲", limit*2) // 多字节字符 × 2
	preview, truncated := toolOutputPreview(big, limit)
	if !truncated {
		t.Fatal("big content must be flagged truncated")
	}
	if !utf8.ValidString(preview) {
		t.Fatal("preview must be valid UTF-8")
	}
	if len(preview) > limit+64 {
		t.Fatalf("preview too large: %d bytes (limit %d)", len(preview), limit)
	}
	if !strings.Contains(preview, "快照已截断") {
		t.Fatalf("preview must carry truncation note, got: %q", preview)
	}

	small := "hello"
	kept, truncated := toolOutputPreview(small, limit)
	if truncated || kept != small {
		t.Fatalf("small content must pass through unchanged: truncated=%v kept=%q", truncated, kept)
	}
}

// TestBoundToolResultForSnapshotArchives 验证截断链入口：超限输出归档为
// result_ref，快照只留预览；未超限原样返回且无引用。
func TestBoundToolResultForSnapshotArchives(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	limit := snapshotToolOutputLimit()
	big := strings.Repeat("y", limit+1000)

	service.ViewMu.Lock()
	visible, ref, truncated, total := service.boundToolResultForSnapshot("bash", big)
	service.ViewMu.Unlock()

	if !truncated {
		t.Fatal("big content must be truncated")
	}
	if ref == "" {
		t.Fatal("big content must archive a result_ref")
	}
	if !strings.HasPrefix(ref, "tr-") {
		t.Fatalf("ref must be tr- prefixed, got %q", ref)
	}
	if total != len(big) {
		t.Fatalf("total chars = %d, want %d", total, len(big))
	}
	if !strings.Contains(visible, "快照已截断") {
		t.Fatalf("visible preview must carry note, got %q", visible)
	}
	// 归档已进入 pending 通道（ToolResultContent 立即可读，无需落盘）。
	page, err := service.ToolResultContent(t.Context(), ref, 0, 64)
	if err != nil {
		t.Fatalf("read archived content: %v", err)
	}
	if page.TotalBytes != len(big) {
		t.Fatalf("page total bytes = %d, want %d", page.TotalBytes, len(big))
	}
	if !strings.HasPrefix(page.Content, "y") {
		t.Fatalf("page content must start with original content, got %q", page.Content[:8])
	}

	// 小输出：原样 + 无引用。
	service.ViewMu.Lock()
	visible, ref, truncated, _ = service.boundToolResultForSnapshot("read_file", "small")
	service.ViewMu.Unlock()
	if truncated || ref != "" || visible != "small" {
		t.Fatalf("small output must pass through: truncated=%v ref=%q visible=%q", truncated, ref, visible)
	}
}

// TestToolResultContentPagination 验证分页读回：offset/limit/UTF-8 边界
// 与 has_more 语义（GUI 与 read_tool_result 共用 buildToolResultPage）。
func TestToolResultContentPagination(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	big := strings.Repeat("汉字", 5000) // 15000 bytes
	service.ViewMu.Lock()
	_, ref, _, _ := service.boundToolResultForSnapshot("bash", big)
	service.ViewMu.Unlock()

	page, err := service.ToolResultContent(t.Context(), ref, 0, 1000)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if !page.HasMore || page.NextOffset == 0 || page.Offset != 0 {
		t.Fatalf("first page pagination wrong: %+v", page)
	}
	if utf8.RuneCountInString(page.Content) > 1000+1 {
		t.Fatalf("page content exceeds limit: %d runes", utf8.RuneCountInString(page.Content))
	}

	next, err := service.ToolResultContent(t.Context(), ref, page.NextOffset, 1000)
	if err != nil {
		t.Fatalf("next page: %v", err)
	}
	if next.Offset != page.NextOffset {
		t.Fatalf("next offset = %d, want %d", next.Offset, page.NextOffset)
	}

	// 非法参数：空 ref / 负 offset。
	if _, err := service.ToolResultContent(t.Context(), "", 0, 0); err == nil {
		t.Fatal("empty ref must error")
	}
	if _, err := service.ToolResultContent(t.Context(), ref, -1, 0); err == nil {
		t.Fatal("negative offset must error")
	}
}

// TestSnapshotBudgetUsesConfiguredLimit 验证 limits 注入生效：设置较小
// snapshot_tool_output_chars 后，截断线跟随配置（而非硬编码）。
func TestSnapshotBudgetUsesConfiguredLimit(t *testing.T) {
	previous := limits.Get()
	configured := previous
	configured.SnapshotToolOutputChars = 128
	limits.Apply(configured)
	defer limits.Apply(previous)

	service := newTestService(t, &fakeEngine{})
	big := strings.Repeat("z", 4096)
	service.ViewMu.Lock()
	visible, ref, truncated, _ := service.boundToolResultForSnapshot("bash", big)
	service.ViewMu.Unlock()
	if !truncated || ref == "" {
		t.Fatal("with 128-char limit, 4KB content must be truncated and archived")
	}
	if len(visible) > 128+64 {
		t.Fatalf("preview exceeds configured limit: %d bytes", len(visible))
	}
}
