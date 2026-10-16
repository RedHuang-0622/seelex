package loop

import "testing"

// TestTierTableExactValues 钉住档位数字：这是**唯一**一份，改了就必须改这里的期望。
//
// 期望值取自迁移前的 `application/prompt/effortProfiles`（迁移是判据归一，不是调参）。
func TestTierTableExactValues(t *testing.T) {
	want := map[string]struct {
		loops  int
		rounds int
		calls  int
		stalls int
	}{
		"lite":   {loops: 15, rounds: 15, calls: 30, stalls: 6},
		"medium": {loops: 48, rounds: 48, calls: 96, stalls: 10},
		"high":   {loops: 384, rounds: 384, calls: 768, stalls: 24},
		"max":    {loops: 768, rounds: 768, calls: 1536, stalls: 48},
	}
	if len(want) != len(Levels()) {
		t.Fatalf("档位数量变了：Levels() = %v，期望 %d 档", Levels(), len(want))
	}
	for level, expected := range want {
		profile, ok := For(level)
		if !ok {
			t.Fatalf("档位 %q 不存在", level)
		}
		if profile.MaxLoops != expected.loops {
			t.Errorf("%s MaxLoops = %d, want %d", level, profile.MaxLoops, expected.loops)
		}
		if profile.Budget.MaxToolRounds != expected.rounds ||
			profile.Budget.MaxToolCalls != expected.calls ||
			profile.Budget.MaxNoProgressRounds != expected.stalls {
			t.Errorf("%s Budget = %+v, want rounds=%d calls=%d stalls=%d",
				level, profile.Budget, expected.rounds, expected.calls, expected.stalls)
		}
		if MaxLoops(level) != profile.MaxLoops || BudgetFor(level) != profile.Budget {
			t.Errorf("%s 的两个入口读数不一致：MaxLoops=%d BudgetFor=%+v", level, MaxLoops(level), BudgetFor(level))
		}
	}
}

// TestUnknownLevelDoesNotGuess：未识别档位的两个既有口径必须保住——
// `MaxLoops` 返回 0（不猜档位），`BudgetFor` 兜底 lite（迁移前的行为）。
func TestUnknownLevelDoesNotGuess(t *testing.T) {
	if got := MaxLoops("nope"); got != 0 {
		t.Errorf("MaxLoops(未识别) = %d, want 0", got)
	}
	if got, want := BudgetFor("nope"), profiles["lite"].Budget; got != want {
		t.Errorf("BudgetFor(未识别) = %+v, want lite 的 %+v", got, want)
	}
}

// TestLevelNameNormalization：档位名的解析口径（大小写与首尾空白不敏感）与 effort 侧一致。
func TestLevelNameNormalization(t *testing.T) {
	if got := MaxLoops(" HIGH "); got != 384 {
		t.Errorf("MaxLoops(\" HIGH \") = %d, want 384", got)
	}
}

// TestEffectiveResolution 钉住"这次用多少轮"的解析：会话钉住值优先，否则进程默认值；
// 钉住值 <= 0 表示"这一格没被钉住"（撤掉这一格），不是零轮。
func TestEffectiveResolution(t *testing.T) {
	cases := []struct {
		name     string
		pinned   int
		fallback int
		want     int
	}{
		{"钉住值生效", 20, 384, 20},
		{"钉住值为 0 回退默认", 0, 384, 384},
		{"钉住值为负回退默认", -1, 384, 384},
		{"两者都为 0（未配置）", 0, 0, 0},
		{"默认值为 0 时钉住值仍生效", 7, 0, 7},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := Effective(test.pinned, test.fallback); got != test.want {
				t.Fatalf("Effective(%d, %d) = %d, want %d", test.pinned, test.fallback, got, test.want)
			}
		})
	}
}

// TestValidateSubagentMaxLoopsUsesProcessCeiling：循环声明的判据只有**进程级天花板**。
//
// 三条钉子（与迁移前 `seelebridge/fork` 的同名用例逐条对应）：
//   - 越过天花板（9999）与负数在入口就被拒；
//   - 比 effort 档位节点上限（high=48）大的 60 **可以**——档位不是工作单元次数的
//     判据（2026-10-08 口径：设置 reasoning effort 不得污染 subagent/teammate 的循环次数）；
//   - 0 = 没声明，留给本批默认，不算错。
func TestValidateSubagentMaxLoopsUsesProcessCeiling(t *testing.T) {
	const ceiling = 9999
	if err := ValidateSubagentMaxLoops("s1", ceiling+1, ceiling); err == nil {
		t.Fatalf("越过进程级循环天花板（%d）的 max_loops 必须在入口被拒", ceiling)
	}
	if err := ValidateSubagentMaxLoops("s1", -1, ceiling); err == nil {
		t.Fatal("负数 max_loops 必须在入口被拒")
	}
	if err := ValidateSubagentMaxLoops("s1", 60, ceiling); err != nil {
		t.Fatalf("effort 档位的节点上限（high=48）不得再拦工作单元自己的声明：%v", err)
	}
	if err := ValidateSubagentMaxLoops("s1", 0, ceiling); err != nil {
		t.Fatalf("没声明（0）不是错误，留给本批默认：%v", err)
	}
	// 天花板未配置（0 = 不限）时不判上限：配置层没给数就不许自造一个。
	if err := ValidateSubagentMaxLoops("s1", 100000, 0); err != nil {
		t.Fatalf("未配置天花板时不应判上限：%v", err)
	}
	// 等于天花板可以（判据是"> 天花板"，不是">="）。
	if err := ValidateSubagentMaxLoops("s1", ceiling, ceiling); err != nil {
		t.Fatalf("等于天花板不该被拒：%v", err)
	}
}

// TestValidateSubagentMaxLoopsMessagesUnchanged：错误文案是用户可见面的既有事实，
// 判据搬家不得改字（含 `must be >= 1` 这条对负数说的口径）。
func TestValidateSubagentMaxLoopsMessagesUnchanged(t *testing.T) {
	if got, want := ValidateSubagentMaxLoops("read", -1, 9999).Error(),
		`fork_subagents: subagent "read" max_loops must be >= 1 (got -1)`; got != want {
		t.Errorf("负数文案变了：\n got: %s\nwant: %s", got, want)
	}
	if got, want := ValidateSubagentMaxLoops("read", 10000, 9999).Error(),
		`fork_subagents: subagent "read" max_loops=10000 exceeds the process loop ceiling 9999`; got != want {
		t.Errorf("越天花板文案变了：\n got: %s\nwant: %s", got, want)
	}
}
