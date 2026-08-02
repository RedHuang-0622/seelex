// seelexToolResultProcessor 是 seelex 的 ToolResultProcessor（plan.md §3.5 /
// 架构文档 4.6(3)）：超大工具结果在进入 working history 前先归档为
// result_ref，模型只看到省略警告与读取指引（read_tool_result 分页语义）。
package seelexctx

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"
)

// ToolResultOmittedPrefix 是超大工具结果省略块的起始标记。
const ToolResultOmittedPrefix = "<seelex-tool-result-omitted>"

// ToolResultOmittedMarker 是省略块的结束标记。
const ToolResultOmittedMarker = "</seelex-tool-result-omitted>"

// frameworkToolOutputTruncatedMarker 框架侧截断标记：结果带该后缀同样视为
// 超大（与 session loop truncateResult 语义一致）。
const frameworkToolOutputTruncatedMarker = "\n...[truncated]"

// ToolResultArchiver 归档不可变工具结果，返回结果引用（result_ref）。
// 同一调用 ID 重复归档应返回同一引用（幂等），由实现保证。
type ToolResultArchiver interface {
	Store(ctx context.Context, callID, tool, raw string) (string, error)
}

// ToolResultArchiverFunc 适配函数到 ToolResultArchiver。
type ToolResultArchiverFunc func(context.Context, string, string, string) (string, error)

// Store 实现 ToolResultArchiver。
func (f ToolResultArchiverFunc) Store(ctx context.Context, callID, tool, raw string) (string, error) {
	return f(ctx, callID, tool, raw)
}

// TurnArchiver 持久化压缩轮次的原文消息，返回读回句柄（ref）。
// 压缩把窗口外轮次折叠为 Summary 后，原文经此归档到持久存储，
// 模型可经 read_compressed_turn 工具读回——压缩丢失可逆，减少幻觉。
// 同一 segmentID 重复归档应返回同一引用（幂等），由实现保证。
type TurnArchiver interface {
	StoreTurn(ctx context.Context, segmentID string, messages []types.Message) (string, error)
}

// TurnArchiverFunc 适配函数到 TurnArchiver。
type TurnArchiverFunc func(context.Context, string, []types.Message) (string, error)

// StoreTurn 实现 TurnArchiver。
func (f TurnArchiverFunc) StoreTurn(ctx context.Context, segmentID string, messages []types.Message) (string, error) {
	return f(ctx, segmentID, messages)
}

// InMemoryToolResultArchiver 是内存态归档（无持久后端时的兜底）：
// 以调用 ID 为键，重复归档幂等返回同一引用。
type InMemoryToolResultArchiver struct {
	mu      sync.Mutex
	results map[string]string // callID → raw
	seq     atomic.Uint64
}

// NewInMemoryToolResultArchiver 创建内存归档器。
func NewInMemoryToolResultArchiver() *InMemoryToolResultArchiver {
	return &InMemoryToolResultArchiver{results: make(map[string]string)}
}

// Store 实现 ToolResultArchiver（按调用 ID 幂等）。
func (a *InMemoryToolResultArchiver) Store(_ context.Context, callID, tool, raw string) (string, error) {
	if a == nil {
		return "", fmt.Errorf("seelexctx: archiver is unavailable")
	}
	if callID == "" {
		callID = fmt.Sprintf("anon-%d", a.seq.Add(1))
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.results[callID]; ok {
		return "result:" + callID, nil
	}
	a.results[callID] = raw
	return "result:" + callID, nil
}

// Read 按引用读取归档内容（供 read_tool_result 语义的测试/装配使用）。
func (a *InMemoryToolResultArchiver) Read(ref string) (string, bool) {
	if a == nil {
		return "", false
	}
	callID := strings.TrimPrefix(ref, "result:")
	a.mu.Lock()
	defer a.mu.Unlock()
	raw, ok := a.results[callID]
	return raw, ok
}

// seelexToolResultProcessor 在结果进入 working history 前筛选：
// 超大 → 归档 + 省略警告（result_ref 可被 read_tool_result 分页读取）；
// 正常 → 原样透传；工具错误 → 错误 JSON 透传（保持错误可见）。
type seelexToolResultProcessor struct {
	limit   int
	archive ToolResultArchiver
}

// NewToolResultProcessor 构造结果处理器。limit ≤ 0 时使用框架默认
// MaxToolResultChars；archive 为 nil 时使用内存归档器（会话内可读）。
func NewToolResultProcessor(limit int, archive ToolResultArchiver) seelectx.ToolResultProcessor {
	if limit <= 0 {
		limit = DefaultMaxToolResultChars
	}
	if archive == nil {
		archive = NewInMemoryToolResultArchiver()
	}
	return seelexToolResultProcessor{limit: limit, archive: archive}
}

// Process 实现 seelectx.ToolResultProcessor。
func (p seelexToolResultProcessor) Process(ctx context.Context, result seelectx.ToolResult) (seelectx.ToolResultView, error) {
	if result.Err != nil {
		// 错误结果原样透传（保持错误对模型可见，不省略）。
		return seelectx.ToolResultView{Content: result.Raw}, nil
	}
	if !IsOversizedToolResult(result.Raw, p.limit) {
		return seelectx.ToolResultView{Content: result.Raw}, nil
	}
	ref, err := p.archive.Store(ctx, result.CallID, result.Name, result.Raw)
	if err != nil {
		return seelectx.ToolResultView{}, fmt.Errorf("seelexctx: archive tool result %q: %w", result.Name, err)
	}
	return seelectx.ToolResultView{Content: OversizedToolResultWarning(result.Name, ref)}, nil
}

// IsOversizedToolResult 判断工具结果是否超过单条预算（字符数上限或
// 框架截断标记）。
func IsOversizedToolResult(content string, limit int) bool {
	return len(content) > limit || strings.HasSuffix(content, frameworkToolOutputTruncatedMarker)
}

// OversizedToolResultWarning 生成省略块：显式拒绝在省略内容上推断事实，
// 指向 read_tool_result 分页/过滤读取。
func OversizedToolResultWarning(name, resultRef string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "tool"
	}
	var builder strings.Builder
	builder.WriteString(ToolResultOmittedPrefix + "\n")
	builder.WriteString("tool=")
	builder.WriteString(name)
	builder.WriteByte('\n')
	if resultRef != "" {
		builder.WriteString("result_ref=")
		builder.WriteString(resultRef)
		builder.WriteByte('\n')
	}
	builder.WriteString("The result exceeded the provider-context item budget; raw content was not included.\n")
	builder.WriteString("Do not infer facts from omitted content. Use read_tool_result with pagination or filtering, or issue a narrower read-only query.\n")
	builder.WriteString(ToolResultOmittedMarker)
	return builder.String()
}
