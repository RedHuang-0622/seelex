package account

import "testing"

// TestWindowOverlaySeedFiltering 钉住**入口判据**：起点表里的空白名与非正值不进来，
// 空表归一为 nil（"没设过"与"设过 0"必须分得开）。
func TestWindowOverlaySeedFiltering(t *testing.T) {
	overlay := NewWindowOverlay(map[string]int{
		"agent-1":  600_000,
		"  ":       500_000,
		"agent-2":  0,
		"agent-3":  -1,
		"	agent-4": 200_000, // 有空白但名字非空 ⇒ 保留原名下的值
	})
	if got := overlay.For("agent-1"); got != 600_000 {
		t.Fatalf("For(agent-1) = %d, want 600000", got)
	}
	for _, name := range []string{"", "  ", "agent-2", "agent-3"} {
		if got := overlay.For(name); got != 0 {
			t.Fatalf("For(%q) = %d, want 0（非正值与空白名不进表）", name, got)
		}
	}
	if got := overlay.Snapshot(); len(got) != 2 {
		t.Fatalf("Snapshot() = %v, want 只有两格", got)
	}
	if got := NewWindowOverlay(nil).Snapshot(); got != nil {
		t.Fatalf("空表 Snapshot() = %v, want nil", got)
	}
	if got := NewWindowOverlay(map[string]int{"": 1}).Snapshot(); got != nil {
		t.Fatalf("全被过滤掉的 Snapshot() = %v, want nil", got)
	}
}

// TestWindowOverlaySetClearsEntry 钉住"改一格 / 清一格"的判据：>0 写入；<=0 是**清除**
// （回落到全局值，不是设成 0）；空白名忽略。
func TestWindowOverlaySetClearsEntry(t *testing.T) {
	overlay := NewWindowOverlay(nil)
	overlay.Set("agent-1", 600_000)
	if got := overlay.For("agent-1"); got != 600_000 {
		t.Fatalf("Set 后 For = %d, want 600000", got)
	}
	overlay.Set("agent-1", 0)
	if got := overlay.For("agent-1"); got != 0 {
		t.Fatalf("清除后 For = %d, want 0", got)
	}
	if _, ok := overlay.Snapshot()["agent-1"]; ok {
		t.Fatalf("清除后不该还在表里：%v", overlay.Snapshot())
	}
	overlay.Set("agent-1", 700_000)
	overlay.Set("agent-1", -1) // 负数同样按"清除"处理（校验在界面侧，这里只管表）
	if _, ok := overlay.Snapshot()["agent-1"]; ok {
		t.Fatalf("负数应当清除这一格：%v", overlay.Snapshot())
	}
	overlay.Set("   ", 800_000)
	if got := overlay.Snapshot(); got != nil {
		t.Fatalf("空白账号名不该落表：%v", got)
	}
}

// TestWindowOverlayEffectivePrecedence 钉住**唯一一处优先级**：账号级覆盖 >
// `limits.context_window` 全局值 > 账号档自报（遗留兜底）。三条支路各走一次。
func TestWindowOverlayEffectivePrecedence(t *testing.T) {
	fallback := func(name string) int {
		switch name {
		case "agent-1":
			return 200_000
		case "agent-2":
			return 128_000
		default:
			return 0
		}
	}
	deps := WindowDeps{Global: 400_000, AccountFallback: fallback}

	// ② 全局值压账号档自报。
	overlay := NewWindowOverlay(nil)
	if got := overlay.Effective("agent-1", deps); got != 400_000 {
		t.Fatalf("Effective(agent-1) = %d, want 400000（全局压账号档）", got)
	}
	// ① 账号级覆盖压全局值。
	overlay.Set("agent-2", 1_000_000)
	if got := overlay.Effective("agent-2", deps); got != 1_000_000 {
		t.Fatalf("Effective(agent-2) = %d, want 1000000（账号级压全局）", got)
	}
	// ③ 全局值没设 → 落到账号档自报；账号档也没有 → 0（不 panic）。
	noGlobal := WindowDeps{AccountFallback: fallback}
	if got := overlay.Effective("agent-1", noGlobal); got != 200_000 {
		t.Fatalf("Effective(agent-1) = %d, want 200000（回落账号档）", got)
	}
	if got := overlay.Effective("unknown", noGlobal); got != 0 {
		t.Fatalf("Effective(unknown) = %d, want 0", got)
	}
	if got := overlay.Effective("agent-1", WindowDeps{}); got != 0 {
		t.Fatalf("没有账号档读数的 Effective = %d, want 0（nil 端口不 panic）", got)
	}
}

// TestWindowOverlaySnapshotIsCopy 钉住值拷贝语义：调用方拿到的 map 改不动运行时那一份，
// 且并发读写不产生数据竞争（本用例供 `-race` 使用）。
func TestWindowOverlaySnapshotIsCopy(t *testing.T) {
	overlay := NewWindowOverlay(map[string]int{"agent-1": 600_000})
	snapshot := overlay.Snapshot()
	snapshot["agent-1"] = 1
	snapshot["agent-2"] = 2
	if got := overlay.For("agent-1"); got != 600_000 {
		t.Fatalf("改拷贝后运行时表被改动：%d", got)
	}
	if got := overlay.For("agent-2"); got != 0 {
		t.Fatalf("拷贝里新增的键漏进运行时表：%d", got)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			overlay.Set("agent-1", 600_000+i)
			_ = overlay.Snapshot()
			_ = overlay.For("agent-1")
			_ = overlay.Effective("agent-1", WindowDeps{Global: 400_000})
		}
	}()
	for i := 0; i < 200; i++ {
		_ = overlay.Snapshot()
		_ = overlay.For("agent-1")
	}
	<-done
}

// TestWindowOverlayNilReceiver 钉住 nil 接收者的语义（部分装配的 Runtime 会走到这里）：
// 读返回零值，写是空操作，绝不 panic。
func TestWindowOverlayNilReceiver(t *testing.T) {
	var overlay *WindowOverlay
	overlay.Set("agent-1", 600_000)
	if got := overlay.For("agent-1"); got != 0 {
		t.Fatalf("nil 接收者 For = %d, want 0", got)
	}
	if got := overlay.Snapshot(); got != nil {
		t.Fatalf("nil 接收者 Snapshot = %v, want nil", got)
	}
	// nil 覆盖层 = "没设过任何覆盖"，判定照常往下走（全局值 → 账号档），不是"窗口为 0"。
	if got := overlay.Effective("agent-1", WindowDeps{Global: 400_000}); got != 400_000 {
		t.Fatalf("nil 接收者 Effective = %d, want 400000（覆盖层空，走全局值）", got)
	}
	if got := overlay.Effective("agent-1", WindowDeps{}); got != 0 {
		t.Fatalf("nil 接收者 + 无全局值 Effective = %d, want 0", got)
	}
}
