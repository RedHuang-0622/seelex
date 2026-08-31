package core

import (
	"context"
	"errors"
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/core/chat"
	"github.com/RedHuang-0622/seelex/application/event"
)

// sessionChatRuntime 是单会话聊天运行态：M1 起聊天保护粒度从全局单例
// 收窄为会话级——每个会话独立的 Running/RequestID/cancel/inputQueue/
// 流输出。活跃会话的状态镜像到 Core.Snapshot.Chat 供前端与共享组件读取；
// 非活跃会话的排队与运行态保留在各自 runtime 中（真并行执行 = M2，
// 需共享组件栈的会话级隔离）。
type sessionChatRuntime struct {
	chat          ChatState
	cancel        context.CancelFunc
	inputQueue    []chatRequest
	streamOutput  *chat.VisibleOutputStream
	streamBatcher *chat.StreamBatcher
}

// sessionChatLocked 返回指定会话的聊天运行态（按需创建）。调用方必须
// 持有 Core.Mu。
func (service *Service) sessionChatLocked(sessionID string) *sessionChatRuntime {
	if service.sessionChat == nil {
		service.sessionChat = make(map[string]*sessionChatRuntime)
	}
	runtime := service.sessionChat[sessionID]
	if runtime == nil {
		runtime = &sessionChatRuntime{}
		service.sessionChat[sessionID] = runtime
	}
	return runtime
}

// anyChatRunningLocked 报告是否存在任意会话的运行中聊天。M1 单飞执行
// 闸门依赖它：切换会话/新建会话必须等所有会话空闲；同会话二次提交仍走
// 会话内队列。
func (service *Service) anyChatRunningLocked() bool {
	for _, runtime := range service.sessionChat {
		if runtime != nil && runtime.chat.Running {
			return true
		}
	}
	return false
}

// mirrorActiveChatLocked 把当前活跃会话的聊天运行态写入会话 view（阶段 1：
// Snapshot.Chat 由 view 统一镜像；切换会话后调用保证前端读到活跃会话状态）。
func (service *Service) mirrorActiveChatLocked() {
	sessionID := service.Core.Snapshot.Session.ID
	if runtime := service.sessionChat[sessionID]; runtime != nil {
		service.setSessionChatLockedFor(sessionID, runtime.chat)
	} else {
		service.setSessionChatLockedFor(sessionID, ChatState{})
	}
}

// publishSessionEvent 发布事件；装配的 EventHub 支持会话路由时携带
// sessionID，否则退化为普通 Publish。
func (service *Service) publishSessionEvent(kind event.EventKind, revision uint64, requestID, sessionID string, payload any) event.Event {
	if hub, ok := service.Events.(event.SessionAwareHub); ok {
		return hub.PublishSession(kind, revision, requestID, sessionID, payload)
	}
	return service.Events.Publish(kind, revision, requestID, payload)
}

// SubmitToSession 是会话级提交 API（M2：多会话并行执行）。目标会话即活跃
// 会话时等价 Submit（同会话运行中排队）；目标会话为其它会话且已加载（引擎
// 已实例化）时，在该会话上下文中后台并行启动（不切换活跃会话）；未加载的
// 会话先切换恢复再提交（兼容 M1 语义）。
func (service *Service) SubmitToSession(ctx context.Context, sessionID, text string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session ID is required")
	}
	service.Mu.RLock()
	current := service.Core.Snapshot.Session.ID
	service.Mu.RUnlock()
	if sessionID == current {
		return service.Submit(ctx, text)
	}
	if loaded := service.sessionLoaded(sessionID); loaded {
		return service.submitConversationFor(ctx, sessionID, text)
	}
	// 目标会话未加载：切换恢复后提交（旧 M1 语义；会话级门控允许运行中
	// 恢复空闲会话）。
	if err := service.ActivateSession(sessionID); err != nil {
		return err
	}
	return service.Submit(ctx, text)
}

// sessionLoaded 报告目标会话引擎是否已实例化（后台提交前置检查）。
func (service *Service) sessionLoaded(sessionID string) bool {
	if routed, ok := service.Deps.Engine.(contract.SessionChatEngine); ok {
		return routed.HasSession(sessionID)
	}
	return sessionID == service.Core.Snapshot.Session.ID
}

// ActivateSession 切换当前展示/执行会话。M1 没有每会话驻留快照，切换即
// 恢复（resumeSession 从持久化重建）；运行中切换被拒绝，保证共享
// Snapshot 不串写。多页签并行驻留 = M2/M3。
func (service *Service) ActivateSession(sessionID string) error {
	return service.resumeSession(sessionID)
}

// SnapshotOf 返回指定会话的权威快照。M1 只有活跃会话有驻留快照，其它
// 会话需先 ActivateSession；多页签驻留快照 = M2。
func (service *Service) SnapshotOf(sessionID string) (Snapshot, error) {
	sessionID = strings.TrimSpace(sessionID)
	service.Mu.RLock()
	current := service.Core.Snapshot.Session.ID
	service.Mu.RUnlock()
	if sessionID != "" && sessionID == current {
		return service.Snapshot(), nil
	}
	return Snapshot{}, ErrSessionSnapshotUnavailable
}

// SubscribeSession 返回按会话过滤的事件订阅（只投递该会话或全局事件）。
// 底层 Hub 不支持会话订阅时返回错误。
func (service *Service) SubscribeSession(sessionID string, buffer int) (Subscription, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return Subscription{}, errors.New("session ID is required")
	}
	hub, ok := service.Events.(interface {
		SubscribeSession(string, int) Subscription
	})
	if !ok {
		return Subscription{}, errors.New("event hub does not support session-scoped subscriptions")
	}
	return hub.SubscribeSession(sessionID, buffer), nil
}
