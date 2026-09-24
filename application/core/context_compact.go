package core

// 上下文主动压缩的对外入口（`/compact` 命令 + `compact_context` 工具）：
// 两者共用同一条落点——context_runtime.Coordinator.CompactContextNow →
// CompactTaskContextFor（与引擎钩子的自动压缩同一实现），因此不会出现
// "命令压一次、工具压一次、自动压一次" 三套语义。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/application/model"
)

// ContextCompactionResult 是压缩结果面（命令 notice 与工具 JSON 共用）：
// 只含公开元数据（版本/原因/压缩前消息数/估算 token），不带 checkpoint 正文、
// 不带会话内容——与会话快照里的 ContextCompaction 同一口径。
//
// 两个数字口径不同，绝不混用（混用就会说出"129409 tokens 未达阈值 118962"）：
//
//	ComparedTokens  = 压缩判据量（全量累积/引擎缓存峰值的请求估算）
//	EstimatedTokens = 装配后估算（真正发给 provider 的请求大小）
type ContextCompactionResult struct {
	Compacted bool `json:"compacted"`
	Recorded  bool `json:"recorded,omitempty"`
	Scheduled bool `json:"scheduled,omitempty"`
	// NoEpoch 标记这次压缩发生在**没有在飞回合**的会话上（冷加载、刚清空）：
	// 折叠按会话级维护身份执行（task_context.SessionMaintenanceRequestPrefix），
	// 当场生效、不依赖下一条消息。回执据此说明为什么"按了就有结果"。
	NoEpoch bool   `json:"no_epoch,omitempty"`
	Version uint64 `json:"version,omitempty"`
	Reason  string `json:"reason,omitempty"`
	// MessagesBefore 是**装配前的引擎历史条数**（engineHistory 长度），不是被压
	// 的 transcript 消息数：冷加载/路由会话里它合法为 0，而同一时刻的区间字段
	// （MessageFrom/To、EventFrom/To）仍然记得住"从哪压到哪"。用户可见的折叠
	// 范围一律走 compactionRangeLabel，不要拿这个数字当"压缩前 N 条消息"。
	MessagesBefore  int    `json:"messages_before,omitempty"`
	EstimatedTokens int    `json:"estimated_tokens,omitempty"`
	ComparedTokens  int    `json:"compared_tokens,omitempty"`
	SoftThreshold   int    `json:"soft_threshold,omitempty"`
	HardThreshold   int    `json:"hard_threshold,omitempty"`
	CompactedAt     string `json:"compacted_at,omitempty"`
	// 压缩区间（记录，不推算）：被压出保留窗口、送进 compact_context 的
	// transcript 前缀。MessageFrom/MessageTo = UI 消息号，EventFrom/EventTo =
	// transcript 事件序号。原始内容可用 read_compressed_turn / read_tool_result
	// 按该区间回读。
	MessageFrom string `json:"message_from,omitempty"`
	MessageTo   string `json:"message_to,omitempty"`
	EventFrom   uint64 `json:"event_from,omitempty"`
	EventTo     uint64 `json:"event_to,omitempty"`
	// Origin 是这次压缩的来源（auto/explicit/explicit_after_turn，见 model 常量）：
	// 「用户/模型显式要求」与「自动阈值触发」是两种事实，回执里不得混为一谈。
	Origin string `json:"origin,omitempty"`
	// FrameRef 是可回读的帧正文引用（会话内容存储里的 ref；空 = 本次没有正文可读）。
	// 前端用 ToolResultContent(ref, offset, limit) 分页读，不把正文塞进快照。
	FrameRef    string `json:"frame_ref,omitempty"`
	FrameBytes  int    `json:"frame_bytes,omitempty"`
	FrameTokens int    `json:"frame_tokens,omitempty"`
	Note        string `json:"note"`
}

// compactionReasonLabel 渲染压缩原因（用户可读）。未知原因原样返回，不编造。
func compactionReasonLabel(reason string) string {
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

// compactionRecordNote 渲染「压缩已落记录」的回执：版本 + 原因 + 被压区间 +
// 估算量，并指明帧正文与原始轮次怎么回读。
//
// 只印记录里**已有**的事实：区间空就不印区间段，判据量与装配量分开报
// （混用会说出「129409 tokens 未达阈值 118962」这种自相矛盾的话）。
//
// `/compact` 命令与 `compact_context` 工具共用这一句（命令侧不套自己的模板）：
// 因此"这次压缩为什么能当场生效"（无在飞回合的会话级压缩）也必须写在这里，
// 否则命令回执会把 NoEpoch 这件事悄悄丢掉。
func compactionRecordNote(result ContextCompactionResult) string {
	var builder strings.Builder
	if result.NoEpoch {
		builder.WriteString("会话没有在飞回合（冷加载或刚清空），已按会话级显式压缩立即执行" +
			"（折叠已装载的上下文并落记录，无需下一条消息）：")
	}
	builder.WriteString("已压缩上下文：v")
	builder.WriteString(strconv.FormatUint(result.Version, 10))
	builder.WriteString("（")
	builder.WriteString(compactionReasonLabel(result.Reason))
	builder.WriteString("）")
	if label := compactionRangeLabel(result); label != "" {
		builder.WriteString("，被压区间 ")
		builder.WriteString(label)
	}
	fmt.Fprintf(&builder, "，装配后估算 %d tokens（判据量 %d）", result.EstimatedTokens, result.ComparedTokens)
	if result.Origin != "" {
		builder.WriteString("，来源 " + result.Origin)
	}
	if result.FrameRef != "" {
		builder.WriteString("；帧正文 ref " + result.FrameRef + "（状态页「上下文压缩」条目可展开查看）")
	}
	builder.WriteString("。原始轮次仍在会话存储里，可用 read_tool_result / read_compressed_turn / search_history 回读细节。")
	return builder.String()
}

// compactionRangeLabel 把压缩结果面里**已有的**区间字段（MessageFrom/To、
// EventFrom/To）渲染成一段人可读的范围，例如
// `消息 message-1..message-103 / 事件 1..6`。
//
// 区间为空（老记录、或这次真的没有可记的边界）时返回空串：调用方据此跳过
// 这一段，不得改用别的量顶替（判据见 model.CompactionRangeLabel）。
func compactionRangeLabel(result ContextCompactionResult) string {
	return model.CompactionRangeLabel(result.MessageFrom, result.MessageTo, result.EventFrom, result.EventTo)
}

// CompactContextNow 压缩当前执行会话（命令/工具共用）：会话从 ctx 解析，
// ctx 没带时回退视图会话。
//
// 会话**有没有在飞回合都当场压缩**：有匹配 request 的执行纪元就按该纪元折叠；
// 只有已装载的上下文而没有在飞回合（冷加载、刚清空）时按会话级维护身份立刻
// 折叠已装载的上下文——用户按下回车就是要看到结果，不能要求他再发一条消息。
// 唯一例外是会话真的没有可折叠材料（空会话）：此时登记为"下一条消息组装时
// 立即压缩"（CompactScheduled），"没有可折叠的活"不是错误。
func (service *Service) CompactContextNow(ctx context.Context) (ContextCompactionResult, error) {
	if service == nil {
		return ContextCompactionResult{}, errors.New("compact context: service is unavailable")
	}
	sessionID := sessionIDFromContext(ctx)
	if sessionID == "" {
		service.ViewMu.RLock()
		sessionID = service.Core.Snapshot.Session.ID
		service.ViewMu.RUnlock()
	}
	if sessionID == "" {
		return ContextCompactionResult{}, errors.New("compact context: 当前没有可压缩的会话")
	}
	outcome, err := service.components.context.CompactContextNow(sessionID)
	if err != nil {
		return ContextCompactionResult{}, err
	}
	switch outcome.Outcome {
	case context_runtime.CompactDone:
		// 无在飞回合的会话级压缩由 compactionRecordNote 在同一句里说明
		// （命令与工具共用那一句，见其 doc 注释）。
		return newContextCompactionResult(outcome), nil
	case context_runtime.CompactFoldedUnrecorded:
		// 折叠确实发生了（引擎历史已换成有界 checkpoint 并按会话落盘），只是
		// 这一轮是**自动路径**（软/硬阈值）且执行已收尾——自动压缩记录只在
		// 任务执行中写，故不补记。如实说明，不谎报"未达阈值"。
		if outcome.NoEpoch {
			return ContextCompactionResult{
				Compacted:       true,
				Version:         outcome.Version,
				EstimatedTokens: outcome.AssembledTokens,
				ComparedTokens:  outcome.ComparedTokens,
				SoftThreshold:   outcome.SoftThreshold,
				HardThreshold:   outcome.HardThreshold,
				Note: "会话没有在飞回合（冷加载或刚清空），已按会话级显式压缩立刻折叠已装载的上下文" +
					"（引擎历史已换成压缩形态并按会话落盘）；但这条压缩没有进记录面（该会话的上下文状态被新回合替换），" +
					"故本次不留记录。原始轮次仍在会话存储里，可用 read_tool_result / read_compressed_turn / search_history 回读。",
			}, nil
		}
		return ContextCompactionResult{
			Compacted:       true,
			Version:         outcome.Version,
			EstimatedTokens: outcome.AssembledTokens,
			ComparedTokens:  outcome.ComparedTokens,
			SoftThreshold:   outcome.SoftThreshold,
			HardThreshold:   outcome.HardThreshold,
			Note: "可变 transcript 已折叠为有界 checkpoint 帧并按会话落盘（引擎历史已换成压缩形态）；" +
				"但这次折叠来自自动路径（软/硬阈值）且该回合的任务执行已收尾，自动压缩记录只在执行中写，" +
				"故本次不留记录。原始轮次仍在会话存储里，可用 read_tool_result / read_compressed_turn / search_history 回读。",
		}, nil
	case context_runtime.CompactScheduled:
		// 没有在飞回合、也没有已装载的对话材料（空会话）：不伪造纪元，也不折叠
		// 空上下文（那只会产出一条区间为空的记录，等于把"没做事"记成"做了事"），
		// 登记为下一次装配兑现。
		return ContextCompactionResult{
			Scheduled: true,
			Note: "会话没有在飞回合（刚冷加载或刚清空），也没有已装载的对话材料（空会话）：现在没有可折叠的请求上下文；" +
				"已登记：下一条消息组装上下文前立即压缩，压缩结果对那条消息生效。",
		}, nil
	default:
		return ContextCompactionResult{
			ComparedTokens:  outcome.ComparedTokens,
			EstimatedTokens: outcome.AssembledTokens,
			SoftThreshold:   outcome.SoftThreshold,
			HardThreshold:   outcome.HardThreshold,
			Note: fmt.Sprintf("压缩判据未命中：判据量（全量累积/缓存峰值请求估算）%d tokens < 软阈值 %d，未折叠、未改写状态"+
				"（附带：装配后请求估算 %d tokens，硬阈值 %d）。",
				outcome.ComparedTokens, outcome.SoftThreshold, outcome.AssembledTokens, outcome.HardThreshold),
		}, nil
	}
}

// CompactContextHandler 实现 compact_context 工具：模型在上下文逼近上限、
// 或即将开始一段长任务前主动收拢上下文。原始轮次仍完整留在会话存储里，
// 细节可用 read_tool_result / read_compressed_turn / search_history 回读。
func (service *Service) CompactContextHandler(ctx context.Context, argsJSON string) (string, error) {
	var input struct {
		Reason string `json:"reason,omitempty"`
	}
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
			return "", fmt.Errorf("compact_context: invalid JSON: %w", err)
		}
	}
	result, err := service.CompactContextNow(ctx)
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

func newContextCompactionResult(outcome context_runtime.CompactResult) ContextCompactionResult {
	record := outcome.Record
	result := ContextCompactionResult{
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
		CompactedAt:     record.CompactedAt.Format("2006-01-02T15:04:05Z07:00"),
		MessageFrom:     record.MessageFrom,
		MessageTo:       record.MessageTo,
		EventFrom:       record.EventFrom,
		EventTo:         record.EventTo,
		FrameRef:        record.FrameRef,
		FrameBytes:      record.FrameBytes,
		FrameTokens:     record.FrameTokens,
	}
	result.Note = compactionRecordNote(result)
	return result
}
