package assembly

import (
	"errors"
	"github.com/RedHuang-0622/seelex/kernel/limit"
	"sync"
	"testing"
)

// 「会话驻留窗口」可调面的用例（service_release_window.go）。
//
// 这里钉住四件事：
//  1. **立刻生效**：写完这一格，释放面的现读路径（releaseBudget → Limits().ReleaseWindow()）
//     下一次决策就用新值——不需要重启，也不需要通知任何调用点；
//  2. 边界由 seelexctx 的唯一判据裁定，越界**在写盘与改内存之前**就被拒；
//  3. 落盘在改内存**之前**：写盘失败 ⇒ 内存不动（返回 0 + 错误），于是"重启后的值"
//     与"界面上的值"不会分叉，前端回滚是诚实的；
//  4. 宿主没装配落盘口时，改的值只在本次运行有效（不算失败）。
//
// 不在这里测的：真实落盘文件（backend/limit/overlay/overlay_test.go）、前端控件接线
// （gui/frontend/dist/settings-release-window.test.mjs）。

// recordingReleaseWindowStore 记录落盘调用，并可注入一次失败。
type recordingReleaseWindowStore struct {
	mu    sync.Mutex
	saved []int
	err   error
}

func (store *recordingReleaseWindowStore) SaveSessionReleaseWindow(window int) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.err != nil {
		return store.err
	}
	store.saved = append(store.saved, window)
	return nil
}

func (store *recordingReleaseWindowStore) calls() []int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return append([]int(nil), store.saved...)
}

// snapshotWindowLimits 保存并恢复**进程级** limits 快照：窗口住在包级 atomic 上，
// 用例改完必须还原，否则会渗到同包其它用例（它们读的是同一份快照）。
func snapshotWindowLimits(t *testing.T) {
	t.Helper()
	before := Limits()
	t.Cleanup(func() { ApplyLimits(before) })
}

// TestSetSessionReleaseWindowAppliesImmediately：改一个数，释放面的现读路径立刻看见。
func TestSetSessionReleaseWindowAppliesImmediately(t *testing.T) {
	snapshotWindowLimits(t)
	store := &recordingReleaseWindowStore{}
	service := newTestService(t, &fakeEngine{}, withTestReleaseWindowStore(store))

	applied, err := service.components.policyApplication.SetSessionReleaseWindow(3)
	if err != nil {
		t.Fatalf("SetSessionReleaseWindow(3): %v", err)
	}
	if applied != 3 {
		t.Fatalf("返回的生效值 = %d, want 3", applied)
	}
	if got := service.components.policyApplication.SessionReleaseWindow(); got != 3 {
		t.Fatalf("读数 = %d, want 3", got)
	}
	if got := Limits().ReleaseWindow(); got != 3 {
		t.Fatalf("进程级 limits 的窗口 = %d, want 3", got)
	}
	if saved := store.calls(); len(saved) != 1 || saved[0] != 3 {
		t.Fatalf("落盘序列 = %v, want [3]", saved)
	}
}

// TestSetSessionReleaseWindowAcceptsBoundaries：区间两端（1 与 16）都被接受。
func TestSetSessionReleaseWindowAcceptsBoundaries(t *testing.T) {
	snapshotWindowLimits(t)
	service := newTestService(t, &fakeEngine{})
	for _, window := range []int{limit.SessionReleaseWindowMin, limit.SessionReleaseWindowMax} {
		applied, err := service.components.policyApplication.SetSessionReleaseWindow(window)
		if err != nil {
			t.Fatalf("SetSessionReleaseWindow(%d) 应当被接受：%v", window, err)
		}
		if applied != window {
			t.Fatalf("返回的生效值 = %d, want %d", applied, window)
		}
	}
}

// TestSetSessionReleaseWindowRejectsOutOfRange：越界一个字节都不改——不落盘、不动内存。
func TestSetSessionReleaseWindowRejectsOutOfRange(t *testing.T) {
	snapshotWindowLimits(t)
	store := &recordingReleaseWindowStore{}
	service := newTestService(t, &fakeEngine{}, withTestReleaseWindowStore(store))
	before := service.components.policyApplication.SessionReleaseWindow()

	for _, window := range []int{0, -1, limit.SessionReleaseWindowMax + 1, 1000, 1 << 20} {
		if _, err := service.components.policyApplication.SetSessionReleaseWindow(window); err == nil {
			t.Fatalf("SetSessionReleaseWindow(%d) 应当被拒", window)
		}
	}
	if got := service.components.policyApplication.SessionReleaseWindow(); got != before {
		t.Fatalf("被拒的取值改了内存：%d → %d", before, got)
	}
	if saved := store.calls(); len(saved) != 0 {
		t.Fatalf("被拒的取值不该落盘：%v", saved)
	}
}

// TestSetSessionReleaseWindowLeavesMemoryUntouchedWhenPersistFails：落盘失败 ⇒ 整步
// 不改（返回 0 + 错误），因此"重启后还是旧值"与"界面上显示的仍是旧值"是一致的。
func TestSetSessionReleaseWindowLeavesMemoryUntouchedWhenPersistFails(t *testing.T) {
	snapshotWindowLimits(t)
	store := &recordingReleaseWindowStore{err: errors.New("disk full")}
	service := newTestService(t, &fakeEngine{}, withTestReleaseWindowStore(store))
	before := service.components.policyApplication.SessionReleaseWindow()

	applied, err := service.components.policyApplication.SetSessionReleaseWindow(before + 1)
	if err == nil {
		t.Fatal("落盘失败必须报错")
	}
	if applied != 0 {
		t.Fatalf("落盘失败时不该给出生效值：%d", applied)
	}
	if got := service.components.policyApplication.SessionReleaseWindow(); got != before {
		t.Fatalf("落盘失败却改了内存：%d → %d", before, got)
	}
	if saved := store.calls(); len(saved) != 0 {
		t.Fatalf("失败的落盘不该留下记录：%v", saved)
	}
}

// TestSetSessionReleaseWindowWithoutStoreAppliesToMemoryOnly：宿主没装配落盘口 ⇒
// 本次运行照常生效（只是重启后回到配置值），不算失败。
func TestSetSessionReleaseWindowWithoutStoreAppliesToMemoryOnly(t *testing.T) {
	snapshotWindowLimits(t)
	service := newTestService(t, &fakeEngine{})

	applied, err := service.components.policyApplication.SetSessionReleaseWindow(2)
	if err != nil {
		t.Fatalf("没装配落盘口不该失败：%v", err)
	}
	if applied != 2 || service.components.policyApplication.SessionReleaseWindow() != 2 {
		t.Fatalf("生效值 = (%d, %d), want (2, 2)", applied, service.components.policyApplication.SessionReleaseWindow())
	}
}
