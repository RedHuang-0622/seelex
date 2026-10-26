package core

// 排队输入的编辑面（调换顺序 / 撤回到输入框）。
//
// 队列只承载「回合运行中」接受的输入（回合收尾时整批提升为下一轮，见
// chat_turn_request.go 的 QueuedRequests/Combine）。因此这两个操作只在
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
// 反面：若在此处对载荷过滤（旧实现按载荷另拼投影，
// 只保留 chatRequest），投影会比真实队列短，用户看到的行与队列项错位——调换会
// "操作成功但列表不变"，撤回会把没显示过的条目交还输入框。执行面（下一轮批量
// 提升，chat_turn_request.go 的 QueuedRequests）与展示面因此解耦：展示要"看得见就能
// 操作"，执行要"载荷类型正确"。

// ReorderQueuedInput 把目标会话排队输入中 from 位置的条目移动到 to 位置
// （调换排队顺序；seq 身份不变，仅位置变化）。目标会话的空 sessionID 表示
// 当前视图会话。
func (frontend *Frontend) ReorderQueuedInput(sessionID string, from, to int) error {
	return frontend.queueComponent().ReorderQueuedInput(sessionID, from, to)
}

// RecallQueuedInput 把目标会话排队输入中 index 位置的条目撤回（出队）并返回
// 其展示原文：调用方（GUI/TUI）把原文交还输入框重新编辑，引擎侧 Payload
// （执行内核的 chatRequest，含每次提交固化的 skill 上下文与预算）随条目一并
// 丢弃——重新提交会产生一份新的固化载荷。目标会话的空 sessionID 表示当前
// 视图会话。
func (frontend *Frontend) RecallQueuedInput(sessionID string, index int) (string, error) {
	return frontend.queueComponent().RecallQueuedInput(sessionID, index)
}
