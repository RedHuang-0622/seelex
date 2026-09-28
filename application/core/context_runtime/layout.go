package context_runtime

import (
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

// 本文件把《上下文前缀链路 · 压缩四区模型》的四区显式化，并把与折叠有关的决策
// 收敛成**同一份**结构：判据（软硬阈值、保留窗口）与报表（门禁 Detail、帧正文的
// 四区区块）都读 ContextLayout，不再各算一份。此前的教训正是"报表口径与判据口径
// 分开演化"：界面上显示"超了硬线"而实际什么都没发生，两处数字对不上。

// 四区 kind（协议字面量；前端文案在 GUI 侧，同一处只放一种语言）。
const (
	// ZoneStable 是 ① 绝不压的前缀：system 稳定层（identity/plugin/effort/
	// instructions）+ 插件级 skill 目录 + 稳定栈块（compact 栈顶帧摘要）。
	ZoneStable = "stable_prefix"
	// ZoneFolded 是 ② 被压掉的上下文：折出保留窗口的完整协议单元 → 折成
	// 有界 checkpoint 帧（wire 上只留帧标记块）。
	ZoneFolded = "folded"
	// ZoneProtected 是 ③ 窗口保护的上下文：保留窗口内的完整协议单元，
	// 整条 message 不切。
	ZoneProtected = "protected_window"
	// ZoneTail 是 ④ 绝不压的后缀：plan 上下文消息（含 plan 政策段）。
	ZoneTail = "tail"
	// ZoneInput 是当轮输入（属于 ④ 的一部分，单列以便与 ④ 的常驻部分区分）。
	ZoneInput = "current_input"
)

// ContextZone 是四区模型里的一个分区：token 数 + 消息条数 + 来源。三个字段必须
// 同时给出——只报 token 看不出"这一段是什么"，只报来源又解释不了预算去哪了。
type ContextZone struct {
	Kind     string `json:"kind"`
	Tokens   int    `json:"tokens"`
	Messages int    `json:"messages"`
	Source   string `json:"source"`
}

// RetainDecision 是保留窗口（③ 的边界）的一次决策事实（《待落地》1 显式化）。
//
// 判定公式：retained = min(clamp(min(token1, token2), floor, token1), target)
//
//	token1 = 配置里的保留上限 window.retain_tokens（未配置 → 账号上下文窗口）
//	token2 = window.ratio × 全量上下文
//	floor  = max(最近 1 个完整协议单元, limits.context_retain_floor_percent × 预算)
//	target = limits.context_target_percent × 预算（0 = 未配置）
//
// 「最近 1 个完整协议单元」这条兜底在装配层（TranscriptTailWindow 单元不可拆分），
// 本结构只记比例部分与最终值。target 是**硬上限**：保留规则/下限允许保留更多
// 时也必须收口，给下一轮留出 soft − target 的确定余量；否则保留窗口加固定
// 开销贴着软线，下一轮稍增即越线。
type RetainDecision struct {
	AllContextTokens int  `json:"all_context_tokens"`
	BudgetTokens     int  `json:"budget_tokens"`
	CapTokens        int  `json:"cap_tokens"`    // token1（0 = 未配置 → 窗口兜底）
	RatioTokens      int  `json:"ratio_tokens"`  // token2
	FloorTokens      int  `json:"floor_tokens"`  // 比例下限（0 = 未配置）
	TargetTokens     int  `json:"target_tokens"` // 压缩目标（0 = 未配置）
	Retained         int  `json:"retained"`      // 最终保留区
	FloorApplied     bool `json:"floor_applied"`
	TargetApplied    bool `json:"target_applied"`
}

// ContextLayout 是一次装配的四区显式化 + 保留窗口决策 + 判据量。判据与报表读
// 同一份。帧摘要传递（item 2）与分片重放（item 3）的事实发生在 seelexctx 的帧侧
// （见 CarryDiagnostics / ReplayChunkPlan），本结构不代为转述——打点必须落在
// 事实发生的那一层，否则又多一处需要同步的副本。
type ContextLayout struct {
	Zones []ContextZone `json:"zones"`

	Retain RetainDecision `json:"retain"`

	ComparedTokens  int  `json:"compared_tokens"`
	EstimatedTokens int  `json:"estimated_tokens"`
	SoftThreshold   int  `json:"soft_threshold"`
	HardThreshold   int  `json:"hard_threshold"`
	Compacting      bool `json:"compacting"`
}

// zone 返回指定 kind 的分区（缺失 → 只带 kind 的零值）。
func (l ContextLayout) zone(kind string) ContextZone {
	for _, item := range l.Zones {
		if item.Kind == kind {
			return item
		}
	}
	return ContextZone{Kind: kind}
}

// Terse 渲染保留窗口决策的一行事实（门禁 Detail 用：短、无文案）。
func (r RetainDecision) Terse() string {
	return fmt.Sprintf("all=%d budget=%d cap=%d ratio=%d floor=%d target=%d retained=%d floor_applied=%t target_applied=%t",
		r.AllContextTokens, r.BudgetTokens, r.CapTokens, r.RatioTokens,
		r.FloorTokens, r.TargetTokens, r.Retained, r.FloorApplied, r.TargetApplied)
}

// RetainTerse 渲染保留窗口决策的一行事实。
func (l ContextLayout) RetainTerse() string { return l.Retain.Terse() }

// ZonesTerse 渲染四区 token 数的一行事实（门禁 Detail 用）。
func (l ContextLayout) ZonesTerse() string {
	parts := make([]string, 0, len(l.Zones))
	for _, zone := range l.Zones {
		parts = append(parts, fmt.Sprintf("%s=%d", zone.Kind, zone.Tokens))
	}
	return strings.Join(parts, " ")
}

// buildContextLayout 把装配后的 provider 历史切进四区并汇总判据量：分区判据见
// ContextZones（纯函数），判据量与保留窗口决策由调用方在同一轮次采样写入。
func (c *Coordinator) buildContextLayout(
	systemPrompt string,
	assembled []contract.EngineMessage,
	currentInput string,
) ContextLayout {
	return ContextLayout{
		Zones: ContextZones(systemPrompt, assembled, currentInput, c.countRequestTokens),
	}
}

// countRequestTokens 适配 TaskPort.CountRequestTokens 到四区切分的计数签名。
func (c *Coordinator) countRequestTokens(
	systemPrompt string,
	history []contract.EngineMessage,
	currentInput string,
	tools []model.Tool,
) int {
	return c.tasks.CountRequestTokens(systemPrompt, history, currentInput, tools)
}

// zoneCounter 是四区切分注入的 token 计数（形状同 TaskPort.CountRequestTokens）。
type zoneCounter func(systemPrompt string, history []contract.EngineMessage, currentInput string, tools []model.Tool) int

// ContextZones 把装配后的 provider 历史切进四区并给出各区 token 数、条数与来源。
// 纯函数（计数注入），因此判据侧与报表侧用的是**同一段代码**，不是两份相似实现。
//
// 分区判据全部是**消息自身的事实**（role + 前缀标记），不做位置推算：
//
//	① stable_prefix   —— role=system 且不是 plan 尾部/自主压缩帧的消息
//	② folded          —— 压缩帧标记块（compact-context / 自主压缩帧）：被折出
//	                     窗口的上下文在 wire 上只剩这一块
//	③ protected_window—— 其余历史消息（保留窗口内的完整协议单元）
//	④ tail            —— plan 上下文消息（含政策段）
//	⑤ current_input   —— 当轮输入（含 worktable 打点块）
//
// 未折叠时 ② 为 0（没有任何内容被折出），③ 即全部累积的已定稿轮次——与
// 《压缩四区模型》"要判定的只是 ③ 从哪里开始"一致。
//
// 口径边界（不粉饰）：本层能看见的是**引擎消息数组**加引擎级 system prompt。
// 框架装配器在其下游渲染的 project / memory / compact 栈块会落到 ① 的位置，但它们
// 的 token 不进本结构（本层拿不到那几块），因此 ① 的 token 是下界；要与真实请求
// 估算对账请用 ContextLayout.EstimatedTokens（同一计数器、含全量请求形状）。
func ContextZones(
	systemPrompt string,
	assembled []contract.EngineMessage,
	currentInput string,
	count zoneCounter,
) []ContextZone {
	if count == nil {
		return nil
	}
	sources := map[string]string{
		ZoneStable:    "system 稳定层 + 插件级 skill 目录 + 稳定栈块",
		ZoneFolded:    "折出窗口的完整协议单元（wire 上只剩 checkpoint 帧标记）",
		ZoneProtected: "保留窗口内的完整协议单元",
		ZoneTail:      "plan 上下文消息（含 plan 政策段）",
		ZoneInput:     "当轮输入（含 worktable 打点块）",
	}
	buckets := map[string][]contract.EngineMessage{}
	for _, message := range assembled {
		switch {
		case strings.HasPrefix(message.Content, planContextPrefix):
			buckets[ZoneTail] = append(buckets[ZoneTail], message)
		case strings.HasPrefix(message.Content, seelexctx.CompactContextMarker),
			strings.HasPrefix(message.Content, AutonomousCompactionPrefix):
			buckets[ZoneFolded] = append(buckets[ZoneFolded], message)
		case message.Role == "system":
			buckets[ZoneStable] = append(buckets[ZoneStable], message)
		default:
			buckets[ZoneProtected] = append(buckets[ZoneProtected], message)
		}
	}
	// 引擎级 system prompt（框架装配器注入，不在 assembled 的消息数组里）属于 ①：
	// 与已定稿的 system 材料合并计数。
	if strings.TrimSpace(systemPrompt) != "" {
		buckets[ZoneStable] = append([]contract.EngineMessage{{
			Role: "system", Content: systemPrompt, ContentSet: true,
		}}, buckets[ZoneStable]...)
	}
	zones := make([]ContextZone, 0, len(sources))
	for _, kind := range []string{ZoneStable, ZoneFolded, ZoneProtected, ZoneTail} {
		messages := buckets[kind]
		zones = append(zones, ContextZone{
			Kind: kind, Tokens: count("", messages, "", nil),
			Messages: len(messages), Source: sources[kind],
		})
	}
	zones = append(zones, ContextZone{
		Kind:     ZoneInput,
		Tokens:   count("", nil, currentInput, nil),
		Messages: 1, Source: sources[ZoneInput],
	})
	return zones
}

// retainWindowDecision 计算保留窗口（③ 的边界）并留下决策事实：
//
//	retained = clamp(min(token1, token2), floor, token1)
//
// floorPercent 来自 limits.context_retain_floor_percent（0 = 未配置 → 下限只剩装配层
// 「至少 1 个完整协议单元」的兜底）。floor > retain_tokens 的非法组合已在启动期显式
// 报错（application.core.ValidateRetainWindow）；这里只在运行时把下限夹进 [0, upper]
// 保证返回值单调有界，不承担校验。
func retainWindowDecision(
	config seelexctx.WindowConfig,
	allContextTokens int,
	budget task_context.ContextBudget,
	floorPercent int,
) RetainDecision {
	applied := config.WithDefaults()
	decision := RetainDecision{
		AllContextTokens: allContextTokens,
		BudgetTokens:     budget.Budget,
		CapTokens:        config.RetainTokens,
		RatioTokens:      int(float64(allContextTokens) * applied.Ratio),
		FloorTokens:      seelexctx.RetainFloorTokens(floorPercent, budget.Budget),
	}
	decision.Retained = config.RetainedContextTokensWithFloor(allContextTokens, budget.Window, decision.FloorTokens)
	// floor_applied 只在「下限真的抬高了结果」时为真：配了下限却没生效的轮次不该
	// 在报表里报成生效（否则「我明明配了下限」会变成另一个不可对账的数字）。
	decision.FloorApplied = decision.FloorTokens > 0 &&
		decision.Retained > config.RetainedContextTokens(allContextTokens, budget.Window)
	// target 是折叠后的请求落点（limits.context_target_percent），作为保留区的
	// 最终硬上限：与 soft 的差就是每次折叠留下的余量。不设上限时（0）保持旧行为。
	decision.TargetTokens = budget.TargetAfterCompaction
	if decision.TargetTokens > 0 && decision.Retained > decision.TargetTokens {
		decision.Retained = decision.TargetTokens
		decision.TargetApplied = true
	}
	return decision
}
