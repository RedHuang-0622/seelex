package core

import (
	"context"
	"log"
	"strings"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/switch"
)

// ── 后台作业终态触发对话 ────────────────────────────────────────────────
//
// 用户口径（2026-10-01）：后台作业（bash_bg / read_batch / subagent）落到**终态**
// 时，为它所属会话起一个回合，让主代理自己去 `bg_manage(op=fetch)` 取回结果并继续。
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
// 幂等与重试：句柄由各表自己的单调序号铸成（见 dto.JobNamespace）且永不复用，因此进程内一个 handle 集合就够，同一条
// 作业只触发一次。因为会话忙而跳过的条目**刻意不记入集合**——下一次信号（新字节 /
// 别的作业终态 / 驱逐）还会再试。集合随登记表裁剪（见下），因此它的规模被登记表封顶。
//
// 开关：`limits.async_exec.trigger_conversation`（默认 false = 关，与 async_exec
// 同一套"关就是关"的纪律）；关闭时调用方连扫描都不做。

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
		if !switching.AsyncCompletionTriggers(record.State) {
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
		if err := service.submitConversationFor(context.Background(), sessionID, switching.AsyncCompletionPrompt(record, Limits().EvidenceChars)); err != nil {
			log.Printf("[async] 后台作业 %s 终态触发对话失败（session=%s）：%v", handle, sessionID, err)
		}
	}
	for handle := range triggered {
		if _, ok := present[handle]; !ok {
			delete(triggered, handle)
		}
	}
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

// teamworkJobCompletions / teamworkJobEvents 是 teammate 作业表的**宿主转发**：
// 判据（窄可选端口读取）已在 M5-R6-async-completion 归位 `middle/switch`
// （plan §2.4）；core 内部与 work_table.go 的调用点因此零改动。
func teamworkJobCompletions(runtime contract.RuntimePort) []dto.TeamworkJobCompletionRecord {
	return switching.TeamworkJobCompletions(runtime)
}

func teamworkJobEvents(runtime contract.RuntimePort) <-chan struct{} {
	return switching.TeamworkJobEvents(runtime)
}

// ── teammate 作业终态触发对话（2026-10-04）────────────────────────────
//
// 用户口径：「teammate 干完了、内容我取回来了，但不是像 subagent 那样**自动**回来的」。
// 根因是**两张作业表只有一张接了这条链**：subagent / bash_bg / read_batch 活在 tools 的
// 后台执行登记表（上面那套 AsyncRunsSnapshot + AsyncRunEvents），teammate 作业活在
// Seele 的 jobs.Manager 里（contract.TeamworkJobCompletion），于是"跑完了"这件事在
// teammate 侧既不在投影里、也不在信号口上——同一件事两条链只有一条会醒。
//
// 本函数把第二张表接上，口径与上面逐条对齐：终态（done/failed）触发、killed 不触发、
// **绝不唤醒忙会话**（忙会话走回合边界打点块，见 work_table_async.go 的 teamworkTraceLines）、
// 幂等（句柄单调不复用，进程内一个集合就够）、随登记表裁剪。
//
// 一处**刻意不同**：正文指向 `jobs_manage` 与 `team_context`，不是 `bg_manage`——
// 两张表的取回工具不是同一个，指错了模型就会去一个取不到这条作业的地方捞。

// triggerTeamworkJobCompletions 对 teammate 作业表做一次全量扫描：把"终态 + 会话空闲 +
// 还没触发过"的作业各自起一个回合。
//
// 与 triggerAsyncCompletions 同一形状（全量扫描 + 幂等账 + 只唤醒空闲会话）：登记表是
// 唯一事实源，信号只承诺"有事发生"。
func (service *Service) triggerTeamworkJobCompletions(triggered map[string]struct{}) {
	records := switching.TeamworkJobCompletions(service.Deps.Runtime)
	if len(records) == 0 {
		return
	}
	present := make(map[string]struct{}, len(records))
	for _, record := range records {
		handle := strings.TrimSpace(record.Handle)
		if handle == "" {
			continue
		}
		key := switching.TeamworkTriggeredPrefix + handle
		present[key] = struct{}{}
		if !switching.AsyncCompletionTriggers(record.State) {
			continue
		}
		sessionID := strings.TrimSpace(record.SessionID)
		if sessionID == "" {
			continue
		}
		if _, done := triggered[key]; done {
			continue
		}
		if !service.sessionIdleForAsyncTrigger(sessionID) {
			// 忙会话不唤醒（铁律 §6.1）：不记入集合，留待下一次信号重试；忙会话那一轮
			// 的**回合边界**会从打点块看到这条完成行（teamworkTraceLines）。
			continue
		}
		triggered[key] = struct{}{}
		if err := service.submitConversationFor(context.Background(), sessionID, switching.TeamworkCompletionPrompt(record, Limits().EvidenceChars)); err != nil {
			log.Printf("[teamwork] teammate 作业 %s 终态触发对话失败（session=%s）：%v", handle, sessionID, err)
		}
	}
	for key := range triggered {
		if !strings.HasPrefix(key, switching.TeamworkTriggeredPrefix) {
			continue
		}
		if _, ok := present[key]; !ok {
			delete(triggered, key)
		}
	}
}
