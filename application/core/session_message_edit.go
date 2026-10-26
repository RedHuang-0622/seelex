package core

import "time"

const (
	editForkOperationTTL = 5 * time.Minute
	editForkMissingSeq   = "这条消息缺少可定位的持久事件序号"
	editForkCompressed   = "该消息已在上下文压缩前缀中，不能编辑"
	editForkRunning      = "父会话正在运行；等待本轮结束后再编辑"
	editForkNoWindow     = "当前上下文没有可编辑的保留窗口"
)

// EditUserMessageAndFork 原子地完成“编辑用户消息 → 从上一完整段落边界 fork →
// 切换到 child → 提交修改后的文本”。调用方不能把它拆成 ForkSession + Submit：
// 本方法用 operation_id 收口双击/重试，并在 child 首轮启动失败时回收本次 child。

// annotateConversationForEditLocked 给可见用户消息补权威 EventSeq 与编辑判据。
// 调用方持有 Core.ViewMu。
