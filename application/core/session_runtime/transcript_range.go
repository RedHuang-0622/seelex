package session_runtime

// C1 冷读面：transcript 区间读（事件库事实源）。目录/record 之外，事件通道
// 是会话权威日志（Seq 递增）；冷读与热读共用同一存储键，热态一致性由此保证。

import (
	"errors"
	"fmt"

	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// transcriptRangePort 是事件区间读的可选能力面（生产实现：internal/adapters
// SessionPort.LoadEventRangeWorkspace；窄接口避免把 fork 的整组能力塞进冷读
// 契约——测试桩只需实现这一个方法）。
type transcriptRangePort interface {
	LoadEventRangeWorkspace(projectID, sessionID string, fromSeq, toSeq uint64) ([]sessionstore.Event, error)
}

// LoadTranscriptRange 按 Seq 区间（含端点）读回会话事件日志并转换为
// application model.TranscriptEvent。fromSeq/toSeq == 0 表示全量（与
// sessionstore.EventStore.LoadRange 的 (0,0) 语义一致）。
func (c *Coordinator) LoadTranscriptRange(sessionID string, fromSeq, toSeq uint64) ([]model.TranscriptEvent, error) {
	port, ok := c.Core.Deps.Sessions.(transcriptRangePort)
	if !ok {
		return nil, errors.New("transcript range reads are not assembled")
	}
	location := c.LocateSession(sessionID)
	events, err := port.LoadEventRangeWorkspace(location.WorkspaceID, sessionID, fromSeq, toSeq)
	if err != nil {
		return nil, fmt.Errorf("load transcript range %d..%d for %q: %w", fromSeq, toSeq, sessionID, err)
	}
	return modelTranscriptEventsFromStore(events), nil
}

// modelTranscriptEventsFromStore 把存储层事件转换为应用层 transcript 事件
// （与 internal/adapters 落盘转换互逆；字段一一对应）。
func modelTranscriptEventsFromStore(events []sessionstore.Event) []model.TranscriptEvent {
	adapted := make([]model.TranscriptEvent, 0, len(events))
	for _, event := range events {
		calls := make([]model.TranscriptToolCall, 0, len(event.ToolCalls))
		for _, call := range event.ToolCalls {
			calls = append(calls, model.TranscriptToolCall{
				ID: call.ID, Name: call.Name, Arguments: call.Arguments,
			})
		}
		adapted = append(adapted, model.TranscriptEvent{
			Seq:              event.Seq,
			TaskID:           event.TaskID,
			MessageID:        event.MessageID,
			Kind:             event.Kind,
			Role:             event.Role,
			ReasoningContent: event.ReasoningContent,
			Content:          event.Content,
			ToolCallID:       event.ToolCallID,
			Name:             event.Name,
			ToolCalls:        calls,
			ResultRef:        event.ResultRef,
			TokenCount:       event.TokenCount,
			WireMaterial:     event.WireMaterial,
			CreatedAt:        event.CreatedAt,
			RoleName:         event.RoleName,
			RoleSessionID:    event.RoleSessionID,
			RoundID:          event.RoundID,
			UnitSeq:          event.UnitSeq,
		})
	}
	return adapted
}
