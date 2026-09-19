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
	"strings"

	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/application/model"
)

// ContextCompactionResult 是压缩结果面（命令 notice 与工具 JSON 共用）：
// 只含公开元数据（版本/原因/压缩前消息数/估算 token），不带 checkpoint 正文、
// 不带会话内容——与会话快照里的 ContextCompaction 同一口径。
type ContextCompactionResult struct {
	Compacted       bool   `json:"compacted"`
	Version         uint64 `json:"version,omitempty"`
	Reason          string `json:"reason,omitempty"`
	MessagesBefore  int    `json:"messages_before,omitempty"`
	EstimatedTokens int    `json:"estimated_tokens,omitempty"`
	CompactedAt     string `json:"compacted_at,omitempty"`
	// 压缩区间（记录，不推算）：被压出保留窗口、送进 compact_context 的
	// transcript 前缀。MessageFrom/MessageTo = UI 消息号，EventFrom/EventTo =
	// transcript 事件序号。原始内容可用 read_compressed_turn / read_tool_result
	// 按该区间回读。
	MessageFrom string `json:"message_from,omitempty"`
	MessageTo   string `json:"message_to,omitempty"`
	EventFrom   uint64 `json:"event_from,omitempty"`
	EventTo     uint64 `json:"event_to,omitempty"`
	Note        string `json:"note"`
}

// CompactContextNow 压缩当前执行会话（命令/工具共用）：会话从 ctx 解析，
// ctx 没带时回退视图会话；没有任务执行纪元时返回 Compacted=false 的结果
// （不是错误——"现在没有可压缩的活"不是故障）。
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
		return newContextCompactionResult(outcome.Record), nil
	case context_runtime.CompactNoTask:
		return ContextCompactionResult{
			Compacted: false,
			Note:      "当前会话没有进行中的任务执行，未做压缩；上下文接近上限时会自动压缩（硬阈值 90%）。",
		}, nil
	default:
		return ContextCompactionResult{
			Compacted:       false,
			EstimatedTokens: outcome.EstimatedTokens,
			Note: fmt.Sprintf("当前上下文估算 %d tokens，未达压缩阈值 %d，无需压缩（接近上限时框架会自动压缩）。",
				outcome.EstimatedTokens, outcome.SoftThreshold),
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

func newContextCompactionResult(record model.ContextCompaction) ContextCompactionResult {
	return ContextCompactionResult{
		Compacted:       true,
		Version:         record.Version,
		Reason:          record.Reason,
		MessagesBefore:  record.MessagesBefore,
		EstimatedTokens: record.EstimatedTokens,
		CompactedAt:     record.CompactedAt.Format("2006-01-02T15:04:05Z07:00"),
		MessageFrom:     record.MessageFrom,
		MessageTo:       record.MessageTo,
		EventFrom:       record.EventFrom,
		EventTo:         record.EventTo,
		Note: "可变 transcript 已折叠为有界 checkpoint 帧（稳定前缀 + 任务证据摘要 + plan + 当前输入）；" +
			"原始轮次仍在会话存储里，可用 read_tool_result / read_compressed_turn / search_history 回读细节。",
	}
}
