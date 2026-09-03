package core

// C1 冷读面：未驻留会话（无 unit，或 unit.Resident=false——驱逐/冷启动后）
// 从 record + Transcript/事件库拼**只读基线**，不建引擎 bundle、不改内存态。
// 冷基线 Resident=false，消费方只读展示；快照的 revision=0（无增量源），
// 热态一致性由 record 全量可见对话 + plan 栈 + task 注册表保证（与 resume
// 冷加载同一事实源，见 session_runtime/archive.go）。

import (
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/application/core/task_context"
)

// snapshotOfCold 组装未驻留会话的只读会话快照（record 事实源）。目录已归档
// 会话同样可读（record 保留），不存在的会话返回 ErrSessionSnapshotUnavailable。
func (service *Service) snapshotOfCold(sessionID string) (SessionSnapshot, error) {
	location := service.components.sessions.LocateSession(sessionID)
	record, ok, err := service.components.sessions.LoadSessionRecord(location, sessionID)
	if err != nil {
		return SessionSnapshot{}, fmt.Errorf("cold snapshot load %q: %w", sessionID, err)
	}
	if !ok || record.ID != sessionID {
		return SessionSnapshot{}, ErrSessionSnapshotUnavailable
	}

	conversation := service.components.sessions.RecordConversation(record)
	title := record.Title.Value
	if title == "" {
		title = service.components.sessions.SessionTitleFor(sessionID).Value
	}
	plan := task_context.ActivePlanFromStack(record.PlanStack, record.ActivePlanID)
	snapshot := SessionSnapshot{
		ProtocolVersion: ProtocolVersion,
		Session:         SessionState{ID: sessionID, Name: title},
		Conversation:    conversation,
		Chat:            ChatState{},
		Runtime: SessionRuntime{
			Plan:      clonePlanForSync(plan),
			WorkTable: nil,
		},
		Capabilities:       Capabilities{SessionResume: true, SessionSnapshot: true},
		HistoryOffset:      0,
		TotalMessages:      len(conversation),
		HasMoreHistory:     false,
		ConversationWindow: len(conversation),
		ReadFiles:          append([]ReadFileRef(nil), record.Execution.ReadFiles...),
		Resident:           false,
	}
	if len(record.Tasks) > 0 {
		rows := buildWorkTable(snapshot.Runtime.Plan, record.Tasks, nil)
		snapshot.Runtime.WorkTable = rows
		snapshot.Runtime.WorkTableBatches = buildWorkTableBatches(rows)
	}
	if task := record.Execution.Task; task != nil {
		taskCopy := *task
		taskCopy.ContextCompactions = append([]ContextCompaction(nil), task.ContextCompactions...)
		snapshot.Task = &taskCopy
	}
	return snapshot, nil
}

// GetSessionTranscript 读取指定会话的事件库区间（fromSeq..toSeq 含端点；
// (0,0) = 全量，与 sessionstore.EventStore.LoadRange 同一语义）。目标会话
// 未驻留时同样可用（事件库按项目键直接读回，不依赖内存态）。
func (service *Service) GetSessionTranscript(sessionID string, fromSeq, toSeq uint64) ([]TranscriptEvent, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("session ID is required")
	}
	if fromSeq > toSeq {
		return nil, fmt.Errorf("inverted transcript range %d..%d", fromSeq, toSeq)
	}
	return service.components.sessions.LoadTranscriptRange(sessionID, fromSeq, toSeq)
}
