package context_runtime

import (
	"github.com/RedHuang-0622/seelex/application/contract"
)

// 折叠/修复路径的历史读写入口。
//
// 历史上这里有一整套「环内通道」（loopHistoryChannel / InLoopChannelFrom）：Seele
// 的 Chat/ChatStream 从进函数持锁到出函数，环内（工具 handler、LoopHooks、
// ContextController）再调历史方法就是同 goroutine 抢自己已持有的非重入锁；于是
// 宿主必须凭「引擎注入本轮 ctx 的把手」判定自己在不在环内，并走一条不取锁的旁路。
//
// Seele 现在把这件事从根上换掉了：回合准入用感知 ctx 的闸门（不持锁跑整轮），工作
// 历史由短临界区保护，`Session.History()` 永不阻塞、`Session.ReplaceHistory()` 忙时
// 排队到下一个检查点（空闲时立即应用）。因此**环内与环外走的是同一套方法、同一份
// 数据**，宿主不再需要判断"我此刻在不在环内"，也不再需要 ctx 透传到折叠里。本文件
// 因此只剩两件事：
//
//  1. 按会话路由读写历史 / 推进 system prompt（会话路由端口优先，回退活跃引擎）；
//  2. 把「正在飞的那一截」接回折叠产物尾部——引擎会拒收丢掉在飞 tool_call 单元的
//     替换（`session.ErrInFlightToolCallDropped`），而折叠产物是按"持久化事件 +
//     保留窗口"拼出来的，天然不含它。
//
// 为什么 2 现在是**所有路径**都要做（原先只在环内做）：忙会话的替换不再"登记到回合
// 收尾再装"，而是当场提交给引擎、在下一个检查点落地——提交那一刻引擎就会校验在飞
// 单元，所以锁外对在飞会话的折叠同样必须带上这段尾部，否则会被拒收。空闲会话没有
// 在飞单元，这段是空操作。

// foldHistory 读指定会话的引擎历史（会话路由端口优先）。
func (c *Coordinator) foldHistory(sessionID string) []contract.EngineMessage {
	return c.engineHistory(sessionID)
}

// replaceFoldHistory 写指定会话的引擎历史：替换经会话路由端口下发，落点由引擎
// 决定（空闲立即生效；在飞则排队到下一个检查点，同一回合的下一次请求即读到）。
func (c *Coordinator) replaceFoldHistory(sessionID string, history []contract.EngineMessage) error {
	return c.replaceEngineHistory(sessionID, history)
}

// setFoldSystemPrompt 把本会话 system prompt 推进引擎历史。它不改写进程级 prompt
// 默认（那是 SetSystemPromptFor 的事）——折叠只是把「本会话此刻的 prompt」送进引擎。
func (c *Coordinator) setFoldSystemPrompt(sessionID, prompt string) {
	c.setEngineSystemPrompt(sessionID, prompt)
}

// foldHistory / replaceFoldHistory 是 HistoryCoordinator 的同款入口（provider 历史
// 修复与折叠同源取历史，两者写的是同一份工作历史）。
func (h *HistoryCoordinator) foldHistory(sessionID string) []contract.EngineMessage {
	return h.engineHistory(sessionID)
}

func (h *HistoryCoordinator) replaceFoldHistory(sessionID string, history []contract.EngineMessage) error {
	return h.replaceEngineHistory(sessionID, history)
}

// withInFlightTail 把「正在飞的那一截」接回折叠产物尾部。
//
// 为什么需要：装配产物由「持久化事件 + 保留窗口」拼成，天然不含本轮还没落定的
// assistant(tool_calls)——它的 tool 结果此刻正由这次工具调用产出。若把它丢了，
// 引擎会拒收这次替换（紧随其后 append 的 tool 消息会成孤儿），而这一轮正是要靠
// 折叠把上下文收进预算里。多保留的是**本轮自己**的消息，不参与压缩判据、不改区间
// 与阈值口径。
func (c *Coordinator) withInFlightTail(existing, assembled []contract.EngineMessage) []contract.EngineMessage {
	tail := inFlightTail(existing)
	if len(tail) == 0 {
		return assembled
	}
	if len(assembled) > 0 && sameToolCalls(assembled[len(assembled)-1].ToolCalls, tail[0].ToolCalls) {
		return assembled // 已含（同一次折叠里读到的已是新快照）
	}
	return append(assembled, tail...)
}

// inFlightTail 返回历史末尾那段「assistant 带 tool_calls、其中至少一个 call 还没有
// 配对 tool 结果」的单元（含该单元之后已落的结果行）。
func inFlightTail(history []contract.EngineMessage) []contract.EngineMessage {
	resolved := make(map[string]struct{}, len(history))
	assistantAt := -1
	for index, message := range history {
		switch message.Role {
		case "assistant":
			if len(message.ToolCalls) > 0 {
				assistantAt = index
			}
		case "tool":
			if message.ToolCallID != "" {
				resolved[message.ToolCallID] = struct{}{}
			}
		}
	}
	if assistantAt < 0 {
		return nil
	}
	for _, call := range history[assistantAt].ToolCalls {
		if _, done := resolved[call.ID]; !done {
			return append([]contract.EngineMessage(nil), history[assistantAt:]...)
		}
	}
	return nil
}

func sameToolCalls(left, right []contract.EngineToolCall) bool {
	if len(left) != len(right) || len(left) == 0 {
		return false
	}
	for index := range left {
		if left[index].ID != right[index].ID {
			return false
		}
	}
	return true
}
