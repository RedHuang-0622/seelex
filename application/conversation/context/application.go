// Package context owns explicit context-compaction application orchestration.
// Compression algorithms, persistence and gate decisions remain in
// backend/engine/context.
package context

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/RedHuang-0622/seelex/contract/protocol"
	context_runtime "github.com/RedHuang-0622/seelex/kernel/context"
)

// Deps supplies session resolution, compaction gate and Runtime compaction.
type Deps struct {
	CurrentSessionID func(context.Context) string
	Acquire          func(context.Context, string) error
	Release          func(string)
	Compact          func(context.Context, string) (context_runtime.CompactResult, error)
}

// Application owns explicit compaction request and receipt shaping.
type Application struct {
	deps Deps
}

// New constructs the context-compaction application.
func New(deps Deps) *Application {
	return &Application{deps: deps}
}

// GateTiming is one compaction gate and its elapsed milliseconds.
type GateTiming struct {
	Gate      string `json:"gate"`
	ElapsedMS int    `json:"elapsed_ms"`
}

// Result is the public compaction result shared by command and tool paths.
type Result struct {
	Compacted       bool         `json:"compacted"`
	Recorded        bool         `json:"recorded,omitempty"`
	Scheduled       bool         `json:"scheduled,omitempty"`
	NoEpoch         bool         `json:"no_epoch,omitempty"`
	Version         uint64       `json:"version,omitempty"`
	Reason          string       `json:"reason,omitempty"`
	MessagesBefore  int          `json:"messages_before,omitempty"`
	EstimatedTokens int          `json:"estimated_tokens,omitempty"`
	ComparedTokens  int          `json:"compared_tokens,omitempty"`
	SoftThreshold   int          `json:"soft_threshold,omitempty"`
	HardThreshold   int          `json:"hard_threshold,omitempty"`
	Gates           []GateTiming `json:"gates,omitempty"`
	CompactedAt     string       `json:"compacted_at,omitempty"`
	MessageFrom     string       `json:"message_from,omitempty"`
	MessageTo       string       `json:"message_to,omitempty"`
	EventFrom       uint64       `json:"event_from,omitempty"`
	EventTo         uint64       `json:"event_to,omitempty"`
	Origin          string       `json:"origin,omitempty"`
	FrameRef        string       `json:"frame_ref,omitempty"`
	FrameBytes      int          `json:"frame_bytes,omitempty"`
	FrameTokens     int          `json:"frame_tokens,omitempty"`
	Failure         string       `json:"failure,omitempty"`
	Note            string       `json:"note"`
}

// CompactionReasonLabel renders a user-readable compaction reason.
func CompactionReasonLabel(reason string) string {
	switch reason {
	case "context_budget":
		return "上下文预算"
	case "context_budget_autonomous":
		return "上下文预算（自主压缩）"
	case "large_tool_output":
		return "工具输出过大"
	case "":
		return "上下文压缩"
	default:
		return reason
	}
}

// FailureReasonLabel renders a known failure literal while preserving numeric
// facts.
func FailureReasonLabel(failure string) string {
	trimmed := strings.TrimSpace(failure)
	if trimmed == "" {
		return ""
	}
	literal, facts, _ := strings.Cut(trimmed, " ")
	label := ""
	switch literal {
	case "no_model_summary":
		label = "拿不到模型读后感（摘要器未装配或重放调用失败）"
	case "ineffective_compact":
		label = "压缩换不来余量（压缩落点仍够不到判据线）"
	default:
		return trimmed
	}
	if facts = strings.TrimSpace(facts); facts != "" {
		return label + "；" + facts
	}
	return label
}

// CompactionRecordNote renders a recorded compaction receipt.
func CompactionRecordNote(result Result) string {
	var builder strings.Builder
	if result.NoEpoch {
		builder.WriteString("会话没有在飞回合（冷加载或刚清空），已按会话级显式压缩立即执行" +
			"（压缩已装载的上下文并落记录，无需下一条消息）：")
	}
	builder.WriteString("已压缩上下文：v")
	builder.WriteString(strconv.FormatUint(result.Version, 10))
	builder.WriteString("（")
	builder.WriteString(CompactionReasonLabel(result.Reason))
	builder.WriteString("）")
	if label := CompactionRangeLabel(result); label != "" {
		builder.WriteString("，被压区间 ")
		builder.WriteString(label)
	}
	fmt.Fprintf(&builder, "，装配后估算 %d tokens（判据量 %d）", result.EstimatedTokens, result.ComparedTokens)
	if result.Origin != "" {
		builder.WriteString("，来源 " + result.Origin)
	}
	if checklist := CompactionGateChecklist(result.Gates); checklist != "" {
		builder.WriteString("；门禁 " + checklist)
	}
	if result.FrameRef != "" {
		builder.WriteString("；帧正文 ref " + result.FrameRef + "（状态页「上下文压缩」条目可展开查看）")
	}
	builder.WriteString("。原始轮次仍在会话存储里，可用 read_tool_result / read_compressed_turn / search_history 回读细节。")
	return builder.String()
}

// CompactionGateChecklist renders one line of gate timings.
func CompactionGateChecklist(gates []GateTiming) string {
	if len(gates) == 0 {
		return ""
	}
	parts := make([]string, 0, len(gates)+1)
	parts = append(parts, fmt.Sprintf("reached=%d/%d", len(gates), context_runtime.CompactionGateTotal()))
	for _, gate := range gates {
		parts = append(parts, gate.Gate+CompactionGateDuration(gate.ElapsedMS))
	}
	return strings.Join(parts, " ")
}

// CompactionGateDuration renders zero as "<1ms".
func CompactionGateDuration(milliseconds int) string {
	if milliseconds <= 0 {
		return "<1ms"
	}
	return strconv.Itoa(milliseconds) + "ms"
}

// CompactionRangeLabel renders recorded message/event boundaries.
func CompactionRangeLabel(result Result) string {
	return model.CompactionRangeLabel(result.MessageFrom, result.MessageTo, result.EventFrom, result.EventTo)
}

// CompactContextNow compacts the current execution session.
func (application *Application) CompactContextNow(ctx context.Context) (Result, error) {
	if application == nil {
		return Result{}, errors.New("compact context: service is unavailable")
	}
	sessionID := application.deps.CurrentSessionID(ctx)
	if sessionID == "" {
		return Result{}, errors.New("compact context: 当前没有可压缩的会话")
	}
	if err := application.deps.Acquire(ctx, sessionID); err != nil {
		return Result{}, err
	}
	defer application.deps.Release(sessionID)
	outcome, err := application.deps.Compact(ctx, sessionID)
	if err != nil {
		return Result{}, err
	}
	switch outcome.Outcome {
	case context_runtime.CompactDone:
		return newResult(outcome), nil
	case context_runtime.CompactUnrecorded:
		if outcome.NoEpoch {
			return Result{
				Compacted:       true,
				Version:         outcome.Version,
				EstimatedTokens: outcome.AssembledTokens,
				ComparedTokens:  outcome.ComparedTokens,
				SoftThreshold:   outcome.SoftThreshold,
				HardThreshold:   outcome.HardThreshold,
				Note: "会话没有在飞回合（冷加载或刚清空），已按会话级显式压缩立刻压缩已装载的上下文" +
					"（引擎历史已换成压缩形态并按会话落盘）；但这条压缩没有进记录面（该会话的上下文状态被新回合替换），" +
					"故本次不留记录。原始轮次仍在会话存储里，可用 read_tool_result / read_compressed_turn / search_history 回读。",
			}, nil
		}
		return Result{
			Compacted:       true,
			Version:         outcome.Version,
			EstimatedTokens: outcome.AssembledTokens,
			ComparedTokens:  outcome.ComparedTokens,
			SoftThreshold:   outcome.SoftThreshold,
			HardThreshold:   outcome.HardThreshold,
			Note: "可变 transcript 已压缩为有界 checkpoint 帧并按会话落盘（引擎历史已换成压缩形态）；" +
				"但这次压缩来自自动路径（软/硬阈值）且该回合的任务执行已收尾，自动压缩记录只在执行中写，" +
				"故本次不留记录。原始轮次仍在会话存储里，可用 read_tool_result / read_compressed_turn / search_history 回读。",
		}, nil
	case context_runtime.CompactFailed:
		return Result{
			ComparedTokens:  outcome.ComparedTokens,
			EstimatedTokens: outcome.AssembledTokens,
			SoftThreshold:   outcome.SoftThreshold,
			HardThreshold:   outcome.HardThreshold,
			Failure:         outcome.Failure,
			Gates:           gates(outcome.Gates),
			Note: fmt.Sprintf("压缩失败：判据已命中（判据量 %d tokens ≥ 硬阈值 %d），但这次压不下去"+
				"（原因 %s）。按口径**不折上下文、不推压缩栈顶、不推进上下文版本**——"+
				"上下文原样继续 append，模型看到的仍是原来的会话历史（不需要 search_history 也知道之前发生了什么）；"+
				"本次只留一条失败记录。已经落了一条失败记录，可在状态页「上下文压缩」里查到。",
				outcome.ComparedTokens, outcome.HardThreshold, FailureReasonLabel(outcome.Failure)),
		}, nil
	case context_runtime.CompactScheduled:
		return Result{
			Scheduled: true,
			Note: "会话没有在飞回合（刚冷加载或刚清空），也没有已装载的对话材料（空会话）：现在没有可压缩的请求上下文；" +
				"已登记：下一条消息组装上下文前立即压缩，压缩结果对那条消息生效。",
		}, nil
	default:
		return Result{
			ComparedTokens:  outcome.ComparedTokens,
			EstimatedTokens: outcome.AssembledTokens,
			SoftThreshold:   outcome.SoftThreshold,
			HardThreshold:   outcome.HardThreshold,
			Note: fmt.Sprintf("压缩判据未命中：判据量（全量累积/缓存峰值请求估算）%d tokens < 软阈值 %d，未压缩、未改写状态"+
				"（附带：装配后请求估算 %d tokens，硬阈值 %d）。",
				outcome.ComparedTokens, outcome.SoftThreshold, outcome.AssembledTokens, outcome.HardThreshold),
		}, nil
	}
}

// CompactContextHandler handles the compact_context model tool.
func (application *Application) CompactContextHandler(ctx context.Context, argsJSON string) (string, error) {
	var input struct {
		Reason string `json:"reason,omitempty"`
	}
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
			return "", fmt.Errorf("compact_context: invalid JSON: %w", err)
		}
	}
	result, err := application.CompactContextNow(ctx)
	if err != nil {
		return "", fmt.Errorf("compact_context: %w", err)
	}
	if reason := strings.TrimSpace(input.Reason); reason != "" && result.Compacted {
		result.Note = "压缩原因（模型自述）：" + reason + "；" + result.Note
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("compact_context: encode result: %w", err)
	}
	return string(encoded), nil
}

func gates(values []context_runtime.CompactionGateTiming) []GateTiming {
	if len(values) == 0 {
		return nil
	}
	out := make([]GateTiming, 0, len(values))
	for _, gate := range values {
		out = append(out, GateTiming{Gate: gate.Gate, ElapsedMS: gate.ElapsedMS})
	}
	return out
}

func newResult(outcome context_runtime.CompactResult) Result {
	record := outcome.Record
	result := Result{
		Compacted:       true,
		Recorded:        true,
		NoEpoch:         outcome.NoEpoch,
		Version:         record.Version,
		Reason:          record.Reason,
		Origin:          record.Origin,
		MessagesBefore:  record.MessagesBefore,
		EstimatedTokens: outcome.AssembledTokens,
		ComparedTokens:  outcome.ComparedTokens,
		SoftThreshold:   outcome.SoftThreshold,
		HardThreshold:   outcome.HardThreshold,
		Gates:           gates(outcome.Gates),
		CompactedAt:     record.CompactedAt.Format("2006-01-02T15:04:05Z07:00"),
		MessageFrom:     record.MessageFrom,
		MessageTo:       record.MessageTo,
		EventFrom:       record.EventFrom,
		EventTo:         record.EventTo,
		FrameRef:        record.FrameRef,
		FrameBytes:      record.FrameBytes,
		FrameTokens:     record.FrameTokens,
	}
	result.Note = CompactionRecordNote(result)
	return result
}
