package residency

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// 本用例原在 `application/core/snapshot_budget_test.go`，随判据（M5-R6-residency-preview）
// 一起归位；留在 core 的是宿主级用例（归档 + 预算注入 + 分页读回）。

func TestPreviewTruncates(t *testing.T) {
	limit := 8000
	big := strings.Repeat("甲", limit*2) // 多字节字符 × 2
	preview, truncated := Preview(big, limit)
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
	kept, truncated := Preview(small, limit)
	if truncated || kept != small {
		t.Fatalf("small content must pass through unchanged: truncated=%v kept=%q", truncated, kept)
	}
}

// TestPreviewKeepsUTF8BoundaryAndCountsRunes：截断点必须落在 rune 起点；
// 省略说明里的"字符数"按 rune 计（不是字节）。
func TestPreviewKeepsUTF8BoundaryAndCountsRunes(t *testing.T) {
	content := strings.Repeat("中", 4) // 12 字节
	preview, truncated := Preview(content, 4)
	if !truncated {
		t.Fatal("应判定为截断")
	}
	if strings.ContainsRune(preview, utf8.RuneError) || !utf8.ValidString(preview) {
		t.Fatalf("不得切坏多字节字符：%q", preview)
	}
	if !strings.HasPrefix(preview, "中") {
		t.Fatalf("预览应以完整汉字开头：%q", preview)
	}
	if !strings.Contains(preview, "完整输出 4 字符") {
		t.Fatalf("省略说明应按 rune 计数：%q", preview)
	}
}

// TestPreviewWithoutLimitPassesThrough：limit <= 0 = 不截断（宿主负责给正数预算）。
func TestPreviewWithoutLimitPassesThrough(t *testing.T) {
	content := strings.Repeat("x", 100)
	kept, truncated := Preview(content, 0)
	if truncated || kept != content {
		t.Fatalf("limit<=0 应原样返回：truncated=%v", truncated)
	}
}
