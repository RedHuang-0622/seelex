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
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"strings"

	"github.com/RedHuang-0622/seelex/seelexctx/memory"
	"github.com/RedHuang-0622/seelex/sessionstore"
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

// ChatRecord 是检索到的单条聊天记录（命中帧范围内事件的真实内容）。
type ChatRecord struct {
	Role      string `json:"role"`                 // user | assistant | tool
	Content   string `json:"content,omitempty"`    // 截断后的正文
	ToolName  string `json:"tool_name,omitempty"`  // assistant 工具调用名 / tool 结果名
	ResultRef string `json:"result_ref,omitempty"` // 工具结果句柄（read_tool_result 可读回）
	Truncated bool   `json:"truncated,omitempty"`  // 该条被 token 预算截断
}

// Hit 是一次检索命中的压缩帧：段标识 + 单元范围 + 摘要 + 真实聊天记录。
// Score 是词法相关性分数（仅用于排序展示，不作为事实）。
type Hit struct {
	SegmentID string       `json:"segment_id"`
	From      int          `json:"from"`
	To        int          `json:"to"`
	Summary   string       `json:"summary"`
	Score     float64      `json:"score"`
	Units     int          `json:"units"`     // 实际读回的完整协议单元数
	Records   []ChatRecord `json:"records"`   // 帧范围内的真实聊天记录
	Truncated bool         `json:"truncated"` // 预算耗尽：该帧记录被截断 / 后续命中被丢弃
}

// Result 是一次检索的完整结果（工具与 GUI 的权威返回）。
type Result struct {
	Query         string `json:"query"`
	Hits          []Hit  `json:"hits"`
	IndexedFrames int    `json:"indexed_frames"` // 压缩栈帧总数（语义索引规模）
	TotalUnits    int    `json:"total_units"`    // 事件流完整协议单元总数
	Budget        int    `json:"budget"`         // 实际生效的 token 预算
	Truncated     bool   `json:"truncated"`      // 预算耗尽：部分命中未返回
	Note          string `json:"note,omitempty"` // 边界提示（未压缩兜底等）
}

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

// StackSource 是压缩栈（语义索引）读取源。
// sessionstore.SessionContextStore / seelexctx.CompactStackStore 均满足。
type StackSource interface {
	Snapshot() sessionstore.SessionContextRecord
}

// EventSource 是会话事件流读取源（append-only 真相源，按 Seq 升序）。
type EventSource interface {
	LoadAllEvents(ctx context.Context) ([]sessionstore.Event, error)
}

// routerEventSource 把 sessionstore.Router 绑定到项目/会话后作为事件源。
type routerEventSource struct {
	router    *sessionstore.Router
	projectID string
	sessionID string
}

// NewRouterEventSource 构造绑定到 Router 的事件源（LoadEventTailWorkspace
// 全量读取，MaxInt 双限 = selectEventTail 无截断）。
func NewRouterEventSource(router *sessionstore.Router, projectID, sessionID string) EventSource {
	return routerEventSource{router: router, projectID: projectID, sessionID: sessionID}
}

// LoadAllEvents 实现 EventSource。会话不存在（尚无事件库）→ 空流不报错
// （与 DurableHistory.LoadEventTail 的会话未找到语义一致）。
func (source routerEventSource) LoadAllEvents(ctx context.Context) ([]sessionstore.Event, error) {
	events, err := source.router.LoadEventTailWorkspace(source.projectID, source.sessionID, math.MaxInt, math.MaxInt)
	if err != nil && isSessionNotFound(err) {
		return []sessionstore.Event{}, nil
	}
	return events, err
}

// isSessionNotFound 判断会话不存在错误（fs.ErrNotExist / sql.ErrNoRows）。
func isSessionNotFound(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, sql.ErrNoRows)
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
	units := sessionstore.CompleteEventUnits(allEvents)
	result := Result{Query: query, TotalUnits: len(units), Budget: opts.TokenBudget}
	if len(units) == 0 {
		result.Note = "会话暂无事件记录（历史检索不可用）"
		return result, nil
	}
	var record sessionstore.SessionContextRecord
	if s.stack != nil {
		record = s.stack.Snapshot()
	}
	if len(record.CompactStack) > 0 {
		// 索引路径：压缩栈全部帧作为候选，memory.Select 选相关帧。
		result.IndexedFrames = len(record.CompactStack)
		candidates := make([]memory.Candidate, 0, len(record.CompactStack))
		for _, frame := range record.CompactStack {
			candidates = append(candidates, memory.Candidate{
				SegmentID: frame.SegmentID, Summary: frame.Summary,
				Evidence: frame.Evidence, From: frame.From, To: frame.To,
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
func collectHits(query string, selected []memory.Candidate, units [][]sessionstore.Event, budget int) ([]Hit, bool) {
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

// buildHit 按帧 [From..To] 单元范围读回真实聊天记录。单元索引与帧范围
// 近似对齐：越界 clamp 到事件流边界；clamp 后范围倒置 → 空命中（不报错）。
// Summary 截断到展示长度；Score 复用 memory.Select 同款打分。
func buildHit(query string, candidate memory.Candidate, units [][]sessionstore.Event, budget int) Hit {
	hit := Hit{
		SegmentID: candidate.SegmentID,
		From:      candidate.From,
		To:        candidate.To,
		Summary:   truncateContent(candidate.Summary),
		Score:     memory.Score(query, candidate),
	}
	from := clampIndex(candidate.From, len(units))
	to := clampIndex(candidate.To, len(units))
	if to < from {
		return hit // 帧范围在事件流之外（clamp 后倒置）→ 空命中
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

// renderUnitRecords 渲染一个事件单元为记录列表（token 预算内；预算耗尽
// 返回 truncated=true，单元内剩余事件不再渲染）。
func renderUnitRecords(unit []sessionstore.Event, budget int) ([]ChatRecord, bool) {
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
func recordFromEvent(event sessionstore.Event) ChatRecord {
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
			record.ResultRef = "result:" + event.ToolCallID
		}
	}
	return record
}

// renderUnitSummaryLine 渲染一个事件单元的单行摘要（兜底候选帧的
// Summary：用户输入前 80 字符 + 工具名，与控制器 renderUnitLine 同风格）。
func renderUnitSummaryLine(unit []sessionstore.Event) string {
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

// estimateRecord 估算一条记录的 token（保守公式 (len+2)/3 + 固定开销，
// 与 ConservativeTokenCounter 同源）。
func estimateRecord(record ChatRecord) int {
	total := len(record.Role) + len(record.Content) + len(record.ToolName) + len(record.ResultRef)
	return (total+2)/3 + 1
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
