// Package search 提供超长上下文的历史记录检索（2026-08-07）。
//
// 背景：会话压缩只渲染 CompactStack 栈顶摘要，窗口外轮次在模型请求中只剩
// 摘要（递归内嵌）——久远但相关的细节丢失，且模型没有按需读回的入口。
// 本包把「用压缩栈做语义索引，在索引范围内查聊天记录」做成显式一步：
//
//	查询 → memory.Select 在 CompactStack 全部帧上选相关帧（词法索引）
//	→ 按命中帧 [From..To] 单元范围从事件流读回真实聊天记录
//	→ token 预算内有界返回（帧命中 + 范围 + 记录 + 相关性排序）
//
// 事件流（append-only，含已压缩轮次）是真相源；帧 From/To 是累计 ChatQueue
// 单元索引，与 CompleteEventUnits 的单元下标近似对齐（clamp 到边界）。
// 无压缩栈时退化为尾部扫描兜底（短会话可检索；长会话可检索性有限，
// 见 MaxFallbackScanUnits——提示文案明示）。
package search

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/memory"
	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/tokens"
	"github.com/RedHuang-0622/seelex/contract/dto"
	model "github.com/RedHuang-0622/seelex/contract/protocol"
	storecontext "github.com/RedHuang-0622/seelex/store/context"
)

// ── 上限（token 预算硬上限 / 命中数上限 / 兜底扫描轮数）──────────────

const (
	// DefaultLimit 是默认最多命中数（与 memory.DefaultOptions 一致）。
	DefaultLimit = 3
	// MaxLimit 是命中数硬上限（防止工具/UI 一次拉回过多帧记录）。
	MaxLimit = 20
	// DefaultTokenBudget 是默认记录 token 总预算。
	DefaultTokenBudget = 4000
	// MaxTokenBudget 是记录 token 预算硬上限（调用方 clamp，绝不无界）。
	MaxTokenBudget = 12000
	// MaxFallbackScanUnits 是无压缩栈兜底时最多扫描的尾部单元数：
	// 未压缩的超长会话久远内容可检索性有限（与提示文案一致）。
	MaxFallbackScanUnits = 300
	// maxRecordChars 是单条记录正文的字符截断上限（防单条超大内容
	// 撑爆展示；与 limits.evidence_chars 默认同量级）。
	maxRecordChars = 800
)

var (
	// ErrEmptyQuery 是空查询拒绝（检索必须有关键词）。
	ErrEmptyQuery = errors.New("search: query is required")
	// ErrNoEventSource 是事件源未装配（无持久化事件库时检索不可用）。
	ErrNoEventSource = errors.New("search: event source is unavailable")
)

// 结果形状的唯一事实源在 contract/dto（P3：契约层不依赖引擎上下文包）；
// 这里保留既有名字，调用点与类型身份零改动。
type ChatRecord = dto.SearchRecord
type Hit = dto.SearchHit
type Result = dto.SearchResult

// Options 是检索参数；零值字段回退默认（见 Search 内 applyDefaults）。
type Options struct {
	// Limit 最多返回的命中数（0 → DefaultLimit；> MaxLimit clamp）。
	Limit int
	// TokenBudget 全部命中的记录总预算（0 → DefaultTokenBudget；
	// > MaxTokenBudget clamp——硬上限）。
	TokenBudget int
}

func (o *Options) applyDefaults() {
	if o.Limit <= 0 {
		o.Limit = DefaultLimit
	}
	if o.Limit > MaxLimit {
		o.Limit = MaxLimit
	}
	if o.TokenBudget <= 0 {
		o.TokenBudget = DefaultTokenBudget
	}
	if o.TokenBudget > MaxTokenBudget {
		o.TokenBudget = MaxTokenBudget
	}
}

// StackSource 是压缩栈（语义索引）的窄读取源。
// store.SessionContextStore / seelexctx.CompactStackStore 均满足。
type StackSource interface {
	CompactStackSnapshot() []storecontext.CompactFrame
}

// EventSource 是会话事件流读取源（append-only 真相源，按 Seq 升序）。
type EventSource interface {
	LoadAllEvents(ctx context.Context) ([]model.TranscriptEvent, error)
}

// ProjectEventRouter is the consumer-owned project-scoped event read surface.
type ProjectEventRouter interface {
	Workspace() string
	LoadEventTailWorkspace(projectID, sessionID string, tokenBudget, maxUnits int) ([]model.TranscriptEvent, error)
}

// routerEventSource 把 store.Router 绑定到项目/会话后作为事件源。
type routerEventSource struct {
	router    ProjectEventRouter
	projectID string
	sessionID string
	notFound  func(error) bool
}

// NewRouterEventSource 构造绑定到 Router 的事件源（LoadEventTailWorkspace
// 全量读取，MaxInt 双限 = selectEventTail 无截断）。
func NewRouterEventSource(router ProjectEventRouter, projectID, sessionID string, notFound func(error) bool) EventSource {
	return routerEventSource{router: router, projectID: projectID, sessionID: sessionID, notFound: notFound}
}

// LoadAllEvents 实现 EventSource。会话不存在（尚无事件库）→ 空流不报错
// （与 DurableHistory.LoadEventTail 的会话未找到语义一致：两边都走
// `store.IsSessionNotFound` 这一处判据，不再各自维护副本）。
func (source routerEventSource) LoadAllEvents(ctx context.Context) ([]model.TranscriptEvent, error) {
	events, err := source.router.LoadEventTailWorkspace(source.projectID, source.sessionID, math.MaxInt, math.MaxInt)
	if err != nil && source.notFound != nil && source.notFound(err) {
		return []model.TranscriptEvent{}, nil
	}
	return events, err
}

// Searcher 在压缩栈帧（语义索引）上执行历史检索。依赖全部构造注入
// （StackSource / EventSource），无隐式全局状态。
type Searcher struct {
	stack  StackSource
	events EventSource
}

// New 构造检索器。stack 为 nil 时退化为尾部扫描兜底；events 为 nil 时
// Search 显式报错（ErrNoEventSource）。
func New(stack StackSource, events EventSource) *Searcher {
	return &Searcher{stack: stack, events: events}
}

// Search 执行检索：空查询拒绝；无压缩栈 → 尾部扫描兜底；命中帧按
// [From..To] 单元范围从事件流读回真实聊天记录，token 预算内有界返回。
func (s *Searcher) Search(ctx context.Context, query string, opts Options) (Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return Result{}, ErrEmptyQuery
	}
	opts.applyDefaults()
	if s == nil || s.events == nil {
		return Result{}, ErrNoEventSource
	}
	allEvents, err := s.events.LoadAllEvents(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("search: load events: %w", err)
	}
	units := model.CompleteEventUnits(allEvents)
	result := Result{Query: query, TotalUnits: len(units), Budget: opts.TokenBudget}
	if len(units) == 0 {
		result.Note = "会话暂无事件记录（历史检索不可用）"
		return result, nil
	}
	var frames []storecontext.CompactFrame
	if s.stack != nil {
		frames = s.stack.CompactStackSnapshot()
	}
	if len(frames) > 0 {
		// 索引路径：压缩栈全部帧作为候选，memory.Select 选相关帧。
		result.IndexedFrames = len(frames)
		candidates := make([]memory.Candidate, 0, len(frames))
		for _, frame := range frames {
			candidates = append(candidates, memory.Candidate{
				SegmentID: frame.SegmentID, Summary: frame.Summary,
				Evidence: frame.Evidence, From: frame.From, To: frame.To,
				EventFrom: frame.EventFrom, EventTo: frame.EventTo,
			})
		}
		selected := memory.Select(query, candidates, memory.Options{Limit: opts.Limit})
		result.Hits, result.Truncated = collectHits(query, selected, units, opts.TokenBudget)
		return result, nil
	}
	// 兜底路径：无压缩栈 → 尾部扫描。短会话全部轮次可检索；长会话
	// 只扫描最近 MaxFallbackScanUnits 轮（提示文案明示可检索性有限）。
	result.Note = "历史未压缩：按最近轮次全量扫描检索（可检索性有限）"
	scanStart := 0
	if len(units) > MaxFallbackScanUnits {
		scanStart = len(units) - MaxFallbackScanUnits
		result.Note = fmt.Sprintf("历史未压缩：仅扫描最近 %d 个轮次（可检索性有限）", MaxFallbackScanUnits)
	}
	candidates := make([]memory.Candidate, 0, len(units)-scanStart)
	for index := scanStart; index < len(units); index++ {
		candidates = append(candidates, memory.Candidate{
			SegmentID: fmt.Sprintf("unit-%d", index),
			Summary:   renderUnitSummaryLine(units[index]),
			From:      index,
			To:        index,
		})
	}
	selected := memory.Select(query, candidates, memory.Options{Limit: opts.Limit})
	result.Hits, result.Truncated = collectHits(query, selected, units, opts.TokenBudget)
	return result, nil
}

// collectHits 把选中的候选帧逐一读回真实记录：共享 token 预算累计，
// 预算耗尽停止（Result.Truncated）；帧内记录被截断（Hit.Truncated）。
func collectHits(query string, selected []memory.Candidate, units [][]model.TranscriptEvent, budget int) ([]Hit, bool) {
	hits := make([]Hit, 0, len(selected))
	remaining := budget
	truncated := false
	for index, candidate := range selected {
		if remaining <= 0 {
			truncated = true
			break
		}
		hit := buildHit(query, candidate, units, remaining)
		hits = append(hits, hit)
		if hit.Truncated {
			// 帧记录被截断 = 剩余预算不足以完整渲染该帧 → 后续命中丢弃。
			truncated = true
			remaining = 0
			continue
		}
		remaining -= sumRecordEstimate(hit.Records)
		if remaining <= 0 && index < len(selected)-1 {
			truncated = true // 预算刚好耗尽：后续命中被丢弃
		}
	}
	return hits, truncated
}

// buildHit 按帧区间读回真实聊天记录。
//
// 区间定位有两条路，优先级不可颠倒：
//
//  1. 帧声明了 EventSeq 区间（EventTo > 0）→ 按 Seq 在这份 units 上**反查**单元下标。
//     这是权威路径：装配层压缩（回合开始前那条，也是实际最常发生的那条）手里的
//     事实就是 EventSeq；而 units 由 CompleteEventUnits 切出，会跳过孤儿 tool 与
//     未知角色，因此"事件下标 → 单元下标"**不是减法**，只能按 Seq 查找。
//     反查不到 → 空命中 + 在 Summary 里写明原因，绝不 clamp 猜。
//  2. 未声明（控制器帧 / 真空区帧 / 旧记录）→ 沿用 From/To 累计单元索引 + clamp。
//
// 为什么第 1 条不许回退到 clamp：clamp 会产出一个"看起来合法但指向别的轮次"的
// 区间，读回的是错的原文却不报错——静默错读比空命中糟得多。
// Summary 截断到展示长度；Score 复用 memory.Select 同款打分。
func buildHit(query string, candidate memory.Candidate, units [][]model.TranscriptEvent, budget int) Hit {
	hit := Hit{
		SegmentID: candidate.SegmentID,
		From:      candidate.From,
		To:        candidate.To,
		Summary:   truncateContent(candidate.Summary),
		Score:     memory.Score(query, candidate),
	}
	var from, to int
	if candidate.EventTo > 0 {
		mapped, ok := eventSeqUnitRange(units, candidate.EventFrom, candidate.EventTo)
		if !ok {
			hit.Summary = truncateContent(candidate.Summary) +
				fmt.Sprintf("\n[区间无法定位：帧声明事件序号 %d..%d，但当前事件流里找不到对应单元；不猜测、不 clamp]",
					candidate.EventFrom, candidate.EventTo)
			return hit
		}
		from, to = mapped.from, mapped.to
	} else {
		from = clampIndex(candidate.From, len(units))
		to = clampIndex(candidate.To, len(units))
		if to < from {
			return hit // 帧范围在事件流之外（clamp 后倒置）→ 空命中
		}
	}
	hit.From, hit.To = from, to
	hit.Units = to - from + 1
	remaining := budget
	index := from
	for ; index <= to && remaining > 0; index++ {
		records, truncated := renderUnitRecords(units[index], remaining)
		hit.Records = append(hit.Records, records...)
		if truncated {
			break // 单元内记录超出剩余预算
		}
	}
	hit.Truncated = index <= to // 范围内还有单元未渲染 = 预算耗尽截断
	return hit
}

// unitRange 是一次 EventSeq → 单元下标的映射结果（含端点）。
type unitRange struct {
	from int
	to   int
}

// eventSeqUnitRange 在 units 上按 EventSeq 区间反查单元下标区间。
//
// from = 首个"最大 Seq >= eventFrom"的单元；to = 末个"最小 Seq <= eventTo"的单元。
// Seq 为 0 的事件（合成事件，见 TranscriptPrefixRange 的同款跳过规则）不参与定界。
// 任一端找不到 → ok=false，调用方必须空命中，不得回退 clamp。
func eventSeqUnitRange(units [][]model.TranscriptEvent, eventFrom, eventTo uint64) (unitRange, bool) {
	if eventTo == 0 || len(units) == 0 {
		return unitRange{}, false
	}
	out := unitRange{from: -1, to: -1}
	for index, unit := range units {
		low, high := unitSeqBounds(unit)
		if low == 0 && high == 0 {
			continue // 整个单元都没有真实事件序号，无法定界
		}
		if out.from < 0 && high >= eventFrom {
			out.from = index
		}
		if low <= eventTo {
			out.to = index
		}
	}
	if out.from < 0 || out.to < 0 || out.to < out.from {
		return unitRange{}, false
	}
	return out, true
}

// unitSeqBounds 返回一个单元内非零 EventSeq 的最小/最大值（都为 0 = 无真实序号）。
func unitSeqBounds(unit []model.TranscriptEvent) (low, high uint64) {
	for _, event := range unit {
		if event.Seq == 0 {
			continue
		}
		if low == 0 || event.Seq < low {
			low = event.Seq
		}
		if event.Seq > high {
			high = event.Seq
		}
	}
	return low, high
}

// renderUnitRecords 渲染一个事件单元为记录列表（token 预算内；预算耗尽
// 返回 truncated=true，单元内剩余事件不再渲染）。
func renderUnitRecords(unit []model.TranscriptEvent, budget int) ([]ChatRecord, bool) {
	var records []ChatRecord
	used := 0
	for _, event := range unit {
		record := recordFromEvent(event)
		cost := estimateRecord(record)
		if used+cost > budget {
			return records, true
		}
		records = append(records, record)
		used += cost
	}
	return records, false
}

// recordFromEvent 把一个事件渲染为聊天记录：用户输入、assistant 文本/
// 工具调用名、tool 结果（正文截断 + 引用句柄，ResultRef 缺失时按调用
// ID 推导 result 句柄，与 gap 证据提取同源）。
func recordFromEvent(event model.TranscriptEvent) ChatRecord {
	record := ChatRecord{Role: event.Role, Content: truncateContent(event.Content)}
	switch event.Role {
	case "assistant":
		if len(event.ToolCalls) > 0 {
			names := make([]string, 0, len(event.ToolCalls))
			for _, call := range event.ToolCalls {
				names = append(names, call.Name)
			}
			record.ToolName = strings.Join(names, ", ")
		}
	case "tool":
		record.ToolName = event.Name
		record.ResultRef = event.ResultRef
		if record.ResultRef == "" && event.ToolCallID != "" {
			record.ResultRef = model.WireToolResultRef(event.ToolCallID)
		}
	}
	return record
}

// renderUnitSummaryLine 渲染一个事件单元的单行摘要（兜底候选帧的
// Summary：用户输入前 80 字符 + 工具名，与控制器 renderUnitLine 同风格）。
func renderUnitSummaryLine(unit []model.TranscriptEvent) string {
	var builder strings.Builder
	for _, event := range unit {
		switch {
		case event.Role == "user" && event.Content != "":
			content := event.Content
			if len(content) > 80 {
				content = content[:80] + "..."
			}
			builder.WriteString("- 用户: ")
			builder.WriteString(content)
			builder.WriteByte('\n')
		case event.Role == "assistant":
			for _, call := range event.ToolCalls {
				builder.WriteString("- 工具调用: ")
				builder.WriteString(call.Name)
				builder.WriteByte('\n')
			}
		case event.Role == "tool" && event.Name != "":
			builder.WriteString("- 工具结果: ")
			builder.WriteString(event.Name)
			builder.WriteByte('\n')
		}
	}
	return strings.TrimSpace(builder.String())
}

// estimateRecord 估算一条记录的 token（脚本感知估算 + 固定开销，
// 与 ConservativeTokenCounter 同源）。
func estimateRecord(record ChatRecord) int {
	total := record.Role + record.Content + record.ToolName + record.ResultRef
	return tokens.Count(total) + 1
}

// sumRecordEstimate 汇总记录 token 估算。
func sumRecordEstimate(records []ChatRecord) int {
	total := 0
	for _, record := range records {
		total += estimateRecord(record)
	}
	return total
}

// truncateContent 把正文截断到 maxRecordChars（末尾加省略标记）。
func truncateContent(content string) string {
	if len(content) <= maxRecordChars {
		return content
	}
	return content[:maxRecordChars] + "..."
}

// clampIndex 把单元索引 clamp 到 [0, total-1]（total=0 → 返回 0，
// 由调用方范围倒置守卫兜底）。
func clampIndex(index, total int) int {
	if index < 0 {
		return 0
	}
	if index >= total {
		return total - 1
	}
	return index
}
