package core

// 「会话内全量用户输入索引」（SA-D 右侧索引）：
//
//	右侧导航刻度 = 用户输入（一条刻度 = 会话里的一条用户输入），且是**全量**：
//	包含尚未加载到前端窗口的早期轮次。索引只带**有界摘要**，不带正文，因此
//	可以整份下发；读取通道优先走会话存储的 conversation 模块区间读（只解析
//	conversation 派生子树，不反序列化 Plan/Execution/Projection），不依赖
//	view_state 里"已加载的那一窗内容"——窗口只用来标注 Loaded 与回读页数。
//
// 事实源与降级：
//  1. SessionConversationRangePort（会话存储 message 行派生的可见会话，全量）；
//  2. 旧布局/无行时会话退回 record 的可见会话（RecordConversation）；
//  3. 两者都没有（草稿/空会话/不存在的会话）→ 空索引，不报错（右侧轨道隐藏）。

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/RedHuang-0622/seelex/backend/engine/context"
	"github.com/RedHuang-0622/seelex/backend/engine/session"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/middle/residency"
	"github.com/RedHuang-0622/seelex/middle/switch/sessionusecase"
	"github.com/RedHuang-0622/seelex/middle/view/state"
)

func (service *Service) readSessionInputIndex(sessionID string) (model.SessionInputIndex, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return model.SessionInputIndex{}, errors.New("session ID is required")
	}
	location := service.components.sessions.LocateSession(sessionID)
	conversation, total, err := service.sessionInputIndexConversation(location, sessionID)
	if err != nil {
		return model.SessionInputIndex{}, err
	}
	window, loaded := service.sessionInputWindowLoaded(sessionID, total)
	items := residency.BuildInputIndex(conversation, loaded, residency.InputIndexSummaryLimit, inputIndexUserText)
	return model.SessionInputIndex{
		SessionID:  sessionID,
		Total:      total,
		InputCount: len(items),
		Window:     window,
		Items:      items,
	}, nil
}

// sessionInputIndexConversation 读取会话的完整可见会话（用户输入的事实源）。
// 返回消息列表与**总数**（分页偏移空间的长度）。
func (service *Service) sessionInputIndexConversation(location session_runtime.Location, sessionID string) ([]model.Message, int, error) {
	var rangeErr error
	if store, ok := service.Deps.Sessions.(session_runtime.SessionConversationRangePort); ok {
		// limit=0 = 全量（只解析 conversation 派生子树，不读 view_state）。
		messages, total, err := store.LoadConversationRangeWorkspace(location.WorkspaceID, sessionID, 0, 0)
		if err == nil {
			return messages, total, nil
		}
		rangeErr = err
	}
	// 降级：旧布局（无 message 行）会话用 record 的可见会话（同一权威事实源）。
	record, ok, err := service.components.sessions.LoadSessionRecord(location, sessionID)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("load session record %q: %w", sessionID, err)
	}
	if !ok || record.ID != sessionID {
		if rangeErr != nil && !errors.Is(rangeErr, fs.ErrNotExist) {
			// 两条通道都读不到且并非"会话不存在"：显式失败，不静默成空索引。
			return nil, 0, fmt.Errorf("load session conversation %q: %w", sessionID, rangeErr)
		}
		return nil, 0, nil
	}
	conversation := service.components.sessions.RecordConversation(record)
	return conversation, len(conversation), nil
}

// sessionInputWindowLoaded 返回目标会话的已加载窗口元数据与窗口内已加载的
// 用户输入正文（顺序一致，用于在全量索引里对齐出 Loaded 段）。
//
// 只读会话域单元（不存在 = 该会话没有任何已加载内容：全部 Loaded=false）。
func (service *Service) sessionInputWindowLoaded(sessionID string, total int) (model.SessionInputWindow, []string) {
	window := model.SessionInputWindow{WindowSize: Limits().HistoryWindow, Total: total}
	loaded := []string{}
	service.ViewMu.RLock()
	if unit := service.sessions.Unit(sessionID); unit != nil {
		unit.View.Read(func(view *session.View) {
			window.Offset = view.HistoryOffset
			window.Count = view_state.DurableConversationCount(view.Conversation)
			window.HasMore = view.HasMoreHistory
			if view.TotalMessages > 0 {
				window.Total = view.TotalMessages
			}
			if view.ConversationWindow > 0 {
				window.WindowSize = view.ConversationWindow
			}
			for _, message := range view.Conversation {
				if text := inputIndexUserText(message); text != "" {
					loaded = append(loaded, text)
				}
			}
		})
	}
	service.ViewMu.RUnlock()
	if window.WindowSize <= 0 {
		window.WindowSize = Limits().HistoryWindow
	}
	if window.Total <= 0 {
		window.Total = total
	}
	if window.Count <= 0 {
		window.Count = len(loaded)
	}
	return window, loaded
}

// inputIndexUserText 返回一条可见消息作为「用户输入」的展示正文；非用户输入
// 返回空串（调用方据此跳过）。
func inputIndexUserText(message model.Message) string {
	if message.Role != "user" {
		return ""
	}
	if strings.HasPrefix(message.Content, session_runtime.SessionArchiveResumePrefix) {
		return "" // 会话恢复摘要（内部行，不是用户输入）
	}
	if context_runtime.IsProviderOnlyHistoryContent(message.Content) {
		return ""
	}
	return strings.TrimSpace(displayUserInput(message.Content))
}
