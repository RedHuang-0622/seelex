package integration

import (
	"context"
	. "github.com/RedHuang-0622/seelex/application/assembly"
	"strings"
	"testing"
)

// 「每账号上下文窗口」判据已在 M5-R-limita 随接收者转移归位
// `backend/limit/accountwindow`（行为用例在那边：setter_test.go）。本文件只钉**门面这一侧**
// 自己负责的两件事，不重复判据：
//
//  1. nil Service 守卫（前端可能拿到空门面）；
//  2. 宿主端口的**类型断言**：`Deps.Runtime` 未实现 `contract.AccountWindowPort` 时，
//     必须转成"不支持"的明确错误，且不落盘、不改内存。

func TestSetAccountWindowNeedsRuntimePort(t *testing.T) {
	store := &recordingWindowStoreForForward{}
	// 默认桩 fakeRuntime 不实现 AccountWindowPort。
	service := newTestService(t, &fakeEngine{}, withTestAccountWindowStore(store))

	if _, err := ComponentsFor(service).Admin.Account.SetAccountWindow(context.Background(), "agent-1", 400_000); err == nil ||
		!strings.Contains(err.Error(), "当前宿主不支持按账号设窗口") {
		t.Fatalf("宿主不支持时必须报明确错误，got %v", err)
	}
	if store.saved {
		t.Fatal("宿主不支持时不该先落盘（盘上有、界面无效的分叉）")
	}
}

type recordingWindowStoreForForward struct{ saved bool }

func (store *recordingWindowStoreForForward) SaveAccountWindow(string, int) error {
	store.saved = true
	return nil
}
