package context_runtime

import (
	"context"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/model"
)

// 装配层折叠（prepareExecutionContextFor）把折出保留窗口的区间推进会话压缩栈的
// **调用侧**。接收侧（生成栈帧、归档原文、PushCompact 的链锚校验）在 seelebridge；
// 本文件只做两件事：按合约提问，以及把回执/降级原因如实记进帧正文与门禁。
//
// 为什么必须接通这一条：压缩栈（SessionContextRecord.CompactStack）此前只有两个
// 生产写入方——回合内控制器与真空区覆盖；而实际最常发生的折叠是装配层那条
// （软线 / /compact / compact_context），它只写快照侧的 ContextCompactions 与
// 内容存储里的帧正文，**不推栈**。栈空则三处同时失效：记忆块初筛返回 nil、
// search_history 退化为尾部扫描、真空区覆盖被主动跳过。
//
// 降级方向天然安全：这是**窄可选**能力（CompactionIndexPort，见 ports.go），
// 未装配（Deps.CompactionIndex == nil）或推帧失败都不影响折叠与本次请求。但
// "没有 segment_id" 的两种原因必须分开记账——**没尝试**（索引面未启用）与
// **试了失败**（带真实原因），否则读帧的人会把"索引面没接"当成"推帧出错"，
// 或者反过来以为"没报错就是推上了"。

// compactionIndexPush 是一次推帧尝试的事实面。帧正文的 readback 段与门禁的
// index 关都读这一份，两处不会各说各话。
type compactionIndexPush struct {
	// Attempted 报告是否真的问过索引面：false = 未装配（没尝试），
	// true 且 Err != nil = 试了但失败。这一个布尔量就是两种降级的判别依据。
	Attempted bool
	// Skipped 报告"索引面在，但这次没有可推的区间"（溢出为空）。它不是降级：
	// 没有区间就没有原文，推一帧空档只会让检索命中一个读不回来的段。
	Skipped bool
	// SegmentID 是栈帧标识（read_compressed_turn 的必选入参）；空 = 没有推成帧。
	SegmentID string
	// Summary 是栈帧的两章节摘要正文（Markdown 读后感），帧正文原样嵌入它。
	Summary string
	// SummarySource 是摘要来源：replay（前缀重放厚摘要）| local（本地确定性折叠）。
	SummarySource string
	// Err 是推帧失败的真实原因（nil = 没失败）。
	Err error
}

// pushCompactionFrame 把这次折叠折出保留窗口的区间推进会话压缩栈。
//
// 素材全部取自折叠那一刻手里已有的事实，**不重算**：
//   - overflow：transcript 里被折出保留窗口的那一段（events[from:to]）——使用
//     provider 侧已发出的字节（TranscriptEventMessages 走 ProviderContent 优先），
//     不是视图文本；
//   - replay：上一次真实请求的历史字节（引擎历史，与产出该请求是同一条装配路径，
//     见 seelebridge.MainCompactionDAG 的注释），前缀重放摘要据此共享 provider
//     缓存；从事件流重拼会丢掉这份字节一致性，因此绝不那样做；
//   - 区间：TranscriptPrefixRange 的记录值。EventSeq 是装配层手里的**权威**区间
//     事实，单元下标由接收侧按 Seq 反查（两者不是减法关系）。
func (c *Coordinator) pushCompactionFrame(
	sessionID, requestID string,
	overflow, replay []contract.EngineMessage,
	window task_context.TranscriptEventRange,
) compactionIndexPush {
	if c == nil || c.compactionIndex == nil {
		return compactionIndexPush{}
	}
	if len(overflow) == 0 {
		return compactionIndexPush{Attempted: true, Skipped: true}
	}
	receipt, err := c.compactionIndex.PushCompactionFrame(context.Background(), sessionID, CompactionIndexRequest{
		Overflow:      overflow,
		ReplayHistory: replay,
		EventFrom:     window.EventFrom,
		EventTo:       window.EventTo,
		MessageFrom:   window.MessageFrom,
		MessageTo:     window.MessageTo,
		RequestID:     requestID,
	})
	if err != nil {
		return compactionIndexPush{Attempted: true, Err: err}
	}
	return compactionIndexPush{
		Attempted:     true,
		SegmentID:     receipt.SegmentID,
		Summary:       receipt.Summary,
		SummarySource: receipt.SummarySource,
	}
}

// gateDetail 渲染门禁 index 关的 Detail：这一步的**事实**（有没有尝试、成没成、
// 标识与摘要来源、真实的降级原因）。门禁 Detail 只写该关的数字/标识事实，中文
// 文案留在前端（见 compaction_progress.go）。
func (p compactionIndexPush) gateDetail() string {
	switch {
	case !p.Attempted:
		return "index=unavailable"
	case p.Skipped:
		return "index=skipped reason=no_overflow"
	case p.Err != nil:
		return "index=error err=" + p.Err.Error()
	case p.SegmentID == "":
		return "segment=(none) source=" + p.sourceLabel()
	default:
		return "segment=" + p.SegmentID + " source=" + p.sourceLabel()
	}
}

// sourceLabel 报告摘要来源；缺省写 (none) 而不是留空——空段会被读成"格式没写对"，
// 与"来源未声明"是两件事。
func (p compactionIndexPush) sourceLabel() string {
	if p.SummarySource == "" {
		return "(none)"
	}
	return p.SummarySource
}

// indexError 返回推帧失败的真实原因（空 = 没失败、也没跳过）。帧正文据此如实写出
// "为什么这一帧没有细筛入口"，不留空段也不假装可读回。
func (p compactionIndexPush) indexError() string {
	if p.Err == nil {
		return ""
	}
	return p.Err.Error()
}

// foldedOverflowEvents 截取被折出保留窗口的 transcript 区间（events[from:to]）。
//
// from 之前的区间已被更早的帧覆盖（帧链自足，见 seelebridge 的 carry 语义），
// 重复喂进去只会让检索命中两段同内容；to 之后是本次保留的窗口。两个边界都做
// 越界钳制：它们是不同来源的下标（累积起点 / 保留窗口起点 / transcript 长度），
// 这里只负责取一段合法切片，不重算边界。
func foldedOverflowEvents(events []model.TranscriptEvent, from, to int) []model.TranscriptEvent {
	if from < 0 {
		from = 0
	}
	if to > len(events) {
		to = len(events)
	}
	if from > to {
		from = to
	}
	return events[from:to]
}
