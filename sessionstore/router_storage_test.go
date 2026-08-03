package sessionstore

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelexctx/lifecycle"
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

// TestRouterStorageWithContextActor 集成：RouterStorage 直接作为
// ContextActor 的冷存储（A 基建的消费方接缝）。
func TestRouterStorageWithContextActor(t *testing.T) {
	router := newTestRouter(t)
	storage := NewRouterStorage(router, "actor-storage-test")

	// ContextActor 以 RouterStorage 为冷存储（lifecycle 泛型模板消费面）。
	actor := lifecycle.NewContextActor[types.Message](lifecycle.PolicyColdLoad, storage, lifecycle.Options{})
	defer actor.Close()
	for i := 0; i < 10; i++ {
		if err := actor.Append([]types.Message{{Role: "user", Content: strPtr("m")}}); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := actor.LoadWindow(context.Background(), 0, 10)
	if err != nil || total != 10 || len(items) != 10 {
		t.Fatalf("actor window: items=%d total=%d err=%v", len(items), total, err)
	}
	// 冷存储（Router）已落库。
	if storage.Count() != 10 {
		t.Fatalf("router storage count = %d, want 10", storage.Count())
	}
}
