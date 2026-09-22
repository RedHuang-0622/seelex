package core

import (
	"strings"

	"github.com/RedHuang-0622/seelex/session"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// durable queue 的重启回填（写入侧接线的恢复半边）。
//
// 存储层在恢复时给出「已发送未确认」的条目（queue_consume.go：该轮 message
// 未发布的消费项）。本函数把这些输入正文回填到**本会话的内存队列**：
//   - 可见：ChatState.InputQueue/QueuedCount 立刻反映，用户能看见、能撤回、
//     能调序（与运行中排队的输入同一套下标空间，见 SessionUnit.QueueProjection）；
//   - 可送达：本会话不运行时不在此处自动开轮（打开会话即触发 LLM 调用是
//     用户不可预期的副作用，且可能失败），而是由下一次提交把队列里的待发项
//     与本次输入**合并提升**为同一轮（见 submitConversation/submitConversationFor
//     的非运行分支）——这样"重启后输入仍在"与"不会悄悄计费"同时成立。
//
// 幂等：同一批条目的内容为空则跳过；回填只发生在会话冷加载路径一次。
func (service *Service) reEnqueueRecoveredInputs(sessionID string, resent []sessionstore.QueueItem) {
	if service == nil || len(resent) == 0 {
		return
	}
	layers := service.promptStack.Layers()
	service.ViewMu.Lock()
	unit := service.sessionUnitLocked(sessionID)
	enqueued := 0
	for _, item := range resent {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		unit.Enqueue(session.QueuedRequest{DisplayInput: content, Payload: newChatRequest(content, layers)})
		enqueued++
	}
	if enqueued == 0 {
		service.ViewMu.Unlock()
		return
	}
	displays, count := unit.QueueProjection()
	unit.UpdateChat(func(chat *ChatState) {
		chat.InputQueue = displays
		chat.QueuedCount = count
	}, nil)
	revision := uint64(0)
	if service.isActiveSessionLocked(sessionID) {
		service.setSessionChatLockedFor(sessionID, unit.ChatState())
		revision = service.bumpLocked()
	}
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
	service.publishChatStateFor(sessionID)
}

// drainRecoveredQueueLocked 把本会话队列里已有的待发项与本次提交合并为同一轮
// （调用方持 Core.ViewMu；返回合并后的 request，由调用方在锁外启动回合）。
//
// 只在**非运行**分支使用：运行中分支的队列由回合收尾的批量提升消费（chat.go），
// 两条路径互斥（Running 是同一把 ViewMu 下的判据），不会重复消费。返回
// changed=false 表示队列为空、request 原样。
func (service *Service) drainRecoveredQueueLocked(unit *session.SessionUnit, request chatRequest) (chatRequest, bool) {
	pending := queuedChatRequests(unit.PendingRequests())
	if len(pending) == 0 {
		return request, false
	}
	unit.SetRequests(nil)
	displays, count := unit.QueueProjection()
	unit.UpdateChat(func(chat *ChatState) {
		chat.InputQueue = displays
		chat.QueuedCount = count
	}, nil)
	return combineChatRequests(append(pending, request)), true
}
