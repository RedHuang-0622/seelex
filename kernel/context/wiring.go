package context_runtime

// wiring.go 收纳上下文面的**判据**（P21-17「域方法随类型走」：从
// `backend/engine/assemble/runtime_context.go` 把"值那一跳"搬进本域；plan §2.5 把
// `seelebridge/runtime_context.go` 列为 `backend/engine/context/` 的目标）。
//
// 留在装配层的是**组件工厂**（把宿主的 limits / completer / 会话上下文存储接成
// `seelectx.*` 与 `seelexctx.*` 的构造参数）——那是宿主读数的接线，不是本域的判据。
// 本文件反过来只收"从宿主读数到域决策"的那一跳：
//
//	ReplayInputTokens      分片重放的片预算（配置旋钮 vs 窗口推导）
//	NewCompactionSummarizer 厚摘要器与"为什么没有摘要器"（三条 nil 出口各带理由）
//	WindowTailBudget       读尾预算（轮数回退）
//	CoverHistoryGap        真空区补压（**无压缩基线就不补**的判据）
//	RelatedMemoryBlocks    相关记忆块（候选 → 词法选取 → 渲染；无命中就不注入）

import (
	"context"
	"fmt"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/seelectx"

	seelexctx "github.com/RedHuang-0622/seelex/backend/engine/context/seelectx"
	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/memory"
	"github.com/RedHuang-0622/seelex/store"
)

// ReplayInputTokens 返回分片重放的片预算（模型输入侧）：取账号上下文窗口减去
// 固定开销后的保守值。≤0 → 不分片（走原有单次重放，保住前缀缓存）。
//
// 为什么用 ContextWindow 的保守份额而不是硬阈值：分片是"压缩区自身大到一次发不出"
// 的容灾路径，判据应当是"这一次请求能不能装下原始溢出区"，而不是"要不要现在压缩"。
// 取窗口的 3/4 给正文、留 1/4 给 system/tools/指令与估算偏差。
//
// limits.context_compaction_summary.input_tokens > 0 时以配置为准；未配置才走
// 这里的推导——旋钮的零值必须是"沿用既有行为"，否则加了它就静默改掉片预算。
func ReplayInputTokens(configuredTokens, contextWindow int) int {
	if configuredTokens > 0 {
		return configuredTokens
	}
	if contextWindow <= 0 {
		return 0
	}
	return contextWindow * 3 / 4
}

// NewCompactionSummarizer 按开关构造前缀重放厚摘要器（limits.context_compaction_summary）。
//
// 关闭、QuickChat 装配失败、摘要器构造失败三种情况一律返回 **nil**：nil 是
// chapter2Node 的显式判据（`d.opts.Summarizer != nil`），落到本地确定性压缩，
// 既不报错也不静默降级成"发一次没有缓存的调用"。QuickChat 走的是与 seelexCompressor
// 同一条构造路径（共享账号 completer 的隔离调用，无工具、独立 history），
// 不第二次装配 completer。
//
// 第二个返回值是**为什么没有摘要器**（三种 nil 出口的分别是"开关关闭 / QuickChat 装配失败 /
// 摘要器构造失败"）。三个出口此前都不留痕——windowsgui 构建下 log.Printf 也无处可看，现场只剩
// 一个 `summary_source=local`；这份 note 走进压缩 DAG 的 SummarizerNote，最终落进帧证据与帧正文，
// 让"模型为什么没被叫到"在帧里自答。
func NewCompactionSummarizer(completer agent.Completer, enabled bool) (seelexctx.PrefixReplaySummarizer, string) {
	if !enabled {
		return nil, "压缩处厚摘要开关关闭（limits.context_compaction_summary.enabled 非 true），本次不调用模型"
	}
	quickChat, err := seelectx.NewQuickChat(completer)
	if err != nil || quickChat == nil {
		return nil, fmt.Sprintf("QuickChat 装配失败（%v），本次不调用模型", err)
	}
	summarizer, err := seelexctx.NewQuickChatPrefixReplaySummarizer(quickChat)
	if err != nil {
		return nil, fmt.Sprintf("前缀重放摘要器构造失败（%v），本次不调用模型", err)
	}
	return summarizer, ""
}

// WindowTailBudget 推导主会话 DurableHistory 的读尾预算（D1，plan.md §9）。
// 它是「从磁盘读哪些分片」的宽度上限，不是压缩保留前缀的决策——保留前缀走
// WindowConfig.RetainedContextTokens 的 min(token1, token2)。
//   - tokenBudget = 账号上下文窗口：恒 ≥ 保留前缀（该函数已把结果夹在
//     [1, all_context]），所以这一维实际不约束；
//   - maxUnits = 轮数：本路径只给 ProviderContextInfo 填 ContextTokens，
//     AvgRoundTokens/Reserved 缺省 → WindowRounds 恒走"输入缺失回退"分支，
//     因此实际值 = window.rounds（显式配置时）或 window.min_rounds，
//     clamp 推导在此不生效（要自适应需补估算输入，属另一条决策）。
//
// 两条边界：尾窗分支同时是真空区覆盖的唯一触发点（见 CoverHistoryGap）；调用方的
// selectEventTail 把 maxUnits<=0 判为"不读"（返回空历史），故 0 必须回退。
func WindowTailBudget(contextWindow int, policy seelexctx.WindowPolicy) (tokenBudget, maxUnits int) {
	tokenBudget = contextWindow
	info := seelexctx.ProviderContextInfo{ContextTokens: tokenBudget}
	rounds, _ := policy.WindowRounds(context.Background(), info)
	if rounds <= 0 {
		rounds = seelexctx.DefaultWindowConfig().MinRounds
	}
	return tokenBudget, rounds
}

// GapCoverageDeps 是真空区补压要做的那一跳需要的读数（值语义）。
type GapCoverageDeps struct {
	// AllEvents 是会话完整事件流（append-only 真相源，含已压缩轮次）。
	AllEvents []store.Event
	// TailStartUnit 是尾窗第一个单元在单元空间里的下标（由存储层选窗时回报）。
	TailStartUnit int
	// Stacks 是压缩栈（读帧 + 推帧）。nil = 没有栈可读，直接无事返回。
	Stacks seelexctx.CompactStackStore
	// Record 提供本地压缩 Chapter 2 所需的 plan/task 投影；nil → 只使用压栈帧。
	Record func() store.SessionContextRecord
	// Turns 是真空区轮次原文归档（nil = 不归档）。
	Turns seelexctx.TurnArchiver
	// SessionID 是帧 SegmentID 的溯源前缀（可空）。
	SessionID string
	// FrameCarryTokens 是帧摘要传递上限（limits.context_frame_carry_tokens）：
	// 真空区合并帧同样受并入上限约束，避免栈顶帧随帧数无界膨胀。
	FrameCarryTokens int
}

// CoverHistoryGap 把滑动窗口与压缩内容之间的真空区轮次压缩为合并帧
// （`seelexctx.CoverHistoryGap`）：在完整事件流的单元空间里，压缩栈顶 To 与尾窗
// 起点 tailStartUnit 直接对齐，[栈顶To+1, tailStartUnit-1] 即未覆盖区间；它
// 压入会话压缩栈（state blob / 内存兜底），原文经 TurnArchiver 归档
// （read_compressed_turn 可读回）。无真空区 → 无副作用。
//
// **没有压缩基线就不补**（这条判据是本函数存在的理由）：会话从未在运行期产生过
// CompactStack 帧时，冷恢复只装载尾窗，与"未重启继续运行"的上下文组成一致。否则
// 每次恢复都会凭空生成一个覆盖会话头部的合成摘要帧，使恢复后首请求比中断前多出模型
// 从未见过的前缀（前缀不稳定 / 顺序不一致）。
func CoverHistoryGap(ctx context.Context, deps GapCoverageDeps) error {
	if len(deps.AllEvents) == 0 || deps.Stacks == nil {
		return nil
	}
	frames := deps.Stacks.CompactStackSnapshot()
	if len(frames) == 0 {
		return nil
	}
	record := store.SessionContextRecord{CompactStack: frames}
	if deps.Record != nil {
		record = deps.Record()
		record.CompactStack = frames
	}
	_, err := seelexctx.CoverHistoryGap(ctx, seelexctx.GapCoverageOptions{
		AllEvents:        deps.AllEvents,
		TailStartUnit:    deps.TailStartUnit,
		Record:           record,
		Stacks:           deps.Stacks,
		Turns:            deps.Turns,
		SessionID:        deps.SessionID,
		FrameCarryTokens: deps.FrameCarryTokens,
	})
	return err
}

// RelatedMemoryBlocks 按当前查询从 CompactStack 全部帧选取相关记忆块
// （不止栈顶：超长会话的久远相关段经 seelexctx/memory 词法选取器召回）。
// 空记录 / 空查询 / 无命中 → 不注入（请求内容不变）。
func RelatedMemoryBlocks(frames []store.CompactFrame, query string) []seelectx.PromptBlock {
	if len(frames) == 0 {
		return nil
	}
	candidates := make([]memory.Candidate, 0, len(frames))
	for _, frame := range frames {
		candidates = append(candidates, memory.Candidate{
			SegmentID: frame.SegmentID, Summary: frame.Summary,
			Evidence: frame.Evidence, From: frame.From, To: frame.To,
		})
	}
	opts := memory.DefaultOptions()
	selected := memory.Select(query, candidates, opts)
	if len(selected) == 0 {
		return nil
	}
	if block := memory.RenderMemoryBlock(selected, opts.MaxTokens); block != nil {
		return []seelectx.PromptBlock{*block}
	}
	return nil
}
