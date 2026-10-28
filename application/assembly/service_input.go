package assembly

import (
	"context"
	"github.com/RedHuang-0622/seelex/contract"
)

// discardPendingSubagentContexts 排空 Runtime 持有的有界邮箱（活跃会话兼容
// 包装）。
func (service *Service) discardPendingSubagentContexts() {
	// 会话 ID 在 Core.ViewMu 读锁下取值：本方法是锁外入口（测试/活跃会话兼容
	// 包装），裸读会与热切换写 Snapshot.Session 竞争。
	service.ViewMu.RLock()
	sessionID := service.Core.Snapshot.Session.ID
	service.ViewMu.RUnlock()
	service.discardPendingSubagentContextsFor(sessionID)
}

// discardPendingSubagentContextsFor 排空 Runtime 持有的有界邮箱（单一来源 =
// Runtime mailbox）并**丢弃全部内容**：子代理结论经工具结果（tool_result /
// summary / NodeSemanticResult）回传父代理，mailbox 不再作为“注入引擎历史
// + 写入可见会话/transcript”的通道。保留排空是为了让 Runtime 的 mailbox
// 保持有界；任何残余载荷都不进入前端、上下文或持久化存储。
func (service *Service) discardPendingSubagentContextsFor(_ string) {
	_ = service.Deps.Runtime.DrainSubagentContexts()
}

// chatStream 向指定会话引擎提交流式对话（会话路由引擎用 ChatStreamFor，
// 否则回退活跃引擎）。
func (service *Service) chatStream(ctx context.Context, sessionID, input string, onChunk func(string)) (string, error) {
	if routed, ok := contract.RoutedSessionEngine(service.Deps.Engine); ok {
		return routed.ChatStreamFor(sessionID, ctx, input, onChunk)
	}
	return service.Deps.Engine.ChatStream(ctx, input, onChunk)
}

// replaceEngineHistory 会话内替换指定会话引擎历史（会话路由引擎用
// ReplaceHistoryFor，不切活跃；否则回退契约 ReplaceHistory）。
func (service *Service) replaceEngineHistory(sessionID string, history []contract.EngineMessage) error {
	if routed, ok := contract.RoutedSessionEngine(service.Deps.Engine); ok {
		return routed.ReplaceHistoryFor(sessionID, history)
	}
	return service.Deps.Engine.ReplaceHistory(sessionID, history)
}

// engineHistoryFor 返回指定会话引擎历史（只读拷贝）。
func (service *Service) engineHistoryFor(sessionID string) []contract.EngineMessage {
	if routed, ok := contract.RoutedSessionEngine(service.Deps.Engine); ok {
		return routed.HistoryFor(sessionID)
	}
	return service.Deps.Engine.History()
}

// clearEngineHistoryFor 清空指定会话引擎历史（会话路由引擎用 ClearHistoryFor，
// 不切换活跃别名）。不要用 Engine.ClearHistory() 代替：进程级别名指向哪个会话
// 不可预期，可能是另一个正在运行的会话，其 framework Session 锁被 ChatStream
// 全程持有——清空会排在它后面，并在它释放后清掉它的运行历史。
func (service *Service) clearEngineHistoryFor(sessionID string) {
	if routed, ok := contract.RoutedSessionEngine(service.Deps.Engine); ok {
		routed.ClearHistoryFor(sessionID)
		return
	}
	service.Deps.Engine.ClearHistory()
}

// submitConversationFor 在指定（后台）会话提交对话：目标会话运行中则投递
// 到该会话自己的队列，否则在其上下文中后台启动（不切换活跃会话）。
// BeginGracefulShutdown 停止接收新输入，同时允许活跃 chat 及其已排队输入
// 自然完成。

// CancelAllChats 取消全部会话的运行中回合（G0c 关闭超时路径：后台会话同样
// 占用引擎与持久化通道，不能只取消视图会话）。取消后每个 runChat 走正常
// 收尾（逐会话 persist），随后 WaitForIdle 自然收敛。

// CancelChat 是"停止按钮"的终止原语，语义按以下顺序成立（顺序即语义）：
//
//  1. 先停在跑的前台工具调用：取消会话回合的 ctx，引擎 ReAct 循环与正在执行的前台
//     工具（同步 bash 及其派生的整棵进程树）随之中止——工具调用不会在停止之后继续
//     跑完，也不会再按住收尾等它（见 backend/tool/registry 的进程树终止原语）。
//  2. 不动后台子进程：bash_bg / read_batch / subagent 作业用 context.WithoutCancel
//     摘掉了回合 ctx，生死只由 bg_manage(op=kill) 与硬上限决定；点停止不杀它们。
//  3. 清空消息队列并全部发出去：排队输入在回合收尾整批提升为下一轮（见 runChat），
//     停止不会把队列一起丢掉——用户已经发过的消息一条都不会少发。
//
// requestID 只作参考，不作为否决条件：渲染层持有的 request_id 可能滞后一个事件
// tick（排队回合刚提升时尤其明显），而一个会话同时只有一个运行中回合，"停掉我
// 看到在跑的那个回合"与"停掉本会话当前回合"是同一件事。此前这一判断由桌面 Bridge
// 用空 id 重试兜底，属于业务语义，收在本服务内（TUI/CLI/headless 同样受益）。
