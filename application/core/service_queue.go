package core

import (
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/session"
)

// 排队输入的编辑面（调换顺序 / 撤回到输入框）。
//
// 队列只承载「回合运行中」接受的输入（回合收尾时整批提升为下一轮，见
// chat.go 的 queuedChatRequests/combineChatRequests）。因此这两个操作只在
// 目标会话的运行态下有意义：非运行态一律以 ErrQueueNotRunning 明确拒绝，
// 绝不触碰正在运行的回合（不动 Chat.Running/RequestID/StartedAt，不动引擎
// 历史、任务槽或可见会话）。
//
// 串行化：与 Enqueue（submitConversation/submitConversationFor 的排队分支）
// 和 Dequeue（runChat 收尾的提升路径）共用同一把 Core.ViewMu——申请、调换、
// 撤回、提升四类转换点因此互斥，不会出现"撤回刚取走的条目又被提升进下一轮"
// 或"调换与批量提升交错"的孤儿输入。
//
// 下标空间（节点 156 硬化后的唯一约定）：对外（ChatState.InputQueue /
// QueuedCount）与对内（会话队列）是同一套下标——投影由会话域
// SessionUnit.QueueProjection 生成，逐项、按队列顺序、不按载荷类型过滤；域的
// RecallRequest / ReorderRequests 直接作用在这套下标上。因此 GUI/TUI 渲染的
// 第 i 行，就是撤回/调换操作的第 i 项，不需要任何换算。
//
// 反面：若在此处对载荷过滤（旧实现用 queuedChatRequests + chatRequestDisplays，
// 只保留 chatRequest），投影会比真实队列短，用户看到的行与队列项错位——调换会
// "操作成功但列表不变"，撤回会把没显示过的条目交还输入框。执行面（下一轮批量
// 提升，chat.go 的 queuedChatRequests）与展示面因此解耦：展示要"看得见就能
// 操作"，执行要"载荷类型正确"。

// ReorderQueuedInput 把目标会话排队输入中 from 位置的条目移动到 to 位置
// （调换排队顺序；seq 身份不变，仅位置变化）。目标会话的空 sessionID 表示
// 当前视图会话。
func (service *Service) ReorderQueuedInput(sessionID string, from, to int) error {
	service.ViewMu.Lock()
	if err := service.queueEditGuardLocked(); err != nil {
		service.ViewMu.Unlock()
		return err
	}
	unit, err := service.queuedInputUnitLocked(sessionID)
	if err != nil {
		service.ViewMu.Unlock()
		return err
	}
	if !unit.ReorderRequests(from, to) {
		service.ViewMu.Unlock()
		return fmt.Errorf("%w: from=%d to=%d", ErrQueueIndexOutOfRange, from, to)
	}
	service.applyQueueEditLocked(unit) // 释放 Core.ViewMu 并发布事件
	return nil
}

// RecallQueuedInput 把目标会话排队输入中 index 位置的条目撤回（出队）并返回
// 其展示原文：调用方（GUI/TUI）把原文交还输入框重新编辑，引擎侧 Payload
// （执行内核的 chatRequest，含每次提交固化的 skill 上下文与预算）随条目一并
// 丢弃——重新提交会产生一份新的固化载荷。目标会话的空 sessionID 表示当前
// 视图会话。
func (service *Service) RecallQueuedInput(sessionID string, index int) (string, error) {
	service.ViewMu.Lock()
	if err := service.queueEditGuardLocked(); err != nil {
		service.ViewMu.Unlock()
		return "", err
	}
	unit, err := service.queuedInputUnitLocked(sessionID)
	if err != nil {
		service.ViewMu.Unlock()
		return "", err
	}
	request, ok := unit.RecallRequest(index)
	if !ok {
		service.ViewMu.Unlock()
		return "", fmt.Errorf("%w: index=%d", ErrQueueIndexOutOfRange, index)
	}
	service.applyQueueEditLocked(unit) // 释放 Core.ViewMu 并发布事件
	return request.DisplayInput, nil
}

// queueEditGuardLocked 是队列编辑的关闭/排空门禁（与 Submit 同口径）。
func (service *Service) queueEditGuardLocked() error {
	if service == nil {
		return errors.New("application is not available")
	}
	if service.closed {
		return errors.New("application is shut down")
	}
	if service.draining {
		return ErrApplicationDraining
	}
	return nil
}

// queuedInputUnitLocked 解析队列编辑的目标会话单元（调用方持有 Core.ViewMu）。
// 空 sessionID = 当前视图会话（草稿会话亦由视图 ID 归属）。错误语义：
//   - 会话不在会话域注册表且不是当前视图会话 → ErrQueueSessionNotFound
//   - 会话当前没有运行中的回合 → ErrQueueNotRunning（视图会话尚未注册时
//     同样按"没有回合在跑"处理，而不是"会话不存在"）
func (service *Service) queuedInputUnitLocked(sessionID string) (*session.SessionUnit, error) {
	sessionID = strings.TrimSpace(sessionID)
	viewID := service.Core.Snapshot.Session.ID
	if sessionID == "" {
		sessionID = viewID
	}
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		if sessionID == viewID {
			return nil, fmt.Errorf("%w: session %q", ErrQueueNotRunning, sessionID)
		}
		return nil, fmt.Errorf("%w: session %q", ErrQueueSessionNotFound, sessionID)
	}
	if !unit.ChatState().Running {
		return nil, fmt.Errorf("%w: session %q", ErrQueueNotRunning, sessionID)
	}
	return unit, nil
}

// applyQueueEditLocked 在队列编辑成功后重投影该会话的 ChatState.InputQueue /
// QueuedCount 并发布事件。调用方持有 Core.ViewMu；本函数在发布前释放它
// （事件必须在 ViewMu 之外下发，与 submitConversation 的排队分支同口径）。
func (service *Service) applyQueueEditLocked(unit *session.SessionUnit) {
	sessionID := unit.ID
	displays, count := unit.QueueProjection()
	unit.UpdateChat(func(chat *ChatState) {
		chat.InputQueue = displays
		chat.QueuedCount = count
	}, nil)
	if service.isActiveSessionLocked(sessionID) {
		service.setSessionChatLockedFor(sessionID, unit.ChatState())
		revision := service.bumpLocked()
		service.ViewMu.Unlock()
		service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
	} else {
		service.ViewMu.Unlock()
		service.publishSessionEvent(EventSnapshotChanged, 0, "", sessionID, nil)
	}
	service.publishChatStateFor(sessionID)
}
