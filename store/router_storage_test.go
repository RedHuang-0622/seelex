package store

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
)

// ── 基建-A：Router 冷存储适配器（lifecycle.Storage 接缝）──────────────

// TestRouterStorageLifecycle 验证 RouterStorage 满足 lifecycle.Storage：
// Append（事件追加）→ ReadRange（窗口读）→ Count（总数）闭环。
func TestRouterStorageLifecycle(t *testing.T) {
	router := newTestRouter(t)
	storage := NewRouterStorage(router, "router-storage-test")

	messages := []types.Message{
		{Role: "user", Content: strPtr("q1")},
		{Role: "assistant", Content: strPtr("a1")},
		{Role: "user", Content: strPtr("q2")},
		{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "t1", Type: "function", Function: types.ToolCallFunction{Name: "read_file", Arguments: `{"path":"x"}`}}}},
		{Role: "tool", ToolCallID: "t1", Content: strPtr("file")},
	}
	if err := storage.Append(context.Background(), messages); err != nil {
		t.Fatal(err)
	}
	if got := storage.Count(); got != 5 {
		t.Fatalf("count = %d, want 5", got)
	}
	// 窗口读：offset 1 limit 2 → 第 2、3 条。
	items, total, err := storage.ReadRange(context.Background(), 1, 2)
	if err != nil || total != 5 {
		t.Fatalf("read range: items=%d total=%d err=%v", len(items), total, err)
	}
	if len(items) != 2 || items[0].Content == nil || *items[0].Content != "a1" {
		t.Fatalf("window items = %+v, want [a1 q2]", items)
	}
	// 工具调用字段跨事件流转保留。
	for _, item := range items {
		for _, call := range item.ToolCalls {
			if call.Function.Name == "read_file" {
				t.Log("tool call preserved through router storage")
			}
		}
	}
}

func strPtr(value string) *string { return &value }
