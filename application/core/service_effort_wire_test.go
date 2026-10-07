package core

import (
	"context"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// effortWireRuntime 是 contract.ReasoningEffortPort 的记录桩：只记下"档位变化时
// 推过来的 wire 思考强度"。用它钉住的是**接线**（SwitchEffort 是不是唯一入口、
// 映射出来的值是不是 provider 词表值、非法档位是不是一个字节都不推）；
// 账号侧的判定（只改"跟随会话"的账号）由 seelebridge 的真实账号池用例钉住。
type effortWireRuntime struct {
	*fakeRuntime
	mu     sync.Mutex
	pushed []string
}

func (runtime *effortWireRuntime) SetSessionReasoningEffort(effort string) int {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.pushed = append(runtime.pushed, effort)
	return 1
}

func (runtime *effortWireRuntime) pushedEfforts() []string {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return append([]string(nil), runtime.pushed...)
}

func newEffortWireService(t *testing.T) (*Service, *effortWireRuntime) {
	t.Helper()
	runtime := &effortWireRuntime{fakeRuntime: &fakeRuntime{}}
	return newTestService(t, &fakeEngine{}, withTestRuntime(runtime)), runtime
}

// TestSwitchEffortPushesWireReasoningEffort：档位变化必须把该档对应的 wire 思考
// 强度推给账号池，且**恰好推一次**——档位 → provider 词表的映射只有一份实现
// （application/prompt 的 effortProfiles），这里钉住它确实被接到了下发点上。
func TestSwitchEffortPushesWireReasoningEffort(t *testing.T) {
	cases := []struct {
		name      string
		level     string
		wantLevel string
		wantPush  string
	}{
		{name: "lite 档 → low", level: "lite", wantLevel: "lite", wantPush: dto.ReasoningEffortLow},
		{name: "medium 档 → medium", level: "medium", wantLevel: "medium", wantPush: dto.ReasoningEffortMedium},
		{name: "high 档 → high", level: "high", wantLevel: "high", wantPush: dto.ReasoningEffortHigh},
		{name: "max 档 → max", level: "max", wantLevel: "max", wantPush: dto.ReasoningEffortMax},
		{name: "档位名按既有口径归一（大小写与空白）", level: "  MAX ", wantLevel: "max", wantPush: dto.ReasoningEffortMax},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service, runtime := newEffortWireService(t)
			if err := service.SwitchEffort(context.Background(), testCase.level); err != nil {
				t.Fatalf("SwitchEffort(%q): %v", testCase.level, err)
			}
			pushed := runtime.pushedEfforts()
			if len(pushed) != 1 || pushed[0] != testCase.wantPush {
				t.Fatalf("pushed = %v, want exactly [%q]", pushed, testCase.wantPush)
			}
			// 两半条链同源：下发的那次读的就是这次 Apply 之后的档位（不是切换前的）。
			if got := service.effortManager.Current(); got != testCase.wantLevel {
				t.Fatalf("effort current = %q, want %q", got, testCase.wantLevel)
			}
		})
	}
}

// TestSwitchEffortCyclePushesNextLevel：cycle 入口（GUI 控件/TUI 快捷键的循环切档）
// 与显式档位走同一条链，不得绕过下发。默认档是 high，循环一次应到 max。
func TestSwitchEffortCyclePushesNextLevel(t *testing.T) {
	service, runtime := newEffortWireService(t)
	if err := service.SwitchEffort(context.Background(), "cycle"); err != nil {
		t.Fatalf("SwitchEffort(cycle): %v", err)
	}
	pushed := runtime.pushedEfforts()
	if len(pushed) != 1 || pushed[0] != dto.ReasoningEffortMax {
		t.Fatalf("pushed = %v, want [%q]", pushed, dto.ReasoningEffortMax)
	}
}

// TestSwitchEffortUnknownLevelDoesNotPush：不认识的档位在 Apply 阶段就被拒，账号池
// 一个字节都不该动——否则会出现"档位没换成、思考强度先变了"的半截状态。
func TestSwitchEffortUnknownLevelDoesNotPush(t *testing.T) {
	service, runtime := newEffortWireService(t)
	if err := service.SwitchEffort(context.Background(), "turbo"); err == nil {
		t.Fatal("SwitchEffort(turbo) 应当被拒")
	}
	if pushed := runtime.pushedEfforts(); len(pushed) != 0 {
		t.Fatalf("pushed = %v, want none", pushed)
	}
	if got := service.effortManager.Current(); got != "high" {
		t.Fatalf("effort = %q, want 拒绝后保持 high", got)
	}
}
