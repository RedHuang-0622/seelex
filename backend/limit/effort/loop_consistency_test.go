package effort

import (
	"testing"

	"github.com/RedHuang-0622/seelex/backend/limit/loop"
)

// TestEffortLoopReadingsComeFromSingleJudgment 是跨实现一致性用例：档位策略表
// 与循环判据表必须覆盖同一批档位，且每条策略的循环读数就是从判据层取的那一份。
//
// 它挡的是"判据又漂成两份"：谁在 effort 里重新抄一份数字（哪怕是同样的值），
// 只要日后只改了其中一处，这条用例就会红。数字本身的唯一期望值钉在
// `backend/limit/loop/loop_test.go`。
func TestEffortLoopReadingsComeFromSingleJudgment(t *testing.T) {
	levels := loop.Levels()
	if len(levels) != len(profiles) {
		t.Fatalf("档位数量不一致：loop=%v, effort=%d 条", levels, len(profiles))
	}
	for _, level := range levels {
		profile, ok := For(level)
		if !ok {
			t.Fatalf("档位表缺档位 %q（loop 有、effort 没有 = 档位表拆家）", level)
		}
		canonical, ok := loop.For(level)
		if !ok {
			t.Fatalf("判据层缺档位 %q（effort 有、loop 没有）", level)
		}
		if profile.Loops != canonical {
			t.Errorf("档位 %q 的循环读数不是从判据层取的：effort 侧 %+v ≠ 判据层 %+v",
				level, profile.Loops, canonical)
		}
	}
}
