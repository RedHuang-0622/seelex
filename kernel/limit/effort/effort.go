// Package effort 是**档位策略的唯一判据处**：每个 effort 档位对应哪条
// reasoning 强度、哪组循环读数、哪份 plan 约束，以及"未声明时用哪一档"。
//
// 它只依赖 stdlib + `kernel/limit/loop` + `contract/dto`——档位数字与策略
// 属于 limit 板块，不许回头依赖 `application/*`（prompt 管理器、引擎都是消费方）。
package effort

import (
	"strings"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/kernel/limit/loop"
)

// Profile 是一个档位的全部策略读数：循环数字、plan 约束、reasoning 强度。
//
// 一条档位一条记录：新增档位时不会只拿到其中一半。提示词不在这里——四个档位
// 共用同一份 assets/effort/system.md，档位差异只由本结构表达。
type Profile struct {
	Level           string
	Loops           loop.Profile
	SubagentPolicy  dto.SubagentPolicy
	ReasoningEffort string
}

// profiles 是档位策略的唯一表。
var profiles = map[string]Profile{
	"lite": {
		Level:           "lite",
		Loops:           mustLoopProfile("lite"),
		SubagentPolicy:  dto.SubagentPolicy{Effort: "lite"},
		ReasoningEffort: "low",
	},
	"medium": {
		Level: "medium",
		// MaxForkConcurrency 有意保持 0：subagent 只是角色 + 模型供应商。
		SubagentPolicy:  dto.SubagentPolicy{Effort: "medium", MaxNodes: 4, RequireSerial: true},
		ReasoningEffort: "medium",
		Loops:           mustLoopProfile("medium"),
	},
	"high": {
		Level:           "high",
		SubagentPolicy:  dto.SubagentPolicy{Effort: "high", MaxNodeLoops: 48},
		ReasoningEffort: "high",
		Loops:           mustLoopProfile("high"),
	},
	"max": {
		Level:           "max",
		SubagentPolicy:  dto.SubagentPolicy{Effort: "max", MaxNodeLoops: 96},
		ReasoningEffort: "max",
		Loops:           mustLoopProfile("max"),
	},
}

// OrderedLevels 是档位循环顺序（拉条 / Cycle 用）。
var OrderedLevels = []string{"lite", "medium", "high", "max"}

// DefaultLevel 是进程默认档位：新会话与"未声明执行参数"的缺省来源。
const DefaultLevel = "high"

func mustLoopProfile(level string) loop.Profile {
	profile, ok := loop.For(level)
	if !ok {
		panic("effort: level " + level + " has no loop profile in kernel/limit/loop")
	}
	return profile
}

// For 返回档位策略；未识别返回 false（不猜档位）。
func For(level string) (Profile, bool) {
	profile, ok := profiles[strings.ToLower(strings.TrimSpace(level))]
	return profile, ok
}

// ReasoningEffortFor 返回 provider wire 词表里的思考强度；未识别返回空串
// （空值不上 wire，由 provider 用自身默认——绝不猜一个值）。
func ReasoningEffortFor(level string) string {
	profile, ok := For(level)
	if !ok {
		return ""
	}
	return profile.ReasoningEffort
}

// SubagentPolicyFor 返回子代理执行的硬约束；未识别兜底 lite（迁移前口径）。
func SubagentPolicyFor(level string) dto.SubagentPolicy {
	profile, ok := For(level)
	if !ok {
		profile = profiles["lite"]
	}
	return profile.SubagentPolicy
}

// Levels 返回全部已登记档位（顺序不保证；需要顺序用 OrderedLevels）。
func Levels() []string {
	levels := make([]string, 0, len(profiles))
	for level := range profiles {
		levels = append(levels, level)
	}
	return levels
}

// DefaultRunParams 把一次定时任务的执行参数落成显式值：声明优先，未声明
// （空串 / 0）落默认档位读数。只有 0 算未声明；负数是非法值，留给校验层
// 显式拒绝（在这里抹成默认等于把错误数据当没看见）。
func DefaultRunParams(reasoningEffort string, loopLimit int) (string, int) {
	wire := strings.ToLower(strings.TrimSpace(reasoningEffort))
	if wire == "" {
		wire = ReasoningEffortFor(DefaultLevel)
	}
	loops := loopLimit
	if loops == 0 {
		loops = loop.MaxLoops(DefaultLevel)
	}
	return wire, loops
}
