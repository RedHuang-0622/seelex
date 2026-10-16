package prompt

import (
	"testing"

	"github.com/RedHuang-0622/seelex/backend/limit/loop"
)

// TestEffortLoopReadingsComeFromSingleJudgment 是跨实现一致性用例：同一个档位名，
// 从 effort 入口（`prompt.MaxLoops` / `prompt.ReActBudgetFor`）与判据层入口
// （`loop.MaxLoops` / `loop.BudgetFor`）读出来的必须是**同一个判定**。
//
// 它挡的是"判据又漂成两份"：谁在 prompt 里重新抄一份数字（哪怕是同样的值），
// 只要日后只改了其中一处，这条用例就会红。数字本身的唯一期望值钉在
// `backend/limit/loop/loop_test.go`。
func TestEffortLoopReadingsComeFromSingleJudgment(t *testing.T) {
	levels := loop.Levels()
	if len(levels) != len(effortProfiles) {
		t.Fatalf("档位数量不一致：loop=%v, prompt=%d 条", levels, len(effortProfiles))
	}
	for _, level := range levels {
		if got, want := MaxLoops(level), loop.MaxLoops(level); got != want {
			t.Errorf("MaxLoops(%q)：effort 入口 %d ≠ 判据层 %d", level, got, want)
		}
		if got, want := ReActBudgetFor(level), loop.BudgetFor(level); got != want {
			t.Errorf("ReActBudgetFor(%q)：effort 入口 %+v ≠ 判据层 %+v", level, got, want)
		}
		profile, ok := effortProfileFor(level)
		if !ok {
			t.Fatalf("effortProfiles 缺档位 %q（loop 有、prompt 没有 = 档位表拆家）", level)
		}
		canonical, ok := loop.For(level)
		if !ok {
			t.Fatalf("判据层缺档位 %q（prompt 有、loop 没有）", level)
		}
		if profile.loops != canonical {
			t.Errorf("档位 %q 的循环读数不是从判据层取的：effort 侧 %+v ≠ 判据层 %+v",
				level, profile.loops, canonical)
		}
	}
}
