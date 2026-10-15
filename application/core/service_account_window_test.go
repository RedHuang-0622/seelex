package core

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/seelexctx"
)

// 「每账号上下文窗口」可调面的用例（service_account_window.go）。
//
// 这里钉四件事：
//  1. **校验在写盘与改内存之前**：越界值/空账号名被拒，盘与运行时都不动；
//  2. **先写盘再改内存**：写盘失败 ⇒ 运行时不动（返回 0 + 错误），于是"重启后的值"与
//     "界面上的值"不会分叉，前端回滚是诚实的（与 SetSessionReleaseWindow 同一纪律）；
//  3. 返回值是**真正生效**的窗口（从运行面读回，不是请求值——后端可能归一化）；
//  4. 宿主没装配落盘口时，改的值只在本次运行有效（不算失败）；没实现 AccountWindowPort
//     的宿主得到一句明确错误，而不是静默无效。
//
// 不在这里测的：覆盖层的真实落盘（根包 runtime_limits_overlay_test.go）、优先级的
// 三跳（seelebridge 的 TestRuntimeAccountWindowPriority）、前端控件接线
// （gui/frontend/dist 下的设置面用例）。

// recordingAccountWindowStore 记录落盘调用，并可注入一次失败。
type recordingAccountWindowStore struct {
	mu    sync.Mutex
	saved map[string]int
	err   error
}

func (store *recordingAccountWindowStore) SaveAccountWindow(name string, window int) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.err != nil {
		return store.err
	}
	if store.saved == nil {
		store.saved = map[string]int{}
	}
	store.saved[name] = window
	return nil
}

func (store *recordingAccountWindowStore) read(name string) (int, bool) {
	store.mu.Lock()
	defer store.mu.Unlock()
	window, ok := store.saved[name]
	return window, ok
}

// fakeAccountWindowRuntime 是实现了 AccountWindowPort 的运行面桩：账号级覆盖 > 全局值，
// 与 seelebridge 的 ContextWindowFor 同一条口径（那边有真实三跳的用例）。
type fakeAccountWindowRuntime struct {
	*fakeRuntime
	mu      sync.Mutex
	windows map[string]int
	global  int
}

func newFakeAccountWindowRuntime(global int) *fakeAccountWindowRuntime {
	return &fakeAccountWindowRuntime{fakeRuntime: &fakeRuntime{}, windows: map[string]int{}, global: global}
}

func (runtime *fakeAccountWindowRuntime) SetAccountWindow(name string, window int) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if window > 0 {
		runtime.windows[name] = window
		return
	}
	delete(runtime.windows, name)
}

func (runtime *fakeAccountWindowRuntime) AccountWindowFor(name string) int {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if window := runtime.windows[name]; window > 0 {
		return window
	}
	return runtime.global
}

// TestSetAccountWindowValidatesBeforeWriting：越界值与空账号名在动盘/动内存之前就被拒。
func TestSetAccountWindowValidatesBeforeWriting(t *testing.T) {
	store := &recordingAccountWindowStore{}
	runtime := newFakeAccountWindowRuntime(400_000)
	service := newTestService(t, &fakeEngine{},
		withTestRuntime(runtime), withTestAccountWindowStore(store))

	for _, tc := range []struct {
		label  string
		window int
	}{
		{"超过防手滑上界", 10_000_001},
		{"负数（0 才是清除覆盖）", -1},
	} {
		t.Run(tc.label, func(t *testing.T) {
			if _, err := service.SetAccountWindow(context.Background(), "agent-1", tc.window); err == nil {
				t.Fatalf("window=%d 应当被拒", tc.window)
			}
			if _, ok := store.read("agent-1"); ok {
				t.Fatal("被拒的值不该落盘")
			}
			if got := runtime.AccountWindowFor("agent-1"); got != 400_000 {
				t.Fatalf("被拒的值不该改运行面：%d, want 400000", got)
			}
		})
	}
	if _, err := service.SetAccountWindow(context.Background(), "   ", 400_000); err == nil {
		t.Fatal("空账号名应当被拒")
	}
}

// TestSetAccountWindowPersistsThenApplies：合法值先落盘、再改运行面，返回**生效值**。
func TestSetAccountWindowPersistsThenApplies(t *testing.T) {
	store := &recordingAccountWindowStore{}
	runtime := newFakeAccountWindowRuntime(400_000)
	service := newTestService(t, &fakeEngine{},
		withTestRuntime(runtime), withTestAccountWindowStore(store))

	applied, err := service.SetAccountWindow(context.Background(), "agent-1", 1_000_000)
	if err != nil {
		t.Fatalf("SetAccountWindow: %v", err)
	}
	if applied != 1_000_000 {
		t.Fatalf("返回的生效值 = %d, want 1000000", applied)
	}
	if saved, ok := store.read("agent-1"); !ok || saved != 1_000_000 {
		t.Fatalf("落盘读数 = %d/%t, want 1000000/true", saved, ok)
	}
	if got := runtime.AccountWindowFor("agent-1"); got != 1_000_000 {
		t.Fatalf("运行面读数 = %d, want 1000000", got)
	}
	// 0 = 清除该账号的覆盖（回落全局值），不是"设成 0"。
	applied, err = service.SetAccountWindow(context.Background(), "agent-1", 0)
	if err != nil {
		t.Fatalf("SetAccountWindow(清除): %v", err)
	}
	if applied != 400_000 {
		t.Fatalf("清除后的生效值 = %d, want 400000（回落全局值）", applied)
	}
	if saved, ok := store.read("agent-1"); !ok || saved != 0 {
		t.Fatalf("清除也要落盘（0 = 没有覆盖）：%d/%t", saved, ok)
	}
}

// TestSetAccountWindowStoreFailureKeepsRuntimeUntouched：写盘失败 ⇒ 运行面不动。
func TestSetAccountWindowStoreFailureKeepsRuntimeUntouched(t *testing.T) {
	store := &recordingAccountWindowStore{err: errors.New("disk full")}
	runtime := newFakeAccountWindowRuntime(400_000)
	service := newTestService(t, &fakeEngine{},
		withTestRuntime(runtime), withTestAccountWindowStore(store))

	applied, err := service.SetAccountWindow(context.Background(), "agent-1", 1_000_000)
	if err == nil {
		t.Fatal("写盘失败必须报错")
	}
	if applied != 0 {
		t.Fatalf("写盘失败时不该报生效值：%d", applied)
	}
	if got := runtime.AccountWindowFor("agent-1"); got != 400_000 {
		t.Fatalf("写盘失败不该改运行面：%d, want 400000", got)
	}
}

// TestSetAccountWindowWithoutStoreStillApplies：宿主没装落盘口 ⇒ 只本次运行有效，不算失败。
func TestSetAccountWindowWithoutStoreStillApplies(t *testing.T) {
	runtime := newFakeAccountWindowRuntime(400_000)
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))

	applied, err := service.SetAccountWindow(context.Background(), "agent-1", 600_000)
	if err != nil {
		t.Fatalf("没有落盘口不算失败：%v", err)
	}
	if applied != 600_000 || runtime.AccountWindowFor("agent-1") != 600_000 {
		t.Fatalf("应当只在本次运行生效：applied=%d runtime=%d", applied, runtime.AccountWindowFor("agent-1"))
	}
}

// TestSetAccountWindowNeedsRuntimePort：宿主没实现窄可选能力面时给出明确错误，
// 而不是静默无效（默认桩 fakeRuntime 就不实现它）。
func TestSetAccountWindowNeedsRuntimePort(t *testing.T) {
	store := &recordingAccountWindowStore{}
	service := newTestService(t, &fakeEngine{}, withTestAccountWindowStore(store))

	if _, err := service.SetAccountWindow(context.Background(), "agent-1", 400_000); err == nil {
		t.Fatal("宿主不支持时必须报错")
	}
	if _, ok := store.read("agent-1"); ok {
		t.Fatal("宿主不支持时不该先落盘（那会留下盘上有、界面无效的分叉）")
	}
}

// TestAccountContextWindowPresetsAreUsable：挡位表归后端（前端只渲染它），且每个挡位都过校验。
func TestAccountContextWindowPresetsAreUsable(t *testing.T) {
	presets := seelexctx.AccountContextWindowPresets()
	if len(presets) == 0 {
		t.Fatal("挡位表不能为空")
	}
	for index, preset := range presets {
		if err := seelexctx.ValidateAccountContextWindow(preset); err != nil {
			t.Fatalf("挡位 %d 通不过校验：%v", preset, err)
		}
		if index > 0 && preset <= presets[index-1] {
			t.Fatalf("挡位必须升序且不重复：%v", presets)
		}
	}
	if presets[0] != 200_000 || presets[len(presets)-1] != 1_000_000 {
		t.Fatalf("挡位端值 = %d..%d, want 200000..1000000", presets[0], presets[len(presets)-1])
	}
}
