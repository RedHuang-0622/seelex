package seelexctx

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/lifecycle"
	"github.com/RedHuang-0622/seelex/store"
)

// TestRouterStorageWithContextActor 集成：`store.RouterStorage` 直接作为
// ContextActor 的冷存储（基建-A 的消费方接缝）。
//
// M3-S4 把它从 `store/router_storage_test.go` 迁到本包：它是**消费侧**用例
// （需要 `lifecycle.ContextActor`），留在底座会让 store 的测试文件继续 import 上层，
// 而 I6 对测试文件同样成立。
func TestRouterStorageWithContextActor(t *testing.T) {
	root := t.TempDir()
	router, err := store.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })

	storage := store.NewRouterStorage(router, "actor-storage-test")

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

func strPtr(value string) *string { return &value }
