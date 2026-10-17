package residency

import (
	"reflect"
	"testing"
)

// 本文件钉"使用序维护"与"释放额度"这两条判据——迁移前它们在四个地方各写了一份
// （引擎驻留层 / 正文层 / 归档路径 / 释放面），现在只有这一份。

func TestOrderTouchMovesToFrontAndDedupes(t *testing.T) {
	order := Order{"a", "b", "c"}
	order = order.Touch("c")
	if want := (Order{"c", "a", "b"}); !reflect.DeepEqual(order, want) {
		t.Fatalf("重复 touch 应去重并前置：got %v want %v", order, want)
	}
	order = order.Touch("d")
	if want := (Order{"d", "c", "a", "b"}); !reflect.DeepEqual(order, want) {
		t.Fatalf("新会话应前置：got %v want %v", order, want)
	}
	// 空 ID 无效：原样返回（调用方负责守卫）。
	if got := order.Touch(""); !reflect.DeepEqual(got, order) {
		t.Fatalf("空 ID 不该改动序：got %v", got)
	}
}

func TestOrderTouchDoesNotMutateUnderlying(t *testing.T) {
	base := Order{"a", "b"}
	_ = base.Touch("c")
	if want := (Order{"a", "b"}); !reflect.DeepEqual(base, want) {
		t.Fatalf("Touch 不得改动原序（调用方重新赋值）：got %v", base)
	}
}

func TestOrderForgetKeepsRelativeOrder(t *testing.T) {
	order := Order{"a", "b", "c", "b"}
	got := order.Forget("b")
	if want := (Order{"a", "c"}); !reflect.DeepEqual(got, want) {
		t.Fatalf("Forget 应摘掉全部同名项且保持相对顺序：got %v want %v", got, want)
	}
	if got := (Order{"a"}).Forget(""); !reflect.DeepEqual(got, Order{"a"}) {
		t.Fatalf("空 ID 不该改动序：got %v", got)
	}
	if got := (Order(nil)).Forget("a"); got != nil {
		t.Fatalf("空序应原样返回：got %v", got)
	}
}

func TestBudgetFollowsWindowOrLegacyLimit(t *testing.T) {
	for _, tc := range []struct {
		legacy, protected, window, want int
	}{
		{0, 3, 1, 1}, // 统一窗口：不看受保护数
		{0, 0, 2, 2}, // 窗口可配
		{6, 2, 1, 4}, // 分层口径：总上限 − 受保护数
		{3, 1, 1, 2}, // 同上
		{1, 2, 1, 0}, // 受保护数已超上限：额度 0（不驱逐）
		{2, 2, 1, 0}, // 相等也是 0
	} {
		if got := Budget(tc.legacy, tc.protected, tc.window); got != tc.want {
			t.Fatalf("Budget(%d,%d,%d) = %d, want %d", tc.legacy, tc.protected, tc.window, got, tc.want)
		}
	}
}
