package context_runtime

import (
	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/model"
)

// 压缩/修复路径的历史读写入口。
//
// 历史上这里有一整套「环内通道」（loopHistoryChannel / InLoopChannelFrom）：Seele
// 的 Chat/ChatStream 从进函数持锁到出函数，环内（工具 handler、LoopHooks、
// ContextController）再调历史方法就是同 goroutine 抢自己已持有的非重入锁；于是
// 宿主必须凭「引擎注入本轮 ctx 的把手」判定自己在不在环内，并走一条不取锁的旁路。
//
// Seele 现在把这件事从根上换掉了：回合准入用感知 ctx 的闸门（不持锁跑整轮），工作
// 历史由短临界区保护，`Session.History()` 永不阻塞、`Session.ReplaceHistory()` 忙时
// 排队到下一个检查点（空闲时立即应用）。因此**环内与环外走的是同一套方法、同一份
// 数据**，宿主不再需要判断"我此刻在不在环内"，也不再需要 ctx 透传到压缩里。本文件
// 因此只剩两件事：
//
//  1. 按会话路由读写历史 / 推进 system prompt（会话路由端口优先，回退活跃引擎）；
//  2. 把「正在飞的那一截」接回压缩产物尾部——引擎会拒收丢掉在飞 tool_call 单元的
//     替换（`session.ErrInFlightToolCallDropped`），而压缩产物是按"持久化事件 +
//     保留窗口"拼出来的，天然不含它。
//
// 为什么 2 现在是**所有路径**都要做（原先只在环内做）：忙会话的替换不再"登记到回合
// 收尾再装"，而是当场提交给引擎、在下一个检查点落地——提交那一刻引擎就会校验在飞
// 单元，所以锁外对在飞会话的压缩同样必须带上这段尾部，否则会被拒收。空闲会话没有
// 在飞单元，这段是空操作。

// sessionHistory 读指定会话的引擎历史（会话路由端口优先）。
func (c *Coordinator) sessionHistory(sessionID string) []contract.EngineMessage {
	return c.engineHistory(sessionID)
}

// replaceSessionHistory 写指定会话的引擎历史：替换经会话路由端口下发，落点由引擎
// 决定（空闲立即生效；在飞则排队到下一个检查点，同一回合的下一次请求即读到）。
func (c *Coordinator) replaceSessionHistory(sessionID string, history []contract.EngineMessage) error {
	return c.replaceEngineHistory(sessionID, history)
}

// setSessionSystemPrompt 把本会话 system prompt 推进引擎历史。它不改写进程级 prompt
// 默认（那是 SetSystemPromptFor 的事）——压缩只是把「本会话此刻的 prompt」送进引擎。
func (c *Coordinator) setSessionSystemPrompt(sessionID, prompt string) {
	c.setEngineSystemPrompt(sessionID, prompt)
}

// sessionHistory / replaceSessionHistory 是 HistoryCoordinator 的同款入口（provider 历史
// 修复与压缩同源取历史，两者写的是同一份工作历史）。
func (h *HistoryCoordinator) sessionHistory(sessionID string) []contract.EngineMessage {
	return h.engineHistory(sessionID)
}

func (h *HistoryCoordinator) replaceSessionHistory(sessionID string, history []contract.EngineMessage) error {
	return h.replaceEngineHistory(sessionID, history)
}

// withInFlightTail 把「正在飞的那一截」接回压缩产物尾部。
//
// 为什么需要：装配产物由「持久化事件 + 保留窗口」拼成，天然不含本轮还没落定的
// assistant(tool_calls)——它的 tool 结果此刻正由这次工具调用产出。若把它丢了，
// 引擎会拒收这次替换（紧随其后 append 的 tool 消息会成孤儿），而这一轮正是要靠
// 压缩把上下文收进预算里。多保留的是**本轮自己**的消息，不参与压缩判据、不改区间
// 与阈值口径。
func (c *Coordinator) withInFlightTail(existing, assembled []contract.EngineMessage) []contract.EngineMessage {
	tail := inFlightTail(existing)
	if len(tail) == 0 {
		return assembled
	}
	if len(assembled) > 0 && sameToolCalls(assembled[len(assembled)-1].ToolCalls, tail[0].ToolCalls) {
		return assembled // 已含（同一次压缩里读到的已是新快照）
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

// sameCompactionFailure 判定失败的压缩是否**同一次**：同一上下文版本 + 同一原因
// （即"上次留下的失败痕说的就是这次这件事"）。失败会一直持续到某次压缩真的成功
// （那时上下文版本推进），所以这是"同一次失败只留一条痕"的幂等判据——没有它，一条
// 长期压不下去的会话会按每轮装配追加一条失败痕，把状态页写成流水账。
//
// 只比最后一次：失败痕总在列表末尾追加且从不被改写（成功记录追加在其后），历史顺序
// 即时间顺序，不需要全表扫描。
func sameCompactionFailure(records []model.ContextCompaction, failure model.ContextCompaction) bool {
	if len(records) == 0 {
		return false
	}
	last := records[len(records)-1]
	return last.Failed && last.Version == failure.Version && last.Reason == failure.Reason
}
