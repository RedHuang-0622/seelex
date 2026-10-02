package task_context

import (
	"strings"

	"github.com/RedHuang-0622/seelex/application/model"
)

// 会话级上下文维护：给**没有在飞回合**的会话（冷加载、刚清空）提供一次可
// 压缩的执行身份，使显式压缩（`/compact`、`compact_context`）能立刻压缩已
// 装载的上下文，而不是登记到"下一条消息"才兑现。
//
// 为什么不直接伪造一个回合纪元（2026-09-23 的结论仍然成立）：真实纪元一旦
// 写进状态，"有人在跑这个会话"就会漏到可见面（`ChatState.Running`、任务
// 注册表、快照 `RequestID`），而冷加载的会话并没有回合在跑。这里的折中是
// **带前缀的维护身份**：
//
//   - `RequestID` 恒带 `SessionMaintenanceRequestPrefix`，与任何真实回合 ID
//     （`chat-<nanos>-<seq>`）不可能混淆，`sessionForRequestLocked` 也只按
//     绑定表反查；
//   - 状态显式标记为 `StatusIdle`——前端状态面本来就把"没有任务"渲染成
//     `idle`（`gui/frontend/dist/app.js` 的默认值即 `"idle"`），因此不新增
//     一个用户看不懂的状态值；
//   - 不写 `ChatState.Running`、不设 `Snapshot.Chat.RequestID`、不建任务
//     注册表条目：可见面上没有任何"正在执行"的信号；
//   - 会话已有上下文状态（冷恢复的 projection：`ContextVersion`/checkpoint/
//     压缩记录/技能层/objective）时**复用**它，不新建、不覆盖、不重置；
//   - 维护身份在压缩结束后撤销（`RequestID` 归空），会话留下的是"上下文
//     状态"而不是"回合状态"；下一次 `BeginTask` 照常开新回合，并把这份
//     上下文状态当作上一份状态处理。
//
// 生命周期（调用方 context_runtime.Coordinator）：
//
//	BeginSessionContextMaintenanceLocked → 压缩 → EndSessionContextMaintenanceLocked
const SessionMaintenanceRequestPrefix = "session-maintenance:"

// StatusIdle 表示"会话持有上下文状态，但没有在飞回合"。它不是回合终态
// （`IsContinuableStatus` 为假，不会让下一回合把它当成可续接任务），也不
// 参与进度语义；只用于冷加载会话的上下文维护（见
// `SessionMaintenanceRequestPrefix`）。
const StatusIdle = "idle"

// sessionMaintenanceObjectiveLimit 限定维护状态 objective 的字符数：objective
// 会进 checkpoint/压缩帧正文，不得把整条长输入搬进去。
const sessionMaintenanceObjectiveLimit = 400

// SessionMaintenanceRequestID 返回会话的维护身份（同会话可重复使用；压缩
// 结束后由 EndSessionContextMaintenanceLocked 撤销）。
func SessionMaintenanceRequestID(sessionID string) string {
	return SessionMaintenanceRequestPrefix + strings.TrimSpace(sessionID)
}

// BeginSessionContextMaintenanceLocked 为指定会话打开（或复用）会话级上下文
// 维护身份，返回该身份；返回空串表示**没有拿到身份**——会话已有在飞回合
// （或另一个维护身份），此时调用方必须按既有纪元路径处理，不得抢占。
//
// 调用方持有 Core.ViewMu。
func (c *Coordinator) BeginSessionContextMaintenanceLocked(sessionID string) string {
	if c == nil {
		return ""
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	st := c.sessionStateLocked(sessionID)
	state := st.taskExecution
	if state != nil && strings.TrimSpace(state.RequestID) != "" {
		// 有在飞回合：不抢占真实身份（抢占会让该回合后续所有
		// `state.RequestID == requestID` 判定失配）。
		return ""
	}
	if state == nil {
		// 冷加载会话没有任务状态：按会话自己的事实建一份**上下文状态**
		// （objective 取最后一条真实用户输入，上下文版本从 1 起）。
		state = NewTaskExecutionState("", sessionMaintenanceObjective(st.transcript), c.prompt.CurrentEffort())
		state.Status = StatusIdle
		st.taskExecution = state
		c.syncGoalSkillActiveLocked()
	} else if strings.TrimSpace(state.Status) == "" {
		state.Status = StatusIdle
	}
	requestID := SessionMaintenanceRequestID(sessionID)
	state.RequestID = requestID
	c.bindRequestLocked(requestID, sessionID)
	return requestID
}

// EndSessionContextMaintenanceLocked 撤销会话级上下文维护身份：把维护期间
// 挂在状态上的身份归空并解绑，**保留**压缩产生的 `ContextVersion` /
// `ContextCompactions` / checkpoint（这些是会话的上下文事实，不是回合事实）。
//
// 可见任务面若还挂着维护身份，按会话上下文状态重建一次——快照里不该出现
// 一个指向不存在回合的 `RequestID`。
//
// 调用方持有 Core.ViewMu。
func (c *Coordinator) EndSessionContextMaintenanceLocked(sessionID, requestID string) {
	if c == nil {
		return
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	st := c.sessionStateLocked(sessionID)
	state := st.taskExecution
	if state == nil || state.RequestID != requestID {
		return
	}
	state.RequestID = ""
	if strings.TrimSpace(state.Status) == "" {
		state.Status = StatusIdle
	}
	c.unbindRequestLocked(requestID)
	if task := c.Snapshot.Task; task != nil && task.RequestID == requestID {
		c.Snapshot.Task = c._TaskStateFor(sessionID)
	}
}

// sessionMaintenanceObjective 取会话 transcript 里最后一条真实用户输入作为
// 维护状态的目标：冷加载会话没有在飞回合，objective 只能来自会话自己的事实
// （技能正文等内部 user 材料不是用户输入，跳过）。有界截断，见
// `sessionMaintenanceObjectiveLimit`。
func sessionMaintenanceObjective(transcript []model.TranscriptEvent) string {
	for index := len(transcript) - 1; index >= 0; index-- {
		event := transcript[index]
		if !isUserQuestionEvent(event) {
			continue
		}
		content := strings.TrimSpace(event.Content)
		if content == "" {
			continue
		}
		return truncateRunes(content, sessionMaintenanceObjectiveLimit)
	}
	return ""
}

// truncateRunes 按字符数有界截断（不切断多字节字符），带显式省略标记。
func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}
