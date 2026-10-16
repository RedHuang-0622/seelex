package residency

import (
	"errors"
	"reflect"
	"testing"
)

// fakeContentLayer 是正文层收敛的宿主替身：它复刻宿主的形态——卸载成功后会话
// 既不再持有正文、也离开使用序（宿主在 `evictLoadedContent` 里 `Forget`）。
type fakeContentLayer struct {
	order       []string
	loaded      map[string]bool
	protected   map[string]bool
	protect     string
	legacyLimit int
	window      int

	evicted    []string
	failOn     map[string]error
	failReport []string
	snapshots  int
}

func newFakeContentLayer(order []string, loaded, protected []string, protect string, legacyLimit, window int) *fakeContentLayer {
	fake := &fakeContentLayer{
		order:       order,
		loaded:      map[string]bool{},
		protected:   map[string]bool{},
		protect:     protect,
		legacyLimit: legacyLimit,
		window:      window,
		failOn:      map[string]error{},
	}
	for _, sessionID := range loaded {
		fake.loaded[sessionID] = true
	}
	for _, sessionID := range protected {
		fake.protected[sessionID] = true
	}
	return fake
}

func (fake *fakeContentLayer) snapshot() ContentLimitSnapshot {
	fake.snapshots++
	snapshot := ContentLimitSnapshot{
		Order:       append(Order(nil), fake.order...),
		Protect:     fake.protect,
		Loaded:      map[string]bool{},
		Protected:   map[string]bool{},
		LegacyLimit: fake.legacyLimit,
		Window:      fake.window,
	}
	for _, sessionID := range fake.order {
		if sessionID == "" {
			continue
		}
		if !fake.loaded[sessionID] {
			continue
		}
		snapshot.Loaded[sessionID] = true
		if fake.protected[sessionID] {
			snapshot.Protected[sessionID] = true
		}
	}
	return snapshot
}

func (fake *fakeContentLayer) evict(sessionID string) error {
	if err := fake.failOn[sessionID]; err != nil {
		return err
	}
	fake.evicted = append(fake.evicted, sessionID)
	delete(fake.loaded, sessionID)
	kept := make([]string, 0, len(fake.order))
	for _, existing := range fake.order {
		if existing != sessionID {
			kept = append(kept, existing)
		}
	}
	fake.order = kept
	return nil
}

func (fake *fakeContentLayer) deps() ContentLimitDeps {
	return ContentLimitDeps{
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
	fake := newFakeContentLayer([]string{"c", "b", "a"}, []string{"a", "b", "c"}, nil, "", 0, 1)
	EvictUntilWithinLimit(fake.deps())
	if want := []string{"a", "b"}; !reflect.DeepEqual(fake.evicted, want) {
		t.Fatalf("evicted = %v, want %v（最旧端先卸载，直到额度内）", fake.evicted, want)
	}
	if !fake.loaded["c"] || fake.loaded["a"] || fake.loaded["b"] {
		t.Fatalf("loaded = %v, want 只剩 c", fake.loaded)
	}
}

// 受保护集**既不计数也不参与挑选**：窗口只约束"受保护集之外还能留几个"，
// 受保护的会话再多也不会把窗口挤没，于是只剩一个候选时会容忍"看起来超限"。
func TestEvictUntilWithinLimitProtectedNeitherCountedNorChosen(t *testing.T) {
	fake := newFakeContentLayer([]string{"d", "c", "b", "a"}, []string{"a", "b", "c", "d"}, []string{"c"}, "d", 0, 1)
	EvictUntilWithinLimit(fake.deps())
	if want := []string{"a"}; !reflect.DeepEqual(fake.evicted, want) {
		t.Fatalf("evicted = %v, want %v（只卸最旧的一个即认账：受保护数不进窗口折算）", fake.evicted, want)
	}
	for _, sessionID := range []string{"d", "c", "b"} {
		if !fake.loaded[sessionID] {
			t.Fatalf("受保护/触发会话被卸载了：%s（loaded=%v）", sessionID, fake.loaded)
		}
	}
}

// 分层口径（legacyLimit > 0）：额度 = 该层总上限 − 受保护数。
func TestEvictUntilWithinLimitLegacyLimitMinusProtected(t *testing.T) {
	fake := newFakeContentLayer([]string{"e", "d", "c", "b", "a"}, []string{"a", "b", "c", "d", "e"}, []string{"d"}, "e", 3, 1)
	EvictUntilWithinLimit(fake.deps())
	// 受保护数 = 2（e 触发 + d 受保护） ⇒ 额度 = 3 − 2 = 1 ⇒ 从最旧端起卸载 2 个。
	if want := []string{"a", "b"}; !reflect.DeepEqual(fake.evicted, want) {
		t.Fatalf("evicted = %v, want %v", fake.evicted, want)
	}
}

// 本已无正文的会话既不进候选也不计数（`loaded_content_limit` 与统一窗口都不该被它们占位）。
func TestEvictUntilWithinLimitSkipsSessionsWithoutContent(t *testing.T) {
	fake := newFakeContentLayer([]string{"c", "b", "a"}, []string{"a", "c"}, nil, "", 0, 1)
	EvictUntilWithinLimit(fake.deps())
	if want := []string{"a"}; !reflect.DeepEqual(fake.evicted, want) {
		t.Fatalf("evicted = %v, want %v（b 无正文，不占额度）", fake.evicted, want)
	}
	if fake.evicted[0] == "b" {
		t.Fatal("无正文的会话被卸载了")
	}
}

// 空 ID 一律跳过（使用序里可能残留空槽）。
func TestEvictUntilWithinLimitSkipsEmptyIDs(t *testing.T) {
	fake := newFakeContentLayer([]string{"", "b", ""}, []string{"b"}, nil, "", 0, 0)
	EvictUntilWithinLimit(fake.deps())
	if want := []string{"b"}; !reflect.DeepEqual(fake.evicted, want) {
		t.Fatalf("evicted = %v, want %v", fake.evicted, want)
	}
}

// 没有候选（全受保护）⇒ 容忍超限，一个都不动。
func TestEvictUntilWithinLimitToleratesOverLimitWithoutCandidates(t *testing.T) {
	fake := newFakeContentLayer([]string{"c", "b", "a"}, []string{"a", "b", "c"}, []string{"a", "b", "c"}, "", 0, 1)
	EvictUntilWithinLimit(fake.deps())
	if len(fake.evicted) != 0 {
		t.Fatalf("evicted = %v, want 空（无候选时容忍超限）", fake.evicted)
	}
	if fake.snapshots != 1 {
		t.Fatalf("snapshots = %d, want 1（无候选即返回，不空转）", fake.snapshots)
	}
}

// 卸载失败立即停：报一次错、不再挑下一个（正文留在内存，下一轮再试）。
func TestEvictUntilWithinLimitStopsOnEvictFailure(t *testing.T) {
	fake := newFakeContentLayer([]string{"c", "b", "a"}, []string{"a", "b", "c"}, nil, "", 0, 0)
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
	fake := newFakeContentLayer([]string{"b", "a"}, []string{"a", "b"}, nil, "", 0, 2)
	EvictUntilWithinLimit(fake.deps())
	if len(fake.evicted) != 0 || fake.snapshots != 1 {
		t.Fatalf("evicted = %v, snapshots = %d, want 空 / 1", fake.evicted, fake.snapshots)
	}
}

// 装配缺陷（缺 Snapshot 或 Evict）不许 panic：静默返回，由装配期测试兜住。
func TestEvictUntilWithinLimitWithoutDepsIsSafe(t *testing.T) {
	EvictUntilWithinLimit(ContentLimitDeps{})
	EvictUntilWithinLimit(ContentLimitDeps{Snapshot: func() ContentLimitSnapshot { return ContentLimitSnapshot{} }})
}
