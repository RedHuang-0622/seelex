package residency

import (
	"errors"
	"reflect"
	"testing"
)

// fakeLayer 是收敛宿主的替身：它复刻两层的共同形态——卸载成功后会话既不再
// 占用本层内存、也离开使用序（两层宿主都在动作里 `Forget`）。
type fakeLayer struct {
	order       []string
	occupied    map[string]bool
	protected   map[string]bool
	protect     string
	legacyLimit int
	window      int

	evicted    []string
	failOn     map[string]error
	failReport []string
	snapshots  int
}

func newFakeLayer(order []string, occupied, protected []string, protect string, legacyLimit, window int) *fakeLayer {
	fake := &fakeLayer{
		order:       order,
		occupied:    map[string]bool{},
		protected:   map[string]bool{},
		protect:     protect,
		legacyLimit: legacyLimit,
		window:      window,
		failOn:      map[string]error{},
	}
	for _, sessionID := range occupied {
		fake.occupied[sessionID] = true
	}
	for _, sessionID := range protected {
		fake.protected[sessionID] = true
	}
	return fake
}

func (fake *fakeLayer) snapshot() EvictionSnapshot {
	fake.snapshots++
	snapshot := EvictionSnapshot{
		Order:       append(Order(nil), fake.order...),
		Protect:     fake.protect,
		Occupied:    map[string]bool{},
		Protected:   map[string]bool{},
		LegacyLimit: fake.legacyLimit,
		Window:      fake.window,
	}
	for _, sessionID := range fake.order {
		if sessionID == "" {
			continue
		}
		if !fake.occupied[sessionID] {
			continue
		}
		snapshot.Occupied[sessionID] = true
		if fake.protected[sessionID] {
			snapshot.Protected[sessionID] = true
		}
	}
	return snapshot
}

func (fake *fakeLayer) evict(sessionID string) error {
	if err := fake.failOn[sessionID]; err != nil {
		return err
	}
	fake.evicted = append(fake.evicted, sessionID)
	delete(fake.occupied, sessionID)
	kept := make([]string, 0, len(fake.order))
	for _, existing := range fake.order {
		if existing != sessionID {
			kept = append(kept, existing)
		}
	}
	fake.order = kept
	return nil
}

func (fake *fakeLayer) deps() EvictionDeps {
	return EvictionDeps{
		Snapshot: fake.snapshot,
		Evict:    fake.evict,
		OnEvictError: func(sessionID string, err error) {
			fake.failReport = append(fake.failReport, sessionID+": "+err.Error())
		},
	}
}

// 统一窗口口径（默认）：受保护集之外只留 window 个，最旧端先走。
func TestEvictUntilWithinLimitWindowBudgetEvictsOldestFirst(t *testing.T) {
	// 使用序索引 0 = 最近使用 ⇒ 末尾（"a"）最旧。
	fake := newFakeLayer([]string{"c", "b", "a"}, []string{"a", "b", "c"}, nil, "", 0, 1)
	EvictUntilWithinLimit(fake.deps())
	if want := []string{"a", "b"}; !reflect.DeepEqual(fake.evicted, want) {
		t.Fatalf("evicted = %v, want %v（最旧端先卸载，直到额度内）", fake.evicted, want)
	}
	if !fake.occupied["c"] || fake.occupied["a"] || fake.occupied["b"] {
		t.Fatalf("loaded = %v, want 只剩 c", fake.occupied)
	}
}

// 受保护集**既不计数也不参与挑选**：窗口只约束"受保护集之外还能留几个"，
// 受保护的会话再多也不会把窗口挤没，于是只剩一个候选时会容忍"看起来超限"。
func TestEvictUntilWithinLimitProtectedNeitherCountedNorChosen(t *testing.T) {
	fake := newFakeLayer([]string{"d", "c", "b", "a"}, []string{"a", "b", "c", "d"}, []string{"c"}, "d", 0, 1)
	EvictUntilWithinLimit(fake.deps())
	if want := []string{"a"}; !reflect.DeepEqual(fake.evicted, want) {
		t.Fatalf("evicted = %v, want %v（只卸最旧的一个即认账：受保护数不进窗口折算）", fake.evicted, want)
	}
	for _, sessionID := range []string{"d", "c", "b"} {
		if !fake.occupied[sessionID] {
			t.Fatalf("受保护/触发会话被卸载了：%s（loaded=%v）", sessionID, fake.occupied)
		}
	}
}

// 分层口径（legacyLimit > 0）：额度 = 该层总上限 − 受保护数。
func TestEvictUntilWithinLimitLegacyLimitMinusProtected(t *testing.T) {
	fake := newFakeLayer([]string{"e", "d", "c", "b", "a"}, []string{"a", "b", "c", "d", "e"}, []string{"d"}, "e", 3, 1)
	EvictUntilWithinLimit(fake.deps())
	// 受保护数 = 2（e 触发 + d 受保护） ⇒ 额度 = 3 − 2 = 1 ⇒ 从最旧端起卸载 2 个。
	if want := []string{"a", "b"}; !reflect.DeepEqual(fake.evicted, want) {
		t.Fatalf("evicted = %v, want %v", fake.evicted, want)
	}
}

// 本已不占用本层内存的会话既不进候选也不计数（分层上限与统一窗口都不该被它们占位）。
func TestEvictUntilWithinLimitSkipsSessionsWithoutContent(t *testing.T) {
	fake := newFakeLayer([]string{"c", "b", "a"}, []string{"a", "c"}, nil, "", 0, 1)
	EvictUntilWithinLimit(fake.deps())
	if want := []string{"a"}; !reflect.DeepEqual(fake.evicted, want) {
		t.Fatalf("evicted = %v, want %v（b 不占用本层，不占额度）", fake.evicted, want)
	}
	if fake.evicted[0] == "b" {
		t.Fatal("不占用本层的会话被卸载了")
	}
}

// 空 ID 一律跳过（使用序里可能残留空槽）。
func TestEvictUntilWithinLimitSkipsEmptyIDs(t *testing.T) {
	fake := newFakeLayer([]string{"", "b", ""}, []string{"b"}, nil, "", 0, 0)
	EvictUntilWithinLimit(fake.deps())
	if want := []string{"b"}; !reflect.DeepEqual(fake.evicted, want) {
		t.Fatalf("evicted = %v, want %v", fake.evicted, want)
	}
}

// 没有候选（全受保护）⇒ 容忍超限，一个都不动。
func TestEvictUntilWithinLimitToleratesOverLimitWithoutCandidates(t *testing.T) {
	fake := newFakeLayer([]string{"c", "b", "a"}, []string{"a", "b", "c"}, []string{"a", "b", "c"}, "", 0, 1)
	EvictUntilWithinLimit(fake.deps())
	if len(fake.evicted) != 0 {
		t.Fatalf("evicted = %v, want 空（无候选时容忍超限）", fake.evicted)
	}
	if fake.snapshots != 1 {
		t.Fatalf("snapshots = %d, want 1（无候选即返回，不空转）", fake.snapshots)
	}
}

// 卸载失败立即停：报一次错、不再挑下一个（数据留在内存，下一轮再试）。
func TestEvictUntilWithinLimitStopsOnEvictFailure(t *testing.T) {
	fake := newFakeLayer([]string{"c", "b", "a"}, []string{"a", "b", "c"}, nil, "", 0, 0)
	fake.failOn["a"] = errors.New("flush failed")
	EvictUntilWithinLimit(fake.deps())
	if len(fake.evicted) != 0 {
		t.Fatalf("evicted = %v, want 空（首个候选失败即停）", fake.evicted)
	}
	if want := []string{"a: flush failed"}; !reflect.DeepEqual(fake.failReport, want) {
		t.Fatalf("failReport = %v, want %v", fake.failReport, want)
	}
	if fake.snapshots != 1 {
		t.Fatalf("snapshots = %d, want 1（失败即停，不再取数）", fake.snapshots)
	}
}

// 额度已经满足（候选数 <= 额度）时不取动作：一次取数即返回。
func TestEvictUntilWithinLimitNoOpWhenWithinBudget(t *testing.T) {
	fake := newFakeLayer([]string{"b", "a"}, []string{"a", "b"}, nil, "", 0, 2)
	EvictUntilWithinLimit(fake.deps())
	if len(fake.evicted) != 0 || fake.snapshots != 1 {
		t.Fatalf("evicted = %v, snapshots = %d, want 空 / 1", fake.evicted, fake.snapshots)
	}
}

// 装配缺陷（缺 Snapshot 或 Evict）不许 panic：静默返回，由装配期测试兜住。
func TestEvictUntilWithinLimitWithoutDepsIsSafe(t *testing.T) {
	EvictUntilWithinLimit(EvictionDeps{})
	EvictUntilWithinLimit(EvictionDeps{Snapshot: func() EvictionSnapshot { return EvictionSnapshot{} }})
}
