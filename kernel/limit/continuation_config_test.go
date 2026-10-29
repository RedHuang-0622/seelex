package limit

import (
	"path/filepath"
	"testing"
)

// 两条口径必须分开钉住（与 async_exec 同一纪律）：
//   - 出厂配置（config/seelex.yaml）= 自动续跑常驻开（否则整套功能包在盒里）；
//   - 结构零值 / 配置里缺这段 = 关（回滚臂与旧配置文件兼容的根据）。
func TestContinuationShippedConfigOn(t *testing.T) {
	loaded, err := LoadLimits(filepath.Join("..", "..", "config", "seelex.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.WithDefaults().Continuation.On() {
		t.Fatal("出厂配置必须显式打开 limits.continuation.enabled（接线完成后不留成关的默认档）")
	}
	if attempts := loaded.WithDefaults().Continuation.Attempts(); attempts <= 0 {
		t.Fatalf("出厂配置的续跑上限无效：%d", attempts)
	}
}

func TestContinuationShippedZeroValueKeepsRollbackArm(t *testing.T) {
	if limits := (Limits{}).WithDefaults(); limits.Continuation.On() {
		t.Fatal("结构零值必须保持关闭：缺这段配置的旧文件不得被悄悄打开续跑")
	}
}

func TestContinuationNegativeValuesFailLoudly(t *testing.T) {
	for name, mutate := range map[string]func(*ContinuationLimits){
		"max_attempts_per_goal": func(limits *ContinuationLimits) { limits.MaxAttemptsPerGoal = -1 },
		"backoff_sec":           func(limits *ContinuationLimits) { limits.BackoffSec = -1 },
		"max_backoff_sec":       func(limits *ContinuationLimits) { limits.MaxBackoffSec = -1 },
	} {
		limits := ContinuationLimits{Enabled: true}
		mutate(&limits)
		if err := limits.Validate(); err == nil {
			t.Fatalf("%s 为负值必须显式报错（不静默降级成从不续跑）", name)
		}
	}
}
