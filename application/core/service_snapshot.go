package core

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/view_state"
	"github.com/RedHuang-0622/seelex/session"
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
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		return SessionStatusIdle
	}
	runtime := unit.Chat.ChatState()
	if runtime.Running {
		return SessionStatusRunning
	}
	if len(unit.Chat.PendingRequests()) > 0 || runtime.QueuedCount > 0 {
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

// appendSessionMessageLocked 追加一条可见消息到指定会话（阶段 1：后台会话
// 也维护自己的可见投影；活跃会话同步镜像 Snapshot）。
func (service *Service) appendSessionMessageLocked(sessionID, role, content string, tool *ToolCall) *Message {
	return service.components.view.AppendMessageLockedFor(sessionID, role, content, tool)
}

// setSessionChatLockedFor 写指定会话的聊天运行态投影（活跃会话镜像
// Snapshot.Chat）。
func (service *Service) setSessionChatLockedFor(sessionID string, chat ChatState) {
	service.components.view.SetSessionChatLockedFor(sessionID, chat)
}

// mirrorActiveViewLocked 把当前活跃会话 scope 镜像到 Snapshot。
func (service *Service) mirrorActiveViewLocked() {
	service.components.view.MirrorActiveViewLocked()
}

// sessionViewLocked 返回指定会话的可见投影（core 域工具/恢复路径用；
// 调用方持有 Core.Mu）。
func (service *Service) sessionViewLocked(sessionID string) *session.View {
	return service.components.view.SessionViewLocked(sessionID)
}

// recordReadFileForSessionLocked 记录指定会话的 read 文件引用（阶段 1：
// ReadFiles 收进会话 view，不再写全局 Snapshot.ReadFiles）。
func (service *Service) recordReadFileForSessionLocked(sessionID, arguments string) {
	var input struct {
		Path string `json:"path"`
	}
	if json.Unmarshal([]byte(arguments), &input) != nil {
		return
	}
	input.Path = strings.TrimSpace(input.Path)
	if input.Path == "" {
		return
	}
	now := time.Now()
	view := service.sessionViewLocked(sessionID)
	for index := range view.ReadFiles {
		if view.ReadFiles[index].Path == input.Path {
			view.ReadFiles[index].ReadAt = now
			service.mirrorActiveViewLocked()
			return
		}
	}
	view.ReadFiles = append(view.ReadFiles, ReadFileRef{Path: input.Path, ReadAt: now})
	service.mirrorActiveViewLocked()
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
