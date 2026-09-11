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
	// SnapshotView 已释放 Core.ViewMu，这里重新取读锁补状态。
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	if snapshot.Session.Draft {
		snapshot.Session.Status = SessionStatusDraft
	} else {
		snapshot.Session.Status = service.sessionStatusLocked(snapshot.Session.ID)
	}
	if service.draft != nil {
		// 保留的草稿槽位在会话树中始终可见（切换后不再"消失"）。
		alreadyListed := false
		for _, item := range snapshot.Sessions {
			if service.draft.ID != "" && item.ID == service.draft.ID {
				alreadyListed = true
				break
			}
		}
		if !alreadyListed {
			slot := *service.draft
			snapshot.Sessions = append([]SessionInfo{{
				ID:        slot.ID,
				Name:      draftSessionName,
				UpdatedAt: slot.UpdatedAt,
				Status:    SessionStatusDraft,
			}}, snapshot.Sessions...)
		}
	}
	snapshot.Sessions = service.enrichDirectoryRowsLocked(snapshot.Sessions)
	return snapshot
}

// ListSessions 返回当前权威会话目录（C1 冷读面/headless 宿主）：与会话树
// 同一数据源（catalog 联合镜像），按行补会话级可见状态（运行/排队/待批/
// resident/审批计数），不含 UI 专用草稿占位槽。调用方可直接枚举而无需拉
// 整份视图快照。
func (service *Service) ListSessions() []SessionInfo {
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	rows := append([]SessionInfo(nil), service.Core.Snapshot.Sessions...)
	return service.enrichDirectoryRowsLocked(rows)
}

// enrichDirectoryRowsLocked 给目录行补会话级可见状态（调用方持有
// Core.ViewMu）：草稿槽位行恒为 draft；其余行按单元运行态叠加状态/
// 待批计数/resident 标记。
func (service *Service) enrichDirectoryRowsLocked(rows []SessionInfo) []SessionInfo {
	// 草稿行幂等防重：同一时刻只允许一个“新建会话草稿”出现在列表——当前
	// 视图草稿（service.draft）持有责任；历史遗留的其它 draft record（如
	// 崩溃前保存的旧草稿）隐藏但保留在磁盘，不造成列表多值。
	if service.draft != nil && service.draft.ID != "" {
		filtered := rows[:0]
		for _, row := range rows {
			if row.Status == SessionStatusDraft && row.ID != service.draft.ID {
				continue
			}
			filtered = append(filtered, row)
		}
		rows = filtered
	}
	for index := range rows {
		if service.draft != nil && service.draft.ID != "" && rows[index].ID == service.draft.ID {
			// 草稿槽位行：未发送输入期间恒为 draft（单元 ChatState 空闲，
			// 不能被运行态叠加成 idle）。
			rows[index].Status = SessionStatusDraft
			continue
		}
		sessionID := rows[index].ID
		rows[index].Status = service.sessionStatusLocked(sessionID)
		if unit := service.sessions.Unit(sessionID); unit != nil {
			rows[index].ApprovalCount = unit.PendingApprovalCount()
			rows[index].Resident = unit.Resident()
		}
	}
	return rows
}

// sessionStatusLocked 返回指定会话的可见状态（调用方持有 Core.ViewMu）。
func (service *Service) sessionStatusLocked(sessionID string) SessionStatus {
	if sessionID == "" {
		return SessionStatusDraft
	}
	if service.isRestoringLocked(sessionID) {
		return SessionStatusRestoring
	}
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		return SessionStatusIdle
	}
	if unit.PendingApprovalCount() > 0 {
		// awaiting_approval 覆盖运行态：引擎阻塞在审批上（INV-G8 驱逐守卫
		// 的目录面；ChatState.Running 仍为 true，收尾/取消路径不变）。
		return SessionStatusAwaitingApproval
	}
	runtime := unit.ChatState()
	if runtime.Running {
		return SessionStatusRunning
	}
	if len(unit.PendingRequests()) > 0 || runtime.QueuedCount > 0 {
		return SessionStatusQueued
	}
	return SessionStatusIdle
}

// isRestoringLocked 报告目标会话是否处于后台冷加载（调用方持有
// Core.ViewMu）。
func (service *Service) isRestoringLocked(sessionID string) bool {
	if service == nil || service.restoring == nil {
		return false
	}
	_, ok := service.restoring[sessionID]
	return ok
}

// setRestoringLocked 标记目标会话进入后台冷加载（调用方持有 Core.ViewMu）。
func (service *Service) setRestoringLocked(sessionID string) {
	if service.restoring == nil {
		service.restoring = make(map[string]struct{})
	}
	service.restoring[sessionID] = struct{}{}
}

// clearRestoringLocked 移除目标会话的后台冷加载标记（调用方持有
// Core.ViewMu）。
func (service *Service) clearRestoringLocked(sessionID string) {
	delete(service.restoring, sessionID)
}

// nextViewEpoch 推进视图切换序号并返回新值（调用方持有 Core.ViewMu）。
func (service *Service) nextViewEpochLocked() uint64 {
	service.viewEpoch++
	return service.viewEpoch
}

func (service *Service) Subscribe(buffer int) Subscription {
	return service.components.view.Subscribe(buffer)
}

func (service *Service) collectRuntimeProjection(ctx context.Context) view_state.RuntimeStateProjection {
	return service.components.view.CollectRuntimeProjection(ctx)
}

// collectRuntimeProjectionFor 按显式会话收集运行时投影（G1：后台会话的
// 槽写入方；tasks/tokens/skills 走 per-session 端口）。
func (service *Service) collectRuntimeProjectionFor(ctx context.Context, sessionID string) view_state.RuntimeStateProjection {
	return service.components.view.CollectRuntimeProjectionFor(ctx, sessionID)
}

func (service *Service) applyRuntimeProjectionLocked(projection view_state.RuntimeStateProjection) {
	service.components.view.ApplyRuntimeProjectionLocked(projection)
}

// applyRuntimeProjectionForLocked 应用运行时投影到指定会话槽（活跃会话由
// 视图协调器镜像 Snapshot；调用方持有 Core.ViewMu）。
func (service *Service) applyRuntimeProjectionForLocked(sessionID string, projection view_state.RuntimeStateProjection) {
	service.components.view.ApplyRuntimeProjectionForLocked(sessionID, projection)
}

func (service *Service) appendMessageLocked(role, content string, tool *ToolCall) *Message {
	return service.components.view.AppendMessageLocked(role, content, tool)
}

// appendSessionMessageLocked 追加一条可见消息到指定会话（阶段 1：后台会话
// 也维护自己的可见投影；活跃会话同步镜像 Snapshot）。
func (service *Service) appendSessionMessageLocked(sessionID, role, content string, tool *ToolCall) *Message {
	return service.components.view.AppendMessageLockedFor(sessionID, role, content, tool)
}

// appendAssistantPlaceholderAfterToolLocked 在指定会话的 tool_result 之后补一条
// 空 assistant 占位（活跃与后台同一语义；调用方持有 Core.ViewMu）。末条已是
// 空 assistant（无内容、无工具）时不重复补，避免并行工具轮产生多余空行。
func (service *Service) appendAssistantPlaceholderAfterToolLocked(sessionID string) *Message {
	emptyAssistantTail := false
	service.components.view.SessionViewReadLocked(sessionID, func(view *session.View) {
		if n := len(view.Conversation); n > 0 {
			last := view.Conversation[n-1]
			emptyAssistantTail = last.Role == "assistant" && last.Content == "" && last.Tool == nil
		}
	})
	if emptyAssistantTail {
		return nil
	}
	return service.appendSessionMessageLocked(sessionID, "assistant", "", nil)
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
// 调用方持有 Core.ViewMu）。
func (service *Service) sessionViewLocked(sessionID string) *session.View {
	return service.components.view.SessionViewLocked(sessionID)
}

// sessionViewEmptyLocked 报告指定会话的可见会话是否为空（调用方持有
// Core.ViewMu）。冷恢复用它判断"目标会话在我装载期间是否已经积累了更新
// 的活消息"——有则不覆盖。会话域单元不存在时视为空。
func (service *Service) sessionViewEmptyLocked(sessionID string) bool {
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		return true
	}
	empty := true
	unit.View.Read(func(view *session.View) {
		empty = len(view.Conversation) == 0
	})
	return empty
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
	// G5 访问器化：ReadFiles 写经 View.mu（Mutate），避免 ViewMu 下裸字段
	// 穿越；镜像由调用方在 ViewMu 上下文统一执行。
	service.components.view.SessionViewMutateLocked(sessionID, func(view *session.View) {
		for index := range view.ReadFiles {
			if view.ReadFiles[index].Path == input.Path {
				view.ReadFiles[index].ReadAt = now
				return
			}
		}
		view.ReadFiles = append(view.ReadFiles, ReadFileRef{Path: input.Path, ReadAt: now})
	})
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

// advanceMessageSeqLocked 按既有消息 ID 推进**该会话**的消息派号（恢复路径委托）。
func (service *Service) advanceMessageSeqLocked(sessionID string, messages []Message) {
	service.components.view.AdvanceMessageSeqForLocked(sessionID, messages)
}
