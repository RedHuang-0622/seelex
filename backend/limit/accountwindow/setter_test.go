package accountwindow

import (
	"errors"
	"sync"
	"testing"
)

// 「每账号上下文窗口」可调面的用例（setter.go）。原在 `application/assembly/service_account_window_test.go`，
// 随判据（M5-R-limita 的接收者转移）一起归位到判据所在包。
//
// 这里钉五件事：
//  1. **校验在写盘与改内存之前**：越界值/空账号名被拒，盘与运行面都不动；
//  2. **先写盘再改内存**：写盘失败 ⇒ 运行面不动（返回 0 + 错误），于是"重启后的值"与
//     "界面上的值"不会分叉；
//  3. 返回值是**真正生效**的窗口（从运行面读回，不是请求值——宿主可能归一化）；
//  4. 宿主没装配落盘口时，改的值只在本次运行有效（不算失败）；宿主没实现 AccountWindowPort
//     时得到一句明确错误，而不是静默无效；
//  5. 挡位表归后端（前端只渲染它），每个挡位都过校验且升序。
//
// 不在这里测的：覆盖层的真实落盘（backend/limit/overlay/overlay_test.go）、优先级的
// 三跳（assemble 的 RuntimeAccountWindow 优先级）、前端控件接线（gui/frontend/dist）。

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

// fakeAccountWindowRuntime 是实现了 contract.AccountWindowPort 的运行面桩：账号级覆盖 > 全局值
// （与真实运行面的 ContextWindowFor 同一条口径）。
type fakeAccountWindowRuntime struct {
	mu      sync.Mutex
	windows map[string]int
	global  int
}

func newFakeAccountWindowRuntime(global int) *fakeAccountWindowRuntime {
	return &fakeAccountWindowRuntime{windows: map[string]int{}, global: global}
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

// TestSetterValidatesBeforeWriting：越界值与空账号名在动盘/动内存之前就被拒。
func TestSetterValidatesBeforeWriting(t *testing.T) {
	store := &recordingAccountWindowStore{}
	runtime := newFakeAccountWindowRuntime(400_000)
	setter := Setter{Runtime: runtime, Store: store}

	for _, tc := range []struct {
		label  string
		window int
	}{
		{"超过防手滑上界", MaxWindow + 1},
		{"负数（0 才是清除覆盖）", -1},
	} {
		t.Run(tc.label, func(t *testing.T) {
			if _, err := setter.Set("agent-1", tc.window); err == nil {
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
	if _, err := setter.Set("   ", 400_000); err == nil {
		t.Fatal("空账号名应当被拒")
	}
	if _, err := setter.Set("agent-1", 400_000); err != nil {
		t.Fatalf("合法值不该被拒：%v", err)
	}
}

// TestSetterPersistsThenApplies：合法值先落盘、再改运行面，返回**生效值**。
func TestSetterPersistsThenApplies(t *testing.T) {
	store := &recordingAccountWindowStore{}
	runtime := newFakeAccountWindowRuntime(400_000)
	setter := Setter{Runtime: runtime, Store: store}

	applied, err := setter.Set("agent-1", 1_000_000)
	if err != nil {
		t.Fatalf("Set: %v", err)
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
	applied, err = setter.Set("agent-1", 0)
	if err != nil {
		t.Fatalf("Set(清除): %v", err)
	}
	if applied != 400_000 {
		t.Fatalf("清除后的生效值 = %d, want 400000（回落全局值）", applied)
	}
	if saved, ok := store.read("agent-1"); !ok || saved != 0 {
		t.Fatalf("清除也要落盘（0 = 没有覆盖）：%d/%t", saved, ok)
	}
}

// TestSetterStoreFailureKeepsRuntimeUntouched：写盘失败 ⇒ 运行面不动。
func TestSetterStoreFailureKeepsRuntimeUntouched(t *testing.T) {
	store := &recordingAccountWindowStore{err: errors.New("disk full")}
	runtime := newFakeAccountWindowRuntime(400_000)
	setter := Setter{Runtime: runtime, Store: store}

	applied, err := setter.Set("agent-1", 1_000_000)
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

// TestSetterWithoutStoreStillApplies：宿主没装落盘口 ⇒ 只本次运行有效，不算失败。
func TestSetterWithoutStoreStillApplies(t *testing.T) {
	runtime := newFakeAccountWindowRuntime(400_000)
	setter := Setter{Runtime: runtime}

	applied, err := setter.Set("agent-1", 600_000)
	if err != nil {
		t.Fatalf("没有落盘口不算失败：%v", err)
	}
	if applied != 600_000 || runtime.AccountWindowFor("agent-1") != 600_000 {
		t.Fatalf("应当只在本次运行生效：applied=%d runtime=%d", applied, runtime.AccountWindowFor("agent-1"))
	}
}

// TestSetterNeedsRuntimePort：宿主没实现窄可选能力面时给出明确错误，而不是静默无效；
// 且**不先落盘**（那会留下盘上有、界面无效的分叉）。
func TestSetterNeedsRuntimePort(t *testing.T) {
	store := &recordingAccountWindowStore{}
	setter := Setter{Store: store} // Runtime = nil

	if _, err := setter.Set("agent-1", 400_000); err == nil {
		t.Fatal("宿主不支持时必须报错")
	}
	if _, ok := store.read("agent-1"); ok {
		t.Fatal("宿主不支持时不该先落盘")
	}
}

// TestAccountContextWindowPresetsAreUsable：挡位表归后端（前端只渲染它），且每个挡位都过校验。
func TestAccountContextWindowPresetsAreUsable(t *testing.T) {
	presets := Presets()
	if len(presets) == 0 {
		t.Fatal("挡位表不能为空")
	}
	for index, preset := range presets {
		if err := Validate(preset); err != nil {
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
