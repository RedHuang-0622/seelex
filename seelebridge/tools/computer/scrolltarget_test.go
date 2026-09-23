package computer

import "testing"

// ── 可滚动面板的纯函数单测（平台无关）──────────────────────

func TestScrollAxisStateDerivations(t *testing.T) {
	cases := []struct {
		name    string
		axis    ScrollAxis
		known   bool
		atStart bool
		atEnd   bool
	}{
		{"不可滚动不是顶部", ScrollAxis{Scrollable: false, Percent: UnknownScrollValue}, false, false, false},
		{"顶部", ScrollAxis{Scrollable: true, Percent: 0}, true, true, false},
		{"底部", ScrollAxis{Scrollable: true, Percent: 100}, true, false, true},
		{"中段", ScrollAxis{Scrollable: true, Percent: 42.5}, true, false, false},
		{"可滚动但无位置", ScrollAxis{Scrollable: true, Percent: UnknownScrollValue}, false, false, false},
	}
	for _, testCase := range cases {
		if got := testCase.axis.Known(); got != testCase.known {
			t.Fatalf("%s: Known = %v, want %v", testCase.name, got, testCase.known)
		}
		if got := testCase.axis.AtStart(); got != testCase.atStart {
			t.Fatalf("%s: AtStart = %v, want %v", testCase.name, got, testCase.atStart)
		}
		if got := testCase.axis.AtEnd(); got != testCase.atEnd {
			t.Fatalf("%s: AtEnd = %v, want %v", testCase.name, got, testCase.atEnd)
		}
	}
}

func TestNormalizeScrollTargetsFiltersSortsAndLimits(t *testing.T) {
	targets := []ScrollTarget{
		{ // 两轴都不可滚动：不是滚轮目标
			Name: "static", Rect: Rect{X: 0, Y: 0, Width: 900, Height: 900},
		},
		{ // 零面积：过不了面积判据
			Name: "zero", Rect: Rect{X: 0, Y: 0},
			Vertical: ScrollAxis{Scrollable: true, Percent: 0},
		},
		{
			Name: "small", Rect: Rect{X: 10, Y: 10, Width: 100, Height: 100},
			Vertical: ScrollAxis{Scrollable: true, Percent: 0},
		},
		{
			Name: "big", Rect: Rect{X: 0, Y: 0, Width: 800, Height: 600},
			Vertical: ScrollAxis{Scrollable: true, Percent: 100},
		},
		{
			Name: "horizontal-only", Rect: Rect{X: 0, Y: 0, Width: 300, Height: 200},
			Horizontal: ScrollAxis{Scrollable: true, Percent: 20},
		},
	}
	got := NormalizeScrollTargets(targets, 0)
	want := []string{"big", "horizontal-only", "small"}
	if len(got) != len(want) {
		t.Fatalf("过滤后 = %d 条, want %d（%v）", len(got), len(want), namesOf(got))
	}
	for index, name := range want {
		if got[index].Name != name {
			t.Fatalf("排序 = %v, want %v（外层大面板在前）", namesOf(got), want)
		}
	}

	if limited := NormalizeScrollTargets(targets, 2); len(limited) != 2 || limited[0].Name != "big" {
		t.Fatalf("limit=2 结果 = %v", namesOf(limited))
	}
	if capped := NormalizeScrollTargets(targets, maxScrollTargetLimit+50); len(capped) != 3 {
		t.Fatalf("超上限 limit 仍应返回全部有效面板，得到 %v", namesOf(capped))
	}
}

func TestPickScrollTargetAtPointPrefersInnermost(t *testing.T) {
	outer := ScrollTarget{
		Name: "page", Rect: Rect{X: 0, Y: 0, Width: 1000, Height: 800},
		Vertical: ScrollAxis{Scrollable: true, Percent: 10},
	}
	inner := ScrollTarget{
		Name: "sidebar", Rect: Rect{X: 600, Y: 100, Width: 300, Height: 500},
		Vertical: ScrollAxis{Scrollable: true, Percent: 50},
	}
	targets := []ScrollTarget{outer, inner}

	if got, ok := PickScrollTargetAtPoint(targets, Point{X: 700, Y: 200}); !ok || got.Name != "sidebar" {
		t.Fatalf("重叠区域应命中最内层面板，得到 %+v ok=%v", got, ok)
	}
	if got, ok := PickScrollTargetAtPoint(targets, Point{X: 100, Y: 100}); !ok || got.Name != "page" {
		t.Fatalf("外层面板区域应命中 page，得到 %+v ok=%v", got, ok)
	}
	if _, ok := PickScrollTargetAtPoint(targets, Point{X: 5000, Y: 5000}); ok {
		t.Fatal("面板之外的点不应命中")
	}
}

func TestLargestVerticalScrollTarget(t *testing.T) {
	targets := []ScrollTarget{
		{
			Name: "side", Rect: Rect{X: 0, Y: 0, Width: 200, Height: 400},
			Vertical: ScrollAxis{Scrollable: true},
		},
		{
			Name: "body", Rect: Rect{X: 0, Y: 0, Width: 900, Height: 700},
			Vertical: ScrollAxis{Scrollable: true},
		},
		{
			Name: "horizontal", Rect: Rect{X: 0, Y: 0, Width: 2000, Height: 2000},
			Horizontal: ScrollAxis{Scrollable: true},
		},
	}
	got, ok := LargestVerticalScrollTarget(targets)
	if !ok || got.Name != "body" {
		t.Fatalf("应挑最大的纵向可滚动面板，得到 %+v ok=%v", got, ok)
	}
	if _, ok := LargestVerticalScrollTarget([]ScrollTarget{targets[2]}); ok {
		t.Fatal("只有横向可滚动时不应挑出纵向面板")
	}
}

func TestScrollNotchesSplitPerWheelDelta(t *testing.T) {
	cases := map[int][]int{
		0:     nil,
		120:   {120},
		-120:  {-120},
		600:   {120, 120, 120, 120, 120},
		-360:  {-120, -120, -120},
		250:   {120, 120, 10},
		-10:   {-10},
		12000: {120, 120, 120, 120, 120, 120, 120, 120, 120, 120},
	}
	for delta, want := range cases {
		got := scrollNotches(delta)
		if len(got) != len(want) {
			t.Fatalf("scrollNotches(%d) = %v, want %v", delta, got, want)
		}
		sum := 0
		for index, notch := range got {
			if notch != want[index] {
				t.Fatalf("scrollNotches(%d)[%d] = %d, want %d", delta, index, notch, want[index])
			}
			sum += notch
		}
		// 拆格不改变总量（上限截断除外：12000 被 maxWheelNotches 截到 1200）。
		if delta != 12000 && sum != delta {
			t.Fatalf("scrollNotches(%d) 总量 = %d, want %d", delta, sum, delta)
		}
	}
}

func namesOf(targets []ScrollTarget) []string {
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, target.Name)
	}
	return names
}
