package core

import (
	"context"

	"github.com/RedHuang-0622/seelex/application/core/view_state"
)

// Service 门面快照/订阅/投影/消息写入委托（实现位于 view_state 域包）。

func (service *Service) Snapshot() Snapshot {
	snapshot := service.components.view.SnapshotView()
	// 会话状态属性富化：draft / running / queued / idle。
	// SnapshotView 已释放 Core.Mu，这里重新取读锁补状态。
	service.Mu.RLock()
	defer service.Mu.RUnlock()
	if snapshot.Session.Draft {
		snapshot.Session.Status = SessionStatusDraft
	} else {
		snapshot.Session.Status = service.sessionStatusLocked(snapshot.Session.ID)
	}
	if service.draft != nil {
		// 保留的草稿槽位在会话树中始终可见（切换后不再"消失"）。
		alreadyListed := false
		for _, item := range snapshot.Sessions {
			if item.ID == "" {
				alreadyListed = true
				break
			}
		}
		if !alreadyListed {
			slot := *service.draft
			snapshot.Sessions = append([]SessionInfo{{
				ID:        "",
				Name:      draftSessionName,
				UpdatedAt: slot.UpdatedAt,
				Status:    SessionStatusDraft,
			}}, snapshot.Sessions...)
		}
	}
	for index := range snapshot.Sessions {
		snapshot.Sessions[index].Status = service.sessionStatusLocked(snapshot.Sessions[index].ID)
	}
	return snapshot
}

// sessionStatusLocked 返回指定会话的可见状态（调用方持有 Core.Mu）。
func (service *Service) sessionStatusLocked(sessionID string) SessionStatus {
	if sessionID == "" {
		return SessionStatusDraft
	}
	runtime := service.sessionChat[sessionID]
	if runtime == nil {
		return SessionStatusIdle
	}
	if runtime.chat.Running {
		return SessionStatusRunning
	}
	if len(runtime.inputQueue) > 0 || runtime.chat.QueuedCount > 0 {
		return SessionStatusQueued
	}
	return SessionStatusIdle
}

func (service *Service) Subscribe(buffer int) Subscription {
	return service.components.view.Subscribe(buffer)
}

func (service *Service) collectRuntimeProjection(ctx context.Context) view_state.RuntimeStateProjection {
	return service.components.view.CollectRuntimeProjection(ctx)
}

func (service *Service) applyRuntimeProjectionLocked(projection view_state.RuntimeStateProjection) {
	service.components.view.ApplyRuntimeProjectionLocked(projection)
}

func (service *Service) appendMessageLocked(role, content string, tool *ToolCall) *Message {
	return service.components.view.AppendMessageLocked(role, content, tool)
}

func (service *Service) bumpLocked() uint64 {
	return service.components.view.BumpLocked()
}

func (service *Service) addNotice(notice string) {
	service.components.view.AddNotice(notice)
}

// AddNotice 追加一条系统通知（以 system 消息进入可见会话并发布
// message.added 事件）。启动期配置警告等非致命错误用它呈现给前端。
func (service *Service) AddNotice(notice string) {
	service.addNotice(notice)
}

func (service *Service) resetConversation(notice string) {
	service.components.view.ResetConversation(notice)
}

// advanceMessageSeqLocked 按既有消息 ID 推进消息序列（恢复路径委托）。
func (service *Service) advanceMessageSeqLocked(messages []Message) {
	service.components.view.AdvanceMessageSeqLocked(messages)
}
