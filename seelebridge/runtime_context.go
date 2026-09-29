package seelebridge

import (
	"context"
	"fmt"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"

	seenode "github.com/RedHuang-0622/seelex/seelebridge/node"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/seelexctx/compactor"
	"github.com/RedHuang-0622/seelex/seelexctx/memory"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// AttachSessionContextStore 绑定会话上下文存储（state blob，plan.md §3.7.2）。
// 会话恢复流程接线时由调用方注入（router + sessionID 就绪后）。
func (r *Runtime) AttachSessionContextStore(store *sessionstore.SessionContextStore) {
	if bundle := r.activeBundle(); bundle != nil {
		bundle.mu.Lock()
		bundle.binding.attachContextStore(store)
		bundle.mu.Unlock()
	}
}

// SetProjectKnowledgeProvider 注入项目级模块语义提供者（ProjectKnowledge
// 会话前预读；nil 关闭 project 块）。
func (r *Runtime) SetProjectKnowledgeProvider(provider func() *sessionstore.ProjectRecord) {
	r.setProjectKnowledge(provider)
}

// compactorInstance 返回跨会话快照压缩器（构造一次，会话间复用）。
func (r *Runtime) compactorInstance() *compactor.Compactor {
	return compactor.NewCompactor()
}

// compressionSnapshot 跨会话快照压缩输入：当前节点/会话的父证据快照
// （compactor 路径；无 → 走 QuickChat 路径）。
func (r *Runtime) compressionSnapshot(_ string) *snapshot.ContextSnapshot {
	return r.node.ParentEvidence()
}

// coverHistoryGap 把滑动窗口与压缩内容之间的真空区轮次压缩为合并帧
// （seelexctx.CoverHistoryGap）：在完整事件流的单元空间里，压缩栈顶 To 与尾窗
// 起点 tailStartUnit 直接对齐，[栈顶To+1, tailStartUnit-1] 即未覆盖区间；它
// 压入会话压缩栈（state blob / 内存兜底），原文经 TurnArchiver 归档
// （read_compressed_turn 可读回）。无真空区 → 无副作用。
func (r *Runtime) coverHistoryGap(ctx context.Context, allEvents []sessionstore.Event, tailStartUnit int) error {
	if len(allEvents) == 0 {
		return nil
	}
	stacks := runtimeCompactStacks{runtime: r, memory: seelexctx.NewMemoryCompactStack()}
	record := stacks.Snapshot()
	if len(record.CompactStack) == 0 {
		// 没有压缩基线（会话从未在运行期产生过 CompactStack 帧）时不做真空区
		// 补压：冷恢复只装载尾窗，与“未重启继续运行”的上下文组成一致。否则
		// 每次恢复都会凭空生成一个覆盖会话头部的合成摘要帧，使恢复后首请求
		// 比中断前多出模型从未见过的前缀（前缀不稳定 / 顺序不一致）。
		return nil
	}
	_, err := seelexctx.CoverHistoryGap(ctx, seelexctx.GapCoverageOptions{
		AllEvents:     allEvents,
		TailStartUnit: tailStartUnit,
		Record:        record,
		Stacks:        stacks,
		Turns:         r.getTurnArchiver(),
		SessionID:     r.MainSessionID(),
		// 帧摘要传递上限（limits.context_frame_carry_tokens）：真空区合并帧同样
		// 受并入上限约束，避免栈顶帧随帧数无界膨胀。
		FrameCarryTokens: r.limits.ContextFrameCarryTokens,
	})
	return err
}

// mainContextComponents 构造主会话的 ContextComponents（plan.md §3.1 步骤 4）。
// SystemPrompt 置空：会话级提示由 application 经 SetSystemPrompt 注入，
// 保持既有行为；Assembler 的 system provider 在应用层迁移后接管。
func (r *Runtime) mainContextComponents() session.ContextComponents {
	return session.ContextComponents{
		Assembler: r.seelexAssembler(),
		// 工具结果归档上限取 limits 生效值：传 0 会退回出厂默认，
		// max_tool_result_chars 就对回合内归档失效了。
		ToolResultProcessor: seelexctx.NewToolResultProcessor(r.limits.MaxToolResultChars, nil),
		Compressor:          r.seelexCompressor(),
		Controller:          r.seelexController(),
	}
}

// nodeContextComponents 构造节点子代理会话的 ContextComponents
// （bridge.WithSessionComponents 输入）。节点级 PromptBlocks 由
// SeelexAgentNode.Run 注入 ctx（ScopeAssembler 合并），本组件与
// 主会话共享同一套适配器依赖（预算按节点账号限额推导）。
func (r *Runtime) nodeContextComponents() session.ContextComponents {
	return session.ContextComponents{
		Assembler: r.node.Assembler(),
		ToolResultProcessor: seelexctx.NewToolResultProcessor(r.limits.MaxToolResultChars, seenode.ToolResultArchiver{
			ArchiverFor: r.node.ToolResultArchiverFor,
			Shared:      seelexctx.NewInMemoryToolResultArchiver(),
		}),
		Compressor: r.seelexCompressor(),
		Controller: r.nodeController(),
	}
}

// nodeController 构造节点子代理会话的上下文控制器：压缩栈与主会话隔离
// （子代理压缩帧不再写入主会话 SessionContextStore），窗口/预算仍按节点
// 账号限额推导。节点级栈当前为内存态（运行期隔离优先；节点会话记录
// 落盘承载恢复数据面）。
func (r *Runtime) nodeController() seelectx.ContextController {
	policy := seelexctx.NewContextWindowPolicy(r.ContextWindow(), r.MaxOutputTokens(), r.limits)
	return seelexctx.NewContextController(seelexctx.ControllerOptions{
		Policy:             policy,
		Window:             r.windowPolicy(),
		Budget:             runtimeBudgetProvider{runtime: r},
		Stacks:             seelexctx.NewMemoryCompactStack(),
		Turns:              r.getTurnArchiver(),
		MaxToolResultChars: r.limits.MaxToolResultChars,
		// 节点压缩帧 SegmentID 溯源到节点会话：与主会话栈隔离（2026-08-24 修复）。
		SessionIDProvider: func() string { return "node" },
		// 压缩 DAG：节点子代理也走 select_range → chapter1/2 → merge 的
		// workplan 图（2026-09-06 压缩 DAG 详设 §4.6）。前缀重放摘要器
		// 暂不注入（字节级装配出口未固化，见设计 §9 风险 1）→ 本地折叠。
		Compaction: seelexctx.NewCompactionDAG(seelexctx.CompactionDAGOptions{
			SessionIDProvider: func() string { return "node" },
			FrameCarryTokens:  r.limits.ContextFrameCarryTokens, // limits.context_frame_carry_tokens
			ReplayInputTokens: r.replayInputTokens(),
		}),
	})
}

// relatedMemoryBlocks 按当前查询从 CompactStack 全部帧选取相关记忆块
// （不止栈顶：超长会话的久远相关段经 seelexctx/memory 词法选取器召回）。
// 无绑定存储 / 空查询 / 无命中 → 不注入（请求内容不变）。
func (r *Runtime) relatedMemoryBlocks(_ context.Context, query string) []seelectx.PromptBlock {
	store := r.sessionContextStore()
	if store == nil {
		return nil
	}
	record := store.Snapshot()
	if len(record.CompactStack) == 0 {
		return nil
	}
	candidates := make([]memory.Candidate, 0, len(record.CompactStack))
	for _, frame := range record.CompactStack {
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

// resolvePlaceholder 解析 {{name}} 占位符（当前无内置变量，未知占位符
// 原样保留）。
func (r *Runtime) resolvePlaceholder(name string) (string, error) {
	return "", nil
}

// seelexAssembler 构造 seelex 装配器：栈块（now using = 栈顶）来自
// SessionContextStore（未注入 → 无块）；project 块来自 ProjectKnowledge
// 提供者（未注入 → 无块）；记忆块按当前查询从历史压缩段选取（无存储 →
// 无块）；占位符解析委托 seelexctx。
func (r *Runtime) seelexAssembler() seelectx.RequestAssembler {
	return seelexctx.NewAssembler(seelexctx.AssemblerOptions{
		SystemPrompt: nil, // 会话级提示由 application 侧注入（迁移后经此渲染）
		ProjectBlock: r.projectBlock,
		PrefixStacks: r.prefixStacks,
		TailStacks:   r.tailStacks,
		Memories:     r.relatedMemoryBlocks,
		Resolver: seelectx.PlaceholderResolverFunc(func(_ context.Context, name string) (string, error) {
			return r.resolvePlaceholder(name)
		}),
	})
}

// seelexCompressor 构造压缩器：短历史快速路径 + QuickChat 结构化 checkpoint
// （共享账号 completer 的隔离调用，无工具、独立 history）。
func (r *Runtime) seelexCompressor() seelectx.Compressor {
	quickChat, err := seelectx.NewQuickChat(r.completer)
	if err != nil {
		quickChat = nil // 装配失败 → 仅短历史/快照路径可用
	}
	return seelexctx.NewCompressor(seelexctx.CompressorOptions{
		QuickChat: quickChat,
		Compactor: r.compactorInstance(),
		SnapshotFor: func(_ context.Context, request seelectx.CompressionRequest) *snapshot.ContextSnapshot {
			return r.compressionSnapshot(request.SessionID)
		},
	})
}

// seelexController 构造控制器：窗口策略来自 RuntimeConfig.WindowConfig
// （DefaultWindowPolicy，plan.md §3.7.3），阈值预算的窗口/输出来自账号限额，
// 比例与除数来自 limits 段（context_soft_percent / context_hard_percent /
// context_target_percent / context_safety_reserve_divisor）。工具结果归档上限
// 同源于 limits：processor、控制器、应用装配层因此只有一份生效值。
func (r *Runtime) seelexController() seelectx.ContextController {
	policy := seelexctx.NewContextWindowPolicy(r.ContextWindow(), r.MaxOutputTokens(), r.limits)
	return seelexctx.NewContextController(seelexctx.ControllerOptions{
		Policy:             policy,
		Window:             r.windowPolicy(),
		Budget:             runtimeBudgetProvider{runtime: r},
		Stacks:             runtimeCompactStacks{runtime: r, memory: seelexctx.NewMemoryCompactStack()},
		Turns:              r.getTurnArchiver(),
		MaxToolResultChars: r.limits.MaxToolResultChars,
		// 帧摘要传递上限（limits.context_frame_carry_tokens）：本地折叠把上一栈顶
		// 帧 Chapter 2 正文并入新帧时的并入量上限，超出退化为锚点。
		FrameCarryTokens: r.limits.ContextFrameCarryTokens,
		// 压缩帧 SegmentID 溯源到当前会话：每次压缩动态取值，会话切换后
		// 仍指向正确会话（compact-<sessionID>-<ms>）。
		SessionIDProvider: r.MainSessionID,
		// 压缩 DAG（2026-09-06 详设 §4.6）：阈值/窗口/去重/归档不变，
		// 帧生成改走 workplan 图。**控制器路径的 Summarizer 仍不注入**：
		// 启用前提是字节级装配出口快照固化（system/History/Tools 与真实请求
		// 同一条装配路径，见设计 §9 风险 1），而控制器拿到的 ev.History 是否
		// 与 wire 同源尚未验证——贸然注入会付全价却拿不到前缀缓存。
		// 装配层折叠（application/core/context_runtime，回合开始前那条路径）
		// 已注入，因为它手里正好握着上一次真实请求的三样原件，见
		// MainCompactionDAG。未注入时 Chapter 2 恒本地折叠。
		Compaction: seelexctx.NewCompactionDAG(seelexctx.CompactionDAGOptions{
			SessionIDProvider: r.MainSessionID,
			SystemPrompt: func() string {
				if store := r.sessionContextStore(); store != nil {
					return store.SystemPrompt()
				}
				return ""
			},
			Tools: func() []types.Tool {
				if r.agt == nil {
					return nil
				}
				return r.agt.VisibleTools(context.Background())
			},
			FrameCarryTokens:  r.limits.ContextFrameCarryTokens, // limits.context_frame_carry_tokens
			ReplayInputTokens: r.replayInputTokens(),
		}),
	})
}

// replayInputTokens 返回分片重放的片预算（模型输入侧）：取账号上下文窗口减去
// 固定开销后的保守值。≤0 → 不分片（走原有单次重放，保住前缀缓存）。
//
// 为什么用 ContextWindow 的保守份额而不是硬阈值：分片是"压缩区自身大到一次发不出"
// 的容灾路径，判据应当是"这一次请求能不能装下原始溢出区"，而不是"要不要现在压缩"。
// 取窗口的 3/4 给正文、留 1/4 给 system/tools/指令与估算偏差。
//
// limits.context_compaction_summary.input_tokens > 0 时以配置为准；未配置才走
// 这里的推导——旋钮的零值必须是"沿用既有行为"，否则加了它就静默改掉片预算。
func (r *Runtime) replayInputTokens() int {
	if configured := r.limits.ContextCompactionSummary.InputTokens; configured > 0 {
		return configured
	}
	window := r.ContextWindow()
	if window <= 0 {
		return 0
	}
	return window * 3 / 4
}

// compactionSummarizer 按开关构造前缀重放厚摘要器（limits.context_compaction_summary）。
//
// 关闭、QuickChat 装配失败、摘要器构造失败三种情况一律返回 **nil**：nil 是
// chapter2Node 的显式判据（`d.opts.Summarizer != nil`），落到本地确定性折叠，
// 既不报错也不静默降级成"发一次没有缓存的调用"。QuickChat 走的是与
// seelexCompressor 同一条构造路径（共享账号 completer 的隔离调用，无工具、
// 独立 history），不第二次装配 completer。
func (r *Runtime) compactionSummarizer() seelexctx.PrefixReplaySummarizer {
	summarizer, _ := r.compactionSummarizerWithNote()
	return summarizer
}

// compactionSummarizerWithNote 是上面那一跳的唯一实现：除了摘要器，还给出
// **为什么没有摘要器**（三种 nil 出口的分别是"开关关闭 / QuickChat 装配失败 /
// 摘要器构造失败"）。三个出口此前都不留痕——windowsgui 构建下 log.Printf 也无处
// 可看，现场只剩一个 `summary_source=local`；这份 note 走进折叠 DAG 的
// SummarizerNote，最终落进帧证据与帧正文，让"模型为什么没被叫到"在帧里自答。
func (r *Runtime) compactionSummarizerWithNote() (seelexctx.PrefixReplaySummarizer, string) {
	if !r.limits.ContextCompactionSummary.Enabled {
		return nil, "折叠处厚摘要开关关闭（limits.context_compaction_summary.enabled 非 true），本次不调用模型"
	}
	quickChat, err := seelectx.NewQuickChat(r.completer)
	if err != nil || quickChat == nil {
		return nil, fmt.Sprintf("QuickChat 装配失败（%v），本次不调用模型", err)
	}
	summarizer, err := seelexctx.NewQuickChatPrefixReplaySummarizer(quickChat)
	if err != nil {
		return nil, fmt.Sprintf("前缀重放摘要器构造失败（%v），本次不调用模型", err)
	}
	return summarizer, ""
}

// MainCompactionDAG 返回**装配层折叠**（application/core/context_runtime，回合
// 开始前那条路径）用的压缩 DAG 执行器。
//
// 与 seelexController 里那份的差别只有一处、但很关键：这份**注入 Summarizer**。
// 装配层在折叠那一刻手里握着上一次真实请求的 system / history / tools 三样原件
// （coordinator.go 的 systemPrompt / existing / tools，全部来自产出该请求的同一
// 条装配路径），因此重放请求能与真实请求共享字节前缀、几乎全命中缓存——详设 §9
// 风险 1 的启用前提在这条路径上是**满足**的。控制器路径拿到的 ev.History 是否
// 与 wire 同源尚未验证，所以那边仍不注入。
//
// SystemPrompt / Tools 与 seelexController 同源（会话上下文存储 + 可见工具），
// 保证两条路径产出的帧形状一致。
func (r *Runtime) MainCompactionDAG() *seelexctx.CompactionDAG {
	summarizer, summarizerNote := r.compactionSummarizerWithNote()
	return seelexctx.NewCompactionDAG(seelexctx.CompactionDAGOptions{
		SessionIDProvider: r.MainSessionID,
		Summarizer:        summarizer,
		SummarizerNote:    summarizerNote,
		SystemPrompt: func() string {
			if store := r.sessionContextStore(); store != nil {
				return store.SystemPrompt()
			}
			return ""
		},
		Tools: func() []types.Tool {
			if r.agt == nil {
				return nil
			}
			return r.agt.VisibleTools(context.Background())
		},
		Chapter2MaxTokens: r.limits.ContextCompactionSummary.Chapter2Tokens,
		FrameCarryTokens:  r.limits.ContextFrameCarryTokens,
		ReplayInputTokens: r.replayInputTokens(),
	})
}

// sessionContextStore 返回绑定的会话上下文存储（nil = 未绑定）。
func (r *Runtime) sessionContextStore() *sessionstore.SessionContextStore {
	if bundle := r.activeBundle(); bundle != nil {
		return bundle.binding.contextStore()
	}
	return nil
}

// stackBlocks 渲染会话级全部使用栈块（稳定前缀 skill/compact + 动态尾部
// plan/task；now using = 栈顶；未绑定存储 → 无块）。节点子代理继承路径
// 兼容入口（InheritedBlocks），保持原有块集合不变。
func (r *Runtime) stackBlocks() []seelectx.PromptBlock {
	store := r.sessionContextStore()
	if store == nil {
		return nil
	}
	return seelexctx.RenderStackBlocks(store.Snapshot())
}

// prefixStacks 渲染稳定前缀栈块（skill/compact，now using = 栈顶；未绑定
// 存储 → 无块）：主会话装配器在记忆块之后、累积 context 之前注入。
func (r *Runtime) prefixStacks() []seelectx.PromptBlock {
	store := r.sessionContextStore()
	if store == nil {
		return nil
	}
	return seelexctx.RenderStablePrefixBlocks(store.Snapshot())
}

// tailStacks 渲染动态尾部栈块（plan/task，now using = 栈顶；未绑定存储 →
// 无块）：主会话装配器在 WorkingHistory 之后、贴近当前输入注入。
func (r *Runtime) tailStacks() []seelectx.PromptBlock {
	store := r.sessionContextStore()
	if store == nil {
		return nil
	}
	return seelexctx.RenderTailBlocks(store.Snapshot())
}

// windowPolicy 返回当前窗口策略（NewRuntime 时按配置构造）。
func (r *Runtime) windowPolicy() seelexctx.WindowPolicy {
	r.windowMu.RLock()
	defer r.windowMu.RUnlock()
	return r.window
}

// windowTailBudget 推导主会话 DurableHistory 的读尾预算（D1，plan.md §9）。
// 它是「从磁盘读哪些分片」的宽度上限，不是压缩保留前缀的决策——保留前缀走
// WindowConfig.RetainedContextTokens 的 min(token1, token2)。
//   - tokenBudget = 账号上下文窗口：恒 ≥ 保留前缀（该函数已把结果夹在
//     [1, all_context]），所以这一维实际不约束；
//   - maxUnits = 轮数：本路径只给 ProviderContextInfo 填 ContextTokens，
//     AvgRoundTokens/Reserved 缺省 → WindowRounds 恒走"输入缺失回退"分支，
//     因此实际值 = window.rounds（显式配置时）或 window.min_rounds，
//     clamp 推导在此不生效（要自适应需补估算输入，属另一条决策）。
//
// 两条边界：尾窗分支同时是真空区覆盖的唯一触发点（见 runtime.go 装配）；
// selectEventTail 把 maxUnits<=0 判为"不读"（返回空历史），故 0 必须回退。
func (r *Runtime) windowTailBudget() (tokenBudget, maxUnits int) {
	tokenBudget = r.ContextWindow()
	info := seelexctx.ProviderContextInfo{ContextTokens: tokenBudget}
	rounds, _ := r.windowPolicy().WindowRounds(context.Background(), info)
	if rounds <= 0 {
		rounds = seelexctx.DefaultWindowConfig().MinRounds
	}
	return tokenBudget, rounds
}
