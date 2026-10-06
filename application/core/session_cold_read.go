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
	// 工作表格是项目/全局台账：冷读面 = 进程全局表 + 本会话持久化的自身
	// 条目（冷会话未驻留，其条目只在 record 里），合并后投影成同一张表。
	records := record.Tasks
	if service.Deps.Runtime != nil {
		records = mergeTaskRecords(service.Deps.Runtime.TaskSnapshot(), record.Tasks, sessionID)
	}
	asyncRuns := service.asyncRunsForTable()
	if len(records) > 0 || len(asyncRuns) > 0 {
		rows := buildWorkTable(snapshot.Runtime.Plan, records, nil, asyncRuns)
		snapshot.Runtime.WorkTable = rows
		snapshot.Runtime.WorkTableBatches = buildWorkTableBatches(rows)
	}
	if task := record.Execution.Task; task != nil {
		taskCopy := *task
		taskCopy.ContextCompactions = append([]ContextCompaction(nil), task.ContextCompactions...)
		snapshot.Task = &taskCopy
	}
	// v8/S20：record 通道退役后 record.Execution.Task 恒空，压缩记录住在本会话
	// 自己的通道里（见 sessionstore/compaction_records.go）。非驻留会话的右栏
	// 「上下文压缩」读的就是这里——不还原它，未驻留旧会话的压缩栈在重启后为空。
	if snapshot.Task == nil || len(snapshot.Task.ContextCompactions) == 0 {
		if records, ok, _ := service.components.sessions.LoadSessionCompactionRecords(location, sessionID); ok && len(records) > 0 {
			snapshot.Task = &TaskState{
				// 冷读面没有回合身份（无 RequestID），状态取 idle：这是"这个会话
				// 当前没在跑"的如实说法，不是伪造一个已收尾的回合。
				Status:             TurnIdle,
				ContextCompactions: append([]ContextCompaction(nil), records...),
			}
		}
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
