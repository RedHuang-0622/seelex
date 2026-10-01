package core

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// ── 后台作业终态触发对话 ────────────────────────────────────────────────
//
// 用户口径（2026-10-01）：后台作业（bash_bg / read_batch / subagent）落到**终态**
// 时，为它所属会话起一个回合，让主代理自己去 `job_manage(op=fetch)` 取回结果并继续。
// **done 与 failed 都触发**；killed 不触发（那不是"作业有了结果"，是被终止）；
// running 不是终态。
//
// 为什么需要一个"起回合"的路径：作业面从设计起就是**轮询型**——模型答完就停，结果
// 由模型自己的下一次工具调用取回（docs/2026-09-24-async-tool-deferred-ack/README.md
// §10.5「跨回合无人取回：模型答完就停，本轮之内仍无唤醒链路」）。于是长时间后台作业
// 完成后没人去取，用户必须再敲一句。本函数补的就是这一步：终态 → 空闲会话起回合。
//
// 与既有铁律的边界（docs/arch/teamwork-leader-worker-architecture.md §6.1「绝不唤醒
// 忙会话」）：这里**只在会话空闲时**起回合。正在跑的会话不被打断，也不往它的队列里
// 塞东西——它的下一次回合边界本来就会在请求尾部打点块里看到这条完成行
// （work_table_async.go 的完成行回填）。二者因此是互补的：忙会话走打点块，空闲会话
// 走这里。
//
// 驱动器是 consumeAsyncRuns 收到的那个变化信号（work_table.go）：信号口是容量 1 的
// 单接收者通道，不能再起一个消费者去抢同一次发送。因此本文件的入口是一个**被调用的
// 函数**，而不是一个 goroutine。
//
// 幂等与重试：句柄 `a<seq>` 单调且永不复用，因此进程内一个 handle 集合就够，同一条
// 作业只触发一次。因为会话忙而跳过的条目**刻意不记入集合**——下一次信号（新字节 /
// 别的作业终态 / 驱逐）还会再试。集合随登记表裁剪（见下），因此它的规模被登记表封顶。
//
// 开关：`limits.async_exec.trigger_conversation`（默认 false = 关，与 async_exec
// 同一套"关就是关"的纪律）；关闭时调用方连扫描都不做。

// asyncStateDone / asyncStateFailed 是触发口径认的两个终态。
//
// 字面量与 seelebridge 的登记表同源（dto.AsyncRunRecord.State 的注释：
// running | done | failed | killed），这里不新增第二套状态机，只是给触发口径起名。
const (
	asyncStateDone   = "done"
	asyncStateFailed = "failed"
)

// triggerAsyncCompletions 对登记表做一次全量扫描：把"终态 + 会话空闲 + 还没触发过"
// 的作业各自起一个回合。
//
// 走全量扫描而不是"信号里带载荷"：登记表是唯一事实源，信号只承诺"有事发生"
// （见 contract.RuntimePort.AsyncRunEvents 的注释）。全量扫描一次是 O(在册作业数)，
// 而它只在真的有变化时跑。
//
// triggered 是调用方（consumeAsyncRuns 的那个 goroutine）持有的句柄账，只有它一个
// goroutine 读写，因此不需要加锁。
func (service *Service) triggerAsyncCompletions(triggered map[string]struct{}) {
	records := service.Deps.Runtime.AsyncRunsSnapshot()
	present := make(map[string]struct{}, len(records))
	for _, record := range records {
		handle := strings.TrimSpace(record.Handle)
		if handle == "" {
			continue
		}
		// 裁剪账本：登记表里已经没有的句柄（被取回 / 销项 / 驱逐）从集合里去掉，
		// 集合因此被登记表规模封顶，不会随进程存活时长无限增长。
		present[handle] = struct{}{}
		if !asyncCompletionTriggers(record.State) {
			continue
		}
		sessionID := strings.TrimSpace(record.SessionID)
		// 没有会话归属就没有回合的落点：信号本身是广播，孤儿作业不是错误。
		if sessionID == "" {
			continue
		}
		if _, done := triggered[handle]; done {
			continue
		}
		if !service.sessionIdleForAsyncTrigger(sessionID) {
			// 忙会话不唤醒（铁律 §6.1）；不记入集合，留待下一次信号重试。
			continue
		}
		// 先记账再提交：提交失败（关闭中/排空中）不该让每一次后续信号都重放一次。
		triggered[handle] = struct{}{}
		if err := service.submitConversationFor(context.Background(), sessionID, asyncCompletionPrompt(record)); err != nil {
			log.Printf("[async] 后台作业 %s 终态触发对话失败（session=%s）：%v", handle, sessionID, err)
		}
	}
	for handle := range triggered {
		if _, ok := present[handle]; !ok {
			delete(triggered, handle)
		}
	}
}

// asyncCompletionTriggers 报告某个状态是否该触发对话。
// done / failed 触发（用户口径 2026-10-01）；killed 与 running 都不触发。
func asyncCompletionTriggers(state string) bool {
	return state == asyncStateDone || state == asyncStateFailed
}

// sessionIdleForAsyncTrigger 报告目标会话此刻是否**空闲到可以起一个新回合**。
//
// 判据只有一条：会话在会话域注册表里，且它的 ChatState 不在运行中。restoring
// （正在冷加载）与 compacting（正在跑一轮压缩）刻意不在这里判——提交入口
// （submitConversationFor）对这两种状态本来就有"延后到安全点再开回合"的语义
// （service_input.go 的恢复门与 context_compact_gate.go 的压缩门），在这里再判一次
// 等于把同一条规则写两遍，且会丢掉"等它收口后仍然触发"这个正确行为。
//
// 为什么"不在注册表"也算不可触发：注册表里没有这个会话 = 本进程没有它的回合落点
// （别的进程的作业归属、或会话已被删除）。这时**不能**交给 submitConversationFor ——
// 它会经 sessionUnitLocked 按需建单元，等于凭一条作业记录复活一个陌生会话。
func (service *Service) sessionIdleForAsyncTrigger(sessionID string) bool {
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	if service.closed || service.draining {
		return false
	}
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		return false
	}
	return !unit.ChatState().Running
}

// asyncCompletionPrompt 组装触发回合的正文。
//
// 它是一条**用户行**（走 submitConversationFor 的正常输入路径），因此必须自带上下文：
// 用户会看到"为什么这里多了一轮"，模型也需要知道去取哪一条。字段刻意收窄到
// handle / kind / state / exit / 有界摘要 / 有界标题——与打点块同一份口径
// （绝对路径、末行原文、时间戳都不进上下文，见 dto.AsyncRunRecord 的字段分层）。
func asyncCompletionPrompt(record dto.AsyncRunRecord) string {
	var body strings.Builder
	body.WriteString("后台作业已完成，请取回结果并继续：\n")
	fmt.Fprintf(&body, "- handle: %s（kind=%s）\n", record.Handle, asyncPromptKind(record.Kind))
	fmt.Fprintf(&body, "- 状态: %s · exit=%d\n", record.State, record.ExitCode)
	if title := strings.TrimSpace(record.Description); title != "" {
		fmt.Fprintf(&body, "- 作业: %s\n", truncateWorkEvidence(title, 200))
	}
	if summary := strings.TrimSpace(record.Summary); summary != "" {
		fmt.Fprintf(&body, "- 摘要: %s\n", truncateWorkEvidence(summary, Limits().EvidenceChars))
	}
	fmt.Fprintf(&body, "取回全文用 job_manage(op=fetch, handle=%q)；确认不需要了用 job_manage(op=done, handle=%q) 销项。",
		record.Handle, record.Handle)
	return body.String()
}

// asyncPromptKind 是进正文前的类别兜底：类别缺失时按 process 读，与打点块同口径。
func asyncPromptKind(kind string) string {
	if trimmed := strings.TrimSpace(kind); trimmed != "" {
		return trimmed
	}
	return "process"
}
