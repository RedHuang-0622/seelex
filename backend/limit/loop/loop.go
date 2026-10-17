// Package loop 是 ReAct 循环上限的**唯一判据处**：档位数字、工具预算、
// "会话钉住值 vs 进程默认值"的解析，以及子代理声明的上限校验，都只在这里各有一份。
//
// 生态位与边界见 README.md。本包只依赖标准库——它是判据层，不许回头依赖
// 上层（`application/*`、`seelebridge/*`、`seelexctx`）：消费方反过来依赖它。
package loop

import (
	"fmt"
	"strings"
)

// Budget 限定一次用户请求的执行预算。
//
// MaxToolRounds 计"发生过工具调用的已完成 ReAct 轮"，MaxToolCalls 计单次工具调用，
// MaxNoProgressRounds 计"连续没有进展"的轮数。
type Budget struct {
	MaxToolRounds       int
	MaxToolCalls        int
	MaxNoProgressRounds int
}

// Profile 是一个档位在循环维度上的读数：这一档能跑多少轮、以及它的工具预算。
//
// 档位表**只有这一份**：`application/prompt` 的 effortProfile 通过 For 取值，
// 不再自己抄一遍数字（抄一遍就会出现"改了 effort 表、拉条读数没变"的漂移）。
type Profile struct {
	// Level 是档位名（lite/medium/high/max），与 effort 档位词表同一份。
	Level string
	// MaxLoops 是引擎循环上限（engine.SetMaxLoops 的入参）。
	MaxLoops int
	// Budget 是该档位的 ReAct 工具预算。
	Budget Budget
}

// profiles 是档位→循环读数的唯一数字表。
var profiles = map[string]Profile{
	"lite": {
		Level:    "lite",
		MaxLoops: 15,
		Budget:   Budget{MaxToolRounds: 15, MaxToolCalls: 30, MaxNoProgressRounds: 6},
	},
	"medium": {
		Level:    "medium",
		MaxLoops: 48,
		Budget:   Budget{MaxToolRounds: 48, MaxToolCalls: 96, MaxNoProgressRounds: 10},
	},
	"high": {
		Level:    "high",
		MaxLoops: 384,
		Budget:   Budget{MaxToolRounds: 384, MaxToolCalls: 768, MaxNoProgressRounds: 24},
	},
	"max": {
		Level:    "max",
		MaxLoops: 768,
		Budget:   Budget{MaxToolRounds: 768, MaxToolCalls: 1536, MaxNoProgressRounds: 48},
	},
}

// normalize 是档位名的解析口径（大小写与首尾空白不敏感），与 effort 侧一致。
func normalize(level string) string {
	return strings.ToLower(strings.TrimSpace(level))
}

// For 返回档位的循环读数；未识别的档位返回 false（调用方自己决定兜底）。
func For(level string) (Profile, bool) {
	profile, ok := profiles[normalize(level)]
	return profile, ok
}

// MaxLoops 返回档位的循环上限；未识别的档位返回 0（不猜档位）。
func MaxLoops(level string) int {
	if profile, ok := For(level); ok {
		return profile.MaxLoops
	}
	return 0
}

// BudgetFor 返回档位的工具预算；未识别的档位兜底到 lite（与迁移前一致）。
func BudgetFor(level string) Budget {
	if profile, ok := For(level); ok {
		return profile.Budget
	}
	return profiles["lite"].Budget
}

// Levels 返回全部已登记的档位名。
//
// 顺序不保证（与迁移前的 `prompt.ValidEffortLevels` 同款：那里也是遍历 map）。
// 需要顺序的调用方请用 `application/prompt` 的 Cycle 顺序表。
func Levels() []string {
	levels := make([]string, 0, len(profiles))
	for level := range profiles {
		levels = append(levels, level)
	}
	return levels
}

// Effective 解析"这次用多少轮"：会话钉住值优先，否则回退进程默认值。
//
// 钉住值 <= 0 表示"这一格没被钉住"（撤掉这一格），不是"零轮"——这是既有语义，
// 见 `contract.SessionLoopLimitPort` 与 wiring/adapters 的会话钉住实现。
func Effective(pinned, fallback int) int {
	if pinned > 0 {
		return pinned
	}
	return fallback
}

// ValidateSubagentMaxLoops 判一条子代理自己声明的循环次数能不能用。
//
// 判据只有**进程级循环天花板**（limits.input_loop_limit）。effort 档位的节点上限
// （SubagentPolicy.MaxNodeLoops）在这里**不出现**——它是主代理历史 Plan 节点的判据，
// 拿来卡子代理就等于"设一下 reasoning effort，子代理的次数跟着变"（2026-10-08 口径）。
// 0 = 没声明（不是"零轮"），留给本批默认；负数才是非法。
//
// 错误文案是用户可见面的既有事实，逐字保留（含 `must be >= 1` 这条对负数说的口径）。
func ValidateSubagentMaxLoops(id string, maxLoops, ceiling int) error {
	if maxLoops < 0 {
		return fmt.Errorf("fork_subagents: subagent %q max_loops must be >= 1 (got %d)", id, maxLoops)
	}
	if ceiling > 0 && maxLoops > ceiling {
		return fmt.Errorf("fork_subagents: subagent %q max_loops=%d exceeds the process loop ceiling %d", id, maxLoops, ceiling)
	}
	return nil
}
