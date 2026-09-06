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
//     确定性折叠（summary_source=local，不消耗模型 token）；
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
	// Chapter 2 走本地折叠，不消耗模型 token）。
	History []frameworktypes.Message
	// Kind 决定本地折叠的 Current Work 文案（溢出 / 真空区）。
	Kind LocalFoldKind
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
	// Summarizer 是前缀重放厚摘要器（nil → 恒本地折叠）。
	Summarizer PrefixReplaySummarizer
	// SystemPrompt/Tools 是前缀重放字节素材提供者（与真实请求同源时才有
	// 前缀命中价值；nil → 重放请求不带对应素材）。
	SystemPrompt func() string
	Tools        func() []frameworktypes.Tool
	// Chapter2MaxTokens 是厚摘要输出预算（≤0 → PrefixReplayMaxTokens；
	// QuickChat 通道未透传预算时仅作记录）。
	Chapter2MaxTokens int
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
	frame         sessionstore.CompactFrame
	started       map[string]bool
}

// Execute 运行压缩 DAG 并返回拼装完成的 CompactFrame（不含 PushCompact；
// 压栈与去重由调用方保留，见文件头注释）。失败时按详设 §4.5 逐级兜底：
// Chapter 2 失败 → 本地折叠；装配/未执行失败 → 串行化兜底。
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
		} else if len(state.started) > 0 {
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
	state.started[id] = true
}

// selectRangeNode：溢出单元切分（纯计算；request 首尾在 merge 按覆盖范围
// 推导，调用方显式传入的 RequestFrom/To 优先）。
func (d *CompactionDAG) selectRangeNode(state *compactionDAGState) func(context.Context) error {
	return func(context.Context) error {
		markStarted(state, "select_range")
		state.overflow = chatUnits(state.input.Messages)
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

// chapter2Node：前缀重放厚摘要（一次重试）→ 失败/无重放素材回退本地折叠。
func (d *CompactionDAG) chapter2Node(state *compactionDAGState) func(context.Context) error {
	return func(ctx context.Context) error {
		markStarted(state, "chapter2_thick")
		if d.opts.Summarizer != nil && len(state.input.History) > 0 {
			request := ReplayRequest{
				SystemPrompt: d.systemPrompt(),
				History:      append([]frameworktypes.Message(nil), state.input.History...),
				Tools:        d.tools(),
				MaxTokens:    d.chapter2MaxTokens(),
			}
			for attempt := 0; attempt < 2; attempt++ {
				result, err := d.opts.Summarizer.Summarize(ctx, request)
				if err == nil && strings.TrimSpace(result.Chapter2) != "" {
					state.chapter2 = normalizeReplayChapter2(result.Chapter2)
					state.summarySource = CompactSummarySourceReplay
					return nil
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
			}
			// 两次尝试均失败 → 本地折叠兜底（不消耗模型 token 的确定性路径）。
		}
		state.chapter2 = LocalChapter2(LocalFoldOptions{
			Overflow:  state.overflow,
			UnitCount: state.unitCount,
			Kind:      state.input.Kind,
			PrevTop:   state.prevTop,
			Record:    state.record,
		})
		state.summarySource = CompactSummarySourceLocal
		return nil
	}
}

// mergeNode：拼装 CompactFrame（SegmentID/From/To/Evidence + 链锚字段 +
// 两章节 Summary；复用 controller/gap 的覆盖语义）。
func (d *CompactionDAG) mergeNode(state *compactionDAGState) func(context.Context) error {
	return func(context.Context) error {
		markStarted(state, "merge_frame")
		count := state.unitCount
		from, to := 0, count-1
		if state.prevTop != nil {
			from = state.prevTop.From
			to = state.prevTop.To + count
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
			AnchorSource:  state.anchorSource,
			Evidence:      overflowEvidence(state.overflow, state.record),
			CompressedAt:  time.Now(),
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
