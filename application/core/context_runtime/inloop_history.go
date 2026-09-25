package context_runtime

import (
	"context"

	"github.com/RedHuang-0622/seelex/application/contract"
)

// loopHistoryChannel 是「这一次折叠/装配发生在正在跑的回合内」的通道值。
//
// 它把引擎的环内历史端口与携带环内把手的 ctx 绑在一起，**只在一次调用内存在**
// （prepareOptions 的字段 / 显式入参），因此不需要把 context 塞进 Coordinator，
// 也不必把满世界的签名都改一遍。nil = 锁外路径（回合之间的自动装配、冷加载后的
// 显式压缩、测试桩），照旧走 Engine 那套会取锁的方法。
//
// 为什么要这条通道：ChatStream 从进函数持锁到出函数，工具 handler 与全部循环
// 回调都在同一 goroutine、同一把锁内。上下文压缩与 provider 历史修复都必须读写
// 引擎历史，走常规方法就是同 goroutine 再抢一次非重入锁 = 永久自锁（实测：会话
// 永远运行中，排队输入再也不发出去）。环内通道不是绕锁——它用的就是那把锁所保护
// 的数据，只是不再二次取锁，所以折叠当场生效：同回合的下一次模型请求读到的已是
// 折叠后的历史，不需要「交给下一次装载」。
type loopHistoryChannel struct {
	engine contract.InLoopEngine
	ctx    context.Context
}

// InLoopChannelFrom 按 ctx 判定这次调用是否在回合内，返回可传给折叠/修复入口的
// 通道值；取不到把手时返回 nil（= 锁外路径）。不做任何「大概是在环内吧」的猜测
// （不用调用计数、时间戳之类推断引擎此刻在不在跑）。
//
// 返回类型刻意不导出：包外只能透传或传 nil，不能自行组装通道。
func InLoopChannelFrom(engine any, ctx context.Context) *loopHistoryChannel {
	if ctx == nil {
		return nil
	}
	inLoop, ok := engine.(contract.InLoopEngine)
	if !ok {
		return nil
	}
	return &loopHistoryChannel{engine: inLoop, ctx: ctx}
}

// foldHistory 读引擎历史：环内走通道（不取锁），否则按 Coordinator 的既有口径
// （会话路由引擎 HistoryFor / 回退活跃引擎 History）。
func (c *Coordinator) foldHistory(channel *loopHistoryChannel, sessionID string) []contract.EngineMessage {
	if channel != nil {
		if history, ok := channel.engine.HistoryInLoop(channel.ctx); ok {
			return history
		}
	}
	return c.engineHistory(sessionID)
}

// replaceFoldHistory 写引擎历史：环内走通道并当场生效；通道明确拒绝时如实返回
// 错误，**不得**回落到取锁方法（回落等于再去拿一次本回合已持有的锁）。
func (c *Coordinator) replaceFoldHistory(channel *loopHistoryChannel, sessionID string, history []contract.EngineMessage) error {
	if channel != nil {
		if handled, err := channel.engine.ReplaceHistoryInLoop(channel.ctx, history); handled {
			return err
		}
	}
	return c.replaceEngineHistory(sessionID, history)
}

// setFoldSystemPrompt 把本会话 system prompt 推进引擎历史：环内不取锁、也不顺带
// 改写进程级 prompt 默认（那是 SetSystemPromptFor 的事）。
func (c *Coordinator) setFoldSystemPrompt(channel *loopHistoryChannel, sessionID, prompt string) {
	if channel != nil {
		if handled, _ := channel.engine.SetSystemPromptInLoop(channel.ctx, prompt); handled {
			return
		}
	}
	c.setEngineSystemPrompt(sessionID, prompt)
}

// foldHistory / replaceFoldHistory 是 HistoryCoordinator 的同款入口（provider
// 历史修复在环内同样不能二次取锁）。
func (h *HistoryCoordinator) foldHistory(channel *loopHistoryChannel, sessionID string) []contract.EngineMessage {
	if channel != nil {
		if history, ok := channel.engine.HistoryInLoop(channel.ctx); ok {
			return history
		}
	}
	return h.engineHistory(sessionID)
}

func (h *HistoryCoordinator) replaceFoldHistory(channel *loopHistoryChannel, sessionID string, history []contract.EngineMessage) error {
	if channel != nil {
		if handled, err := channel.engine.ReplaceHistoryInLoop(channel.ctx, history); handled {
			return err
		}
	}
	return h.replaceEngineHistory(sessionID, history)
}

// withInFlightTail 把「正在飞的那一截」接回折叠产物尾部，只在环内生效（锁外路径
// 原样返回，行为零变化）。
//
// 为什么需要：装配产物由「持久化事件 + 保留窗口」拼成，天然不含本轮还没落定的
// assistant(tool_calls)——它的 tool 结果此刻正由这次工具调用产出。环内替换如果把
// 它丢了，循环紧随其后 append 的 tool 消息就成了孤儿，引擎（Seele InLoop）会直接
// 拒收这次替换。多保留的是**本轮自己**的消息，不参与压缩判据、不改区间与阈值口径。
func (c *Coordinator) withInFlightTail(channel *loopHistoryChannel, existing, assembled []contract.EngineMessage) []contract.EngineMessage {
	if channel == nil {
		return assembled
	}
	tail := inFlightTail(existing)
	if len(tail) == 0 {
		return assembled
	}
	if sameToolCalls(assembled[len(assembled)-1].ToolCalls, tail[0].ToolCalls) {
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
