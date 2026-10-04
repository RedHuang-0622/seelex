// 压缩 DAG 执行器（docs/2026-09-06-compaction-dag/design.md §4）。
//
// 压缩过程用 Seele workplan 表达：复用 codec.Import + runtime/runner.Run，
// 不自造编排轮子。图形态与详设 §4.1 一致（select_range → 章节级 fork
// {chapter1_anchor, chapter2_thick} → merge_frame → publish_stack）。本次
// 里程碑先以串行调度跑通（详设 §4.5/§7：fork 并发后续再接），节点语义
// 由 codec.NodeFactory 装配成本包实现，Seele 不解释产品语义。
//
// 数据流与兜底：
//   - select_range：溢出单元切分 + request 定界素材收集（纯计算）；
//   - chapter1_anchor：栈顶 → 锚点/一句话（降级标记 degraded）；
//   - chapter2_thick：PrefixReplaySummarizer（一次重试）→ 失败回退本地
//     确定性压缩（summary_source=local，不消耗模型 token）；
//   - merge_frame：纯函数拼装 CompactFrame（SegmentID/From/To/Evidence/
//     两章节 Summary + 链锚字段）；
//   - publish_stack：适配点（当前由 controller/gap 在去重与原文归档后
//     PushCompact，保留既有审计语义，见 controller.go compressWindowOutsideWith）。
//
// 装配失败或节点尚未开始执行 → 同图节点按依赖序串行 Run（串行化兜底）。
package seelexctx

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	frameworktypes "github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/Seele/workplan/codec"
	"github.com/RedHuang-0622/Seele/workplan/core/node"
	workplantypes "github.com/RedHuang-0622/Seele/workplan/core/types"
	"github.com/RedHuang-0622/Seele/workplan/runtime/runner"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// compactionNodeInput 是 codec 文档节点的产品输入（仅 purpose，节点语义由
// 本包 NodeFactory 装配；Seele 不解释）。
type compactionNodeInput struct {
	Purpose string `json:"purpose"`
}

// compactionDAGDocumentJSON 是详设 §4.1 的规范 codec.Document 形态
// （审计/展示与运行共用同一图）。
const compactionDAGDocumentJSON = `{
  "version": 1,
  "entry": "select_range",
  "nodes": [
    {"id": "select_range", "kind": "function", "input": {"purpose": "select_overflow"}},
    {"id": "chapter1_anchor", "kind": "function", "input": {"purpose": "build_prev_anchor"}},
    {"id": "chapter2_thick", "kind": "function", "input": {"purpose": "prefix_replay_summary"}},
    {"id": "merge_frame", "kind": "function", "input": {"purpose": "merge_chapters"}},
    {"id": "publish_stack", "kind": "function", "input": {"purpose": "push_compact_frame"}}
  ],
  "edges": [
    {"from": "select_range", "to": "chapter1_anchor"},
    {"from": "select_range", "to": "chapter2_thick"},
    {"from": "chapter1_anchor", "to": "merge_frame"},
    {"from": "chapter2_thick", "to": "merge_frame"},
    {"from": "merge_frame", "to": "publish_stack"}
  ]
}`

// compactionNodeOrder 是串行化兜底与工厂共用的节点依赖序。
var compactionNodeOrder = []string{
	"select_range", "chapter1_anchor", "chapter2_thick", "merge_frame", "publish_stack",
}

// CompactionInput 是一次压缩 DAG 运行的产品输入（controller/gap 共用）。
type CompactionInput struct {
	// Record 是会话记录快照（Task/Plan 栈 + 既有 CompactStack；prev top
	// 由执行器从栈顶推导）。
	Record sessionstore.SessionContextRecord
	// Messages 是本次新覆盖的完整协议单元原文（扁平消息序，保留原样；
	// select_range 会重新切分单元）。
	Messages []frameworktypes.Message
	// UnitCount 是本次新覆盖的完整协议单元数（0 → 由 Messages 切分推导）。
	UnitCount int
	// History 是最近一次真实请求的历史字节素材（前缀重放用；nil/空 →
	// Chapter 2 走本地压缩，不消耗模型 token）。
	History []frameworktypes.Message
	// PrecomputedSummary 是调用方在**压缩之前**已经拿到的模型读后感（前缀重放
	// 厚摘要）。非空 → chapter2 直接用它，不再调用模型：同一次压缩只该有一次模型
	// 调用，而调用方必须先知道"这次到底有没有读后感"才决定要不要折上下文
	// （见 application/core/context_runtime 的 compactionReadbackProbe）。
	//
	// 语义是 **Chapter 2 正文**，不是整份两章节摘要：读数闸回执天然带整份摘要，
	// 传进来之前要经 Chapter2Body 归一化（本字段的第二个消费者就栽在这里）。
	PrecomputedSummary string
	// PrecomputedSummarySource 是上面那份读后感的来源（replay | local）。空 →
	// 按 replay 处理（旧调用方的语义：它只在拿到模型读后感时才带 PrecomputedSummary）。
	PrecomputedSummarySource string
	// PrecomputedSummaryNote 说明"这份预读为什么没有模型读后感"（空 = 有模型
	// 读后感）。它与 Source 必须一起带下来：预读落在本地确定性压缩上时，帧要如实
	// 写 local + 这条原因，而不是盖成 replay 再把原因丢掉——那正是"压缩看起来成功、
	// 帧却是空骨架、模型为什么没被叫到又查不到"的成因。
	PrecomputedSummaryNote string
	// Kind 决定本地压缩的 Current Work 文案（溢出 / 真空区）。
	Kind LocalCompactKind
	// RequestFrom/RequestTo 是本次覆盖的 request 首尾（空 → 按 ChatQueue
	// 单元标签推导 chat-N；元数据，不进模型正文）。
	RequestFrom string
	RequestTo   string
}

// CompactionDAGOptions 是压缩 DAG 的构造注入依赖。
type CompactionDAGOptions struct {
	// SessionIDProvider 提供会话 ID 用于帧 SegmentID 溯源（nil → 无前缀，
	// 与 controller 旧语义一致）。
	SessionIDProvider func() string
	// SegmentPrefix 是 SegmentID 前缀（空 → "compact"；gap 路径可传
	// "compact-gap" 保持命名一致）。
	SegmentPrefix string
	// Summarizer 是前缀重放厚摘要器（nil → 恒本地压缩）。
	Summarizer PrefixReplaySummarizer
	// SummarizerNote 说明"摘要器为什么不可用"：开关关闭 / QuickChat 装配失败 /
	// 摘要器构造失败（装配层三种 nil 出口），以及**这条链路结构上不注入摘要器**
	// （回合内控制器/节点控制器链路）。它只在 Summarizer == nil 时被消费：nil 的
	// 各出口此前都不留痕，现场只剩一个 summary_source=local，读帧的人无从分辨是
	// 哪一种——而"是配置没开、是装配失败、还是这条链路本来就不接"的处置完全不同。
	SummarizerNote string
	// SystemPrompt/Tools 是前缀重放字节素材提供者（与真实请求同源时才有
	// 前缀命中价值；nil → 重放请求不带对应素材）。
	SystemPrompt func() string
	Tools        func() []frameworktypes.Tool
	// Chapter2MaxTokens 是厚摘要输出预算（≤0 → PrefixReplayMaxTokens；
	// QuickChat 通道未透传预算时仅作记录）。
	Chapter2MaxTokens int
	// FrameCarryTokens 是帧摘要传递上限（limits.context_frame_carry_tokens；
	// ≤0 → DefaultFrameCarryTokens）：本地压缩把上一栈顶帧 Chapter 2 正文并入
	// 新帧时的并入量上限，超出退化为锚点（见 CarryPreviousChapter2）。
	FrameCarryTokens int
	// ReplayInputTokens 是**分片重放的片预算**（模型输入侧；≤0 → 不分片，走原有
	// 单次重放）。溢出区自身超过它时按协议单元切片逐片重放、摘要前向传递
	// （见 ChunkReplayMessages / SummarizeChunkPlan）。
	ReplayInputTokens int
}

// PrefixReplayMaxTokens 是 Chapter 2 厚摘要的默认输出预算。
const PrefixReplayMaxTokens = 2048

// CompactionDAG 执行压缩 DAG（见文件头注释）。
type CompactionDAG struct {
	opts CompactionDAGOptions
}

// NewCompactionDAG 构造压缩 DAG 执行器。
func NewCompactionDAG(opts CompactionDAGOptions) *CompactionDAG {
	if opts.SegmentPrefix == "" {
		opts.SegmentPrefix = "compact"
	}
	return &CompactionDAG{opts: opts}
}

// compactionDAGState 是单次运行的共享状态（节点闭包写入，runner 只调度）。
type compactionDAGState struct {
	input         CompactionInput
	record        sessionstore.SessionContextRecord
	prevTop       *sessionstore.CompactFrame
	overflow      []historyUnit
	unitCount     int
	chapter1      string
	anchorSource  string
	chapter2      string
	summarySource string
	// degradeCode / degradeNote 是"这次为什么没有模型摘要"的事实（code 进帧证据
	// ref，note 进证据正文与帧正文）。replay 成功时两者保持为空——只记降级，
	// 不记正常。
	degradeCode string
	degradeNote string
	frame       sessionstore.CompactFrame
	// carry 是「上一帧 Chapter 2 并入」的决策事实（帧摘要传递上限）。
	carry CarryDiagnostics
	// replay 是分片重放的计划事实（未分片 → 空计划）。
	replay ReplayChunkPlan
	// replayMaterial 是重放素材的规整事实（逐字未动 → 零报告 → 不写证据）。
	replayMaterial ReplayMaterialReport
	started        map[string]bool
	startedMu      sync.Mutex
}

// Execute 运行压缩 DAG 并返回拼装完成的 CompactFrame（不含 PushCompact；
// 压栈与去重由调用方保留，见文件头注释）。失败时按详设 §4.5 逐级兜底：
// Chapter 2 失败 → 本地压缩；装配/未执行失败 → 串行化兜底。
func (d *CompactionDAG) Execute(ctx context.Context, input CompactionInput) (sessionstore.CompactFrame, error) {
	if d == nil {
		return sessionstore.CompactFrame{}, fmt.Errorf("seelexctx: compaction dag is nil")
	}
	state := d.newState(input)
	plan, planErr := codec.Import[compactionNodeInput](
		[]byte(compactionDAGDocumentJSON), d.nodeFactory(state))
	if planErr == nil {
		if _, runErr := runner.New(plan).Run(ctx); runErr == nil {
			return state.frame, nil
		} else if ctx.Err() != nil {
			return sessionstore.CompactFrame{}, ctx.Err()
		} else if state.startedCount() > 0 {
			// 节点已开始执行后失败：不重复跑（避免重复模型调用/副作用）。
			return sessionstore.CompactFrame{}, fmt.Errorf("seelexctx: compaction dag run: %w", runErr)
		}
		// 节点尚未开始 → 按串行化兜底执行。
		planErr = nil
	}
	if err := d.runSerial(ctx, state); err != nil {
		return sessionstore.CompactFrame{}, fmt.Errorf("seelexctx: compaction dag serial fallback: %w", err)
	}
	return state.frame, nil
}

func (d *CompactionDAG) newState(input CompactionInput) *compactionDAGState {
	state := &compactionDAGState{
		input:   input,
		record:  input.Record,
		started: make(map[string]bool),
	}
	if len(input.Record.CompactStack) > 0 {
		top := input.Record.CompactStack[len(input.Record.CompactStack)-1]
		state.prevTop = &top
	}
	return state
}

// ── 节点实现（core/node.Node 最小执行契约）────────────────────────

// dagFuncNode 把纯函数适配为 workplan Node。
type dagFuncNode struct {
	id  string
	run func(context.Context) error
}

func (n *dagFuncNode) ID() string { return n.id }

func (n *dagFuncNode) Run(ctx context.Context, _ *workplantypes.WorkflowContext) (string, error) {
	if n.run == nil {
		return "", fmt.Errorf("seelexctx: dag node %q has no run func", n.id)
	}
	if err := n.run(ctx); err != nil {
		return "", err
	}
	return `{"status":"ok"}`, nil
}

func (d *CompactionDAG) nodeFactory(state *compactionDAGState) codec.NodeFactory[compactionNodeInput] {
	byID := d.buildNodes(state)
	return codec.NodeFactoryFunc[compactionNodeInput](func(spec codec.NodeSpec[compactionNodeInput]) (node.Node, error) {
		node, ok := byID[spec.ID]
		if !ok {
			return nil, fmt.Errorf("seelexctx: unknown compaction node %q", spec.ID)
		}
		return node, nil
	})
}

// buildNodes 构造同一次运行的全部节点（工厂与串行兜底共用同一组闭包）。
func (d *CompactionDAG) buildNodes(state *compactionDAGState) map[string]*dagFuncNode {
	nodes := []*dagFuncNode{
		{id: "select_range", run: d.selectRangeNode(state)},
		{id: "chapter1_anchor", run: d.chapter1Node(state)},
		{id: "chapter2_thick", run: d.chapter2Node(state)},
		{id: "merge_frame", run: d.mergeNode(state)},
		{id: "publish_stack", run: d.publishNode(state)},
	}
	byID := make(map[string]*dagFuncNode, len(nodes))
	for _, node := range nodes {
		byID[node.id] = node
	}
	return byID
}

func (d *CompactionDAG) runSerial(ctx context.Context, state *compactionDAGState) error {
	nodes := d.buildNodes(state)
	for _, id := range compactionNodeOrder {
		if err := nodes[id].run(ctx); err != nil {
			return err
		}
	}
	return nil
}

func markStarted(state *compactionDAGState, id string) {
	state.startedMu.Lock()
	defer state.startedMu.Unlock()
	state.started[id] = true
}

// degrade 记录"这次为什么没有模型摘要"。后写的覆盖先写的：先分片链失败、再单次
// 重放失败，留下的该是**最后一次**的原因。只有最终落到本地压缩时才进帧证据
// （见 mergeNode），重放成功会被 clearDegrade 清掉。
func (state *compactionDAGState) degrade(code, note string) {
	state.degradeCode = code
	state.degradeNote = note
}

// clearDegrade 在重放成功后清掉降级事实：降级记录只记"最终真的降级了"。
func (state *compactionDAGState) clearDegrade() {
	state.degradeCode = ""
	state.degradeNote = ""
}

func (state *compactionDAGState) startedCount() int {
	state.startedMu.Lock()
	defer state.startedMu.Unlock()
	return len(state.started)
}

// selectRangeNode：溢出单元切分（纯计算；request 首尾在 merge 按覆盖范围
// 推导，调用方显式传入的 RequestFrom/To 优先）。
func (d *CompactionDAG) selectRangeNode(state *compactionDAGState) func(context.Context) error {
	return func(context.Context) error {
		markStarted(state, "select_range")
		state.overflow = chatUnits(state.input.Messages, dagOrdinalBase(state.prevTop))
		state.unitCount = state.input.UnitCount
		if state.unitCount <= 0 {
			state.unitCount = len(state.overflow)
		}
		if state.unitCount <= 0 {
			return fmt.Errorf("seelexctx: compaction requires at least one overflow unit")
		}
		return nil
	}
}

// chapter1Node：栈顶 → 锚点/一句话；prev 缺失（首帧）或提取不到一句话时
// 标记 degraded（详设 §4.5）。
func (d *CompactionDAG) chapter1Node(state *compactionDAGState) func(context.Context) error {
	return func(context.Context) error {
		markStarted(state, "chapter1_anchor")
		state.chapter1 = RenderAnchorChapter(state.prevTop)
		state.anchorSource = FrameAnchorSource(state.prevTop)
		return nil
	}
}

// chapter2Node：前缀重放厚摘要（一次重试）→ 失败/无重放素材回退本地压缩。
//
// 重放素材先过 wire 协议规整（PrepareReplayMaterial，见 replay_material.go）：素材
// 是"请求出口修复之前"的历史快照，未回执的工具调用会让整条重放请求被 provider
// 400 拒收（2026-09-29 现场）。规整事实进帧证据（不静默改字节）；规整后仍不合法
// 就不发这次注定被拒的请求，改记 `replay-material-invalid`。
//
// 溢出区自身超过片预算（ReplayInputTokens）时走**分片重放链**：按协议单元切片
// 逐片重放，摘要前向传递（SummarizeChunkPlan），除首片外不追求前缀缓存命中
// （正确性与「不重复送原文」优先）。任何一片失败即整条回退本地压缩。
//
// 每条降级出口都留下 (code, note)：这是本轮压缩"没有模型摘要"的唯一解释来源。
// 早前三种出口（摘要器 nil / 无重放素材 / 重放调用失败）都不留痕，现场只有一个
// summary_source=local，排查只能靠猜配置、猜账号、猜调用——一个 458ms 的 index
// 门禁到底是"没调用"还是"调用失败"读不出来。
func (d *CompactionDAG) chapter2Node(state *compactionDAGState) func(context.Context) error {
	return func(ctx context.Context) error {
		markStarted(state, "chapter2_thick")
		// 调用方已在压缩之前拿到模型读后感（装配层的读数闸，见 CompactionInput.
		// PrecomputedSummary）：直接用，不重复调用模型。同一次压缩只该有一次模型
		// 调用，而"有没有读后感"必须在改写上下文之前就知道。
		if precomputed := strings.TrimSpace(state.input.PrecomputedSummary); precomputed != "" {
			// 调用方交回的是「这次压缩的读后感正文」，但链上有一个出口会把**整份
			// 两章节摘要**当成正文交过来（装配层读数闸回执）。先归一化到 Chapter 2：
			// 整份摘要直接当 Chapter 2 用，会让 Chapter 1 锚点在帧里出现两次，而且
			// 再读回来时正文被裁成空（见 Chapter2Body）——现场那句「压缩成功，帧却
			// 是一具空骨架」就是这一条。
			if body := Chapter2Body(precomputed); body != "" {
				state.chapter2 = body
				state.summarySource = strings.TrimSpace(state.input.PrecomputedSummarySource)
				if state.summarySource == "" {
					state.summarySource = CompactSummarySourceReplay
				}
				state.clearDegrade()
				// 预读回执自带的降级原因照带：它不是"这次压成了"的解释，而是"这次
				// 为什么没有模型读后感"的唯一证据（预读落在本地确定性压缩上）。丢掉
				// 它，帧就只剩一个 source 标记让人猜。
				if note := strings.TrimSpace(state.input.PrecomputedSummaryNote); note != "" {
					state.degrade(PrecomputedLocalDegradeCode, note)
				}
				return nil
			}
			// 归一化后没有任何 Chapter 2 正文（例如传进来的整份摘要，它自己的
			// Chapter 2 就是那具全 (none) 骨架）：它不是读后感，不能拿它冒充——
			// 落回下面的常规路径，由本地压缩 + 真实降级原因给出可读的交代。
		}
		material, materialReport := PrepareReplayMaterial(state.input.History)
		state.replayMaterial = materialReport
		materialErr := ValidateReplayProtocol(material)
		switch {
		case d.opts.Summarizer == nil:
			note := strings.TrimSpace(d.opts.SummarizerNote)
			if note == "" {
				// 默认措辞刻意不提"开关关闭或 QuickChat 装配失败"：那两种是
				// **装配层**的 nil 出口，未装配这一层的调用方未必适用。把没验证
				// 过的原因写成自答，读帧的人会去查一个并不存在的配置事故
				// （2026-09-30 现场：控制器链路的结构性 nil 被答成了开关问题）。
				note = "摘要器未装配（这条压缩链路没有注入摘要器，调用方未说明原因）"
			}
			state.degrade("no-summarizer", note)
		case len(state.input.History) == 0:
			state.degrade("no-replay-material",
				"无重放素材（上一次真实请求的引擎历史为空），本次不调用模型")
		case len(material) == 0:
			state.degrade("no-replay-material", fmt.Sprintf(
				"重放素材规整后为空：尾巴是一个未落定的工具调用单元（宣告的调用 %s 尚无回执），本次不调用模型",
				strings.Join(materialReport.DroppedTailCallIDs, ",")))
		case materialErr != nil:
			state.degrade("replay-material-invalid", fmt.Sprintf(
				"重放素材在 wire 协议规整后仍不合法，本次不发重放请求：%v", materialErr))
		default:
			request := ReplayRequest{
				SystemPrompt: d.systemPrompt(),
				History:      append([]frameworktypes.Message(nil), material...),
				Tools:        d.tools(),
				MaxTokens:    d.chapter2MaxTokens(),
			}
			if plan := d.replayChunkPlan(state); plan.ChunkCount() > 1 {
				result, err := SummarizeChunkPlan(ctx, d.opts.Summarizer, request, plan)
				if err == nil && strings.TrimSpace(result.Chapter2) != "" {
					state.chapter2 = normalizeReplayChapter2(result.Chapter2)
					state.summarySource = CompactSummarySourceReplay
					state.replay = plan
					state.replay.Chained = true
					state.clearDegrade()
					return nil
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				// 分片链失败 → 落回单次重放/本地压缩（不中断请求）。这里的失败原因
				// 先记下：若随后单次重放成功，它会被 clearDegrade 清掉。
				state.degrade("chunk-replay-failed",
					fmt.Sprintf("分片重放（%d 片）失败，已落回单次重放：%v", plan.ChunkCount(), err))
			}
			var lastErr error
			for attempt := 0; attempt < 2; attempt++ {
				result, err := d.opts.Summarizer.Summarize(ctx, request)
				if err == nil && strings.TrimSpace(result.Chapter2) != "" {
					state.chapter2 = normalizeReplayChapter2(result.Chapter2)
					state.summarySource = CompactSummarySourceReplay
					state.clearDegrade()
					return nil
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if err == nil {
					lastErr = fmt.Errorf("模型返回空摘要")
				} else {
					lastErr = err
				}
			}
			// 两次尝试均失败 → 本地压缩兜底（不消耗模型 token 的确定性路径）。
			state.degrade("replay-failed", fmt.Sprintf("前缀重放两次调用均失败，已回退本地压缩：%v", lastErr))
		}
		chapter2, carry := LocalChapter2WithCarry(LocalCompactOptions{
			Overflow:         state.overflow,
			UnitCount:        state.unitCount,
			Kind:             state.input.Kind,
			PrevTop:          state.prevTop,
			CarryLimitTokens: d.frameCarryTokens(),
			Record:           state.record,
		})
		state.chapter2 = chapter2
		state.carry = carry
		state.summarySource = CompactSummarySourceLocal
		return nil
	}
}

// replayChunkPlan 计算本次重放的分片计划：溢出区 token 数超过片预算
// （ReplayInputTokens）才分片，否则返回空计划（调用方走单次重放，保住前缀缓存）。
func (d *CompactionDAG) replayChunkPlan(state *compactionDAGState) ReplayChunkPlan {
	budget := d.replayInputTokens()
	if budget <= 0 {
		return ReplayChunkPlan{}
	}
	return ChunkReplayMessages(overflowMessages(state.overflow), budget)
}

// dagOrdinalBase 返回本次压缩单元区号的基准 = 已被覆盖帧覆盖的单元数
// （栈顶 To+1），无前驱 0。基准取自**已记录**的帧边界。
func dagOrdinalBase(prevTop *sessionstore.CompactFrame) int {
	if prevTop == nil {
		return 0
	}
	return prevTop.To + 1
}

// mergeNode：拼装 CompactFrame（SegmentID/From/To/Evidence + 链锚字段 +
// 两章节 Summary；复用 controller/gap 的覆盖语义）。
func (d *CompactionDAG) mergeNode(state *compactionDAGState) func(context.Context) error {
	return func(context.Context) error {
		markStarted(state, "merge_frame")
		// 帧区间记录实际被压单元的区号（不再用 unitCount 推算终点）：首/末
		// 单元自带 ordinal；合并帧起点沿用已记录的前帧起点（综合摘要覆盖
		// 从 From 到 To 的连续段）。
		count := state.unitCount
		from, to := 0, count-1
		if len(state.overflow) > 0 {
			from = state.overflow[0].ordinal
			to = state.overflow[len(state.overflow)-1].ordinal
		}
		if state.prevTop != nil {
			from = state.prevTop.From
		}
		requestFrom, requestTo := state.input.RequestFrom, state.input.RequestTo
		if requestFrom == "" || requestTo == "" {
			requestFrom, requestTo = ChatQueueRequestLabels(from, to)
		}
		frame := sessionstore.CompactFrame{
			SegmentID:     d.segmentID(),
			From:          from,
			To:            to,
			RequestFrom:   requestFrom,
			RequestTo:     requestTo,
			Summary:       RenderFrameSummary(state.chapter1, state.chapter2),
			SummarySource: state.summarySource,
			AnchorSource:  AnchorSourceWithCarry(state.anchorSource, state.carry),
			Evidence: append(overflowEvidence(state.overflow, state.record),
				append(CarryEvidence(state.carry),
					append(ReplayEvidence(state.replay),
						append(ReplayMaterialEvidence(state.replayMaterial),
							LocalCompactEvidence(state.degradeCode, state.degradeNote)...)...)...)...),
			CompressedAt: time.Now(),
		}
		if state.prevTop != nil {
			frame.PrevSegmentID = state.prevTop.SegmentID
			frame.PrevRequestFrom = state.prevTop.RequestFrom
			frame.PrevRequestTo = state.prevTop.RequestTo
			frame.PrevSummaryOneLine = OneLineSummary(*state.prevTop)
		}
		state.frame = frame
		return nil
	}
}

// publishNode：适配点节点——当前 PushCompact 留在调用方（去重/归档/审计），
// 本节点只校验 merge 已完成（保证图拓扑完整性）。
func (d *CompactionDAG) publishNode(state *compactionDAGState) func(context.Context) error {
	return func(context.Context) error {
		markStarted(state, "publish_stack")
		if state.frame.SegmentID == "" {
			return fmt.Errorf("seelexctx: publish_stack requires merge_frame output")
		}
		return nil
	}
}

func (d *CompactionDAG) systemPrompt() string {
	if d.opts.SystemPrompt == nil {
		return ""
	}
	return d.opts.SystemPrompt()
}

func (d *CompactionDAG) tools() []frameworktypes.Tool {
	if d.opts.Tools == nil {
		return nil
	}
	return d.opts.Tools()
}

func (d *CompactionDAG) chapter2MaxTokens() int {
	if d.opts.Chapter2MaxTokens > 0 {
		return d.opts.Chapter2MaxTokens
	}
	return PrefixReplayMaxTokens
}

// frameCarryTokens 返回帧摘要传递上限（≤0 → DefaultFrameCarryTokens）。
func (d *CompactionDAG) frameCarryTokens() int {
	if d.opts.FrameCarryTokens > 0 {
		return d.opts.FrameCarryTokens
	}
	return DefaultFrameCarryTokens
}

// replayInputTokens 返回分片重放的片预算（≤0 → 不分片）。
func (d *CompactionDAG) replayInputTokens() int { return d.opts.ReplayInputTokens }

// segmentID 生成帧 SegmentID（会话溯源前缀，与 controller 同风格）。
func (d *CompactionDAG) segmentID() string {
	prefix := d.opts.SegmentPrefix
	if prefix == "" {
		prefix = "compact"
	}
	millis := time.Now().UnixMilli()
	if d.opts.SessionIDProvider != nil {
		if sessionID := d.opts.SessionIDProvider(); sessionID != "" {
			return fmt.Sprintf("%s-%s-%d", prefix, sessionID, millis)
		}
	}
	return fmt.Sprintf("%s-%d", prefix, millis)
}
