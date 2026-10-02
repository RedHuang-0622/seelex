package context_runtime

import (
	"context"
	"strings"

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
	// SummaryNote 说明"这次为什么没有模型摘要"（空 = 有模型摘要）。帧正文如实
	// 写出它：local 的三种来路（开关关闭 / 无重放素材 / 重放调用失败）从
	// summary_source 一个标记读不出来，而读帧的人恰恰要问的就是这个。
	SummaryNote string
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
	precomputedSummary string,
) compactionIndexPush {
	if c == nil || c.compactionIndex == nil {
		return compactionIndexPush{}
	}
	if len(overflow) == 0 {
		return compactionIndexPush{Attempted: true, Skipped: true}
	}
	// 同会话推帧串行（跨会话不互等）：见 compactionPushLock 的注释——压缩栈是
	// 链式结构，两条并发推帧的后来者必撞锚点校验。
	lock := c.compactionPushLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	receipt, err := c.compactionIndex.PushCompactionFrame(context.Background(), sessionID, CompactionIndexRequest{
		Overflow:           overflow,
		ReplayHistory:      replay,
		PrecomputedSummary: precomputedSummary,
		EventFrom:          window.EventFrom,
		EventTo:            window.EventTo,
		MessageFrom:        window.MessageFrom,
		MessageTo:          window.MessageTo,
		RequestID:          requestID,
	})
	if err != nil {
		return compactionIndexPush{Attempted: true, Err: err}
	}
	return compactionIndexPush{
		Attempted:     true,
		SegmentID:     receipt.SegmentID,
		Summary:       receipt.Summary,
		SummarySource: receipt.SummarySource,
		SummaryNote:   receipt.SummaryNote,
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

// compactionSummaryProbe 是「这次折叠到底能不能拿到模型读后感」的**窄可选**探针。
//
// 为什么要它：一帧的价值分配是「元数据 + 模型读后感」（见 compaction_frame.go 的
// 文件头），缺了读后感的一帧对检索毫无用处，而折叠会改写请求前缀、把 provider 的
// 整段前缀缓存作废。因此当这次折叠注定落成本地确定性折叠时（生效配置里折叠处厚摘要
// 开关关闭 / QuickChat 装配失败 / 这条链路结构上不注入摘要器），正确的动作是**不折**：
// 上下文原样 append、压缩栈顶不动，只把这次判据如实留痕（用户口径 2026-10-01）。
//
// 刻意与 CompactionIndexPort 分开做成**第二个**可选接口：`PushCompactionFrame` 是
// 推帧义务，而"有没有读后感"是宿主配置/装配事实。把它并进 CompactionIndexPort 会
// 迫使每个 fake/harness 都回答这个问题，而它们（以及不关心摘要的宿主）的正确行为
// 本来就是"沿用既有行为 = 折叠照常"。未实现 = 视为可用。
type compactionSummaryProbe interface {
	// CompactionSummaryAvailable 报告本次折叠能否拿到模型生成的读后感（true =
	// 折叠处厚摘要可用，折叠照常；false = 只能本地折叠，因此这次不折）。
	CompactionSummaryAvailable() bool
}

// compactionReadbackProbe 是「折叠**之前**先试一次前缀重放厚摘要」的窄可选探针。
//
// 为什么需要它：compactionSummaryProbe 只能回答「摘要器装没装」——一次**结构**
// 判断。而现场真正的失败是：摘要器装好了、重放调用却在运行时失败（两次尝试全挂
// → chapter2 落回本地确定性折叠，回执 summary_source=local）。等这个事实被知道
// 时，旧路径已经把 agent 的上下文换成了折叠窗口（推帧发生在 replaceFoldHistory
// **之后**），于是「折叠之后看不见上文」——而那一帧只有元数据、对检索毫无用处。
//
// 用户口径（2026-10-02）：折叠只是压缩失败的一条记录——**失败的压缩不该覆盖
// agent 已经看见的上下文**；只有压缩成功（有 llm 读后感返回）才能让 agent 从新的
// compact 栈顶开始上下文。
//
// 因此把「这次到底有没有模型读后感」从结构判断升级成实测读数，且读数发生在折叠
// 之前：读数没有模型读后感 → 这次不折（noSummary），上下文原样 append，只留一条
// 失败痕。读数成功 → 摘要随推帧请求带下去（CompactionIndexRequest.
// PrecomputedSummary），同一次折叠不重复调用模型。
//
// 与 compactionSummaryProbe 一样刻意做成**窄可选**：未实现它的 fake/harness 与
// 不关心摘要的宿主沿用既有行为（折叠照常）——探测失败的正确行为本来就是"按老
// 样子走"，不该强迫每个 fake 长出空方法。
type compactionReadbackProbe interface {
	// ReadbackCompactionSummary 用本次折叠的重放素材跑一次模型回读，返回回执
	// （SummarySource=replay 才算拿到模型读后感）。**不得有副作用**：读不到原文
	// 也不压栈、不归档——折叠还没决定要不要发生。
	ReadbackCompactionSummary(ctx context.Context, sessionID string, request CompactionIndexRequest) (CompactionIndexReceipt, error)
}

// readbackCompactionSummary 在折叠之前试一次模型回读。attempted=false 表示这条
// 链路没有实现探针（沿用既有行为：折叠照常）；attempted=true 时回执就是这次折叠
// 的读后感事实（err 或非 replay 都等于「没拿到」，调用方据此不折）。
//
// overflow 只用于读数这一次 DAG 运行的本地兜底正文（读不到模型读后感时它会被
// 丢弃），因此调用方给「本次可能被折出的区间」即可，不必与最终折叠区间逐字相等。
func (c *Coordinator) readbackCompactionSummary(
	sessionID string,
	overflow, replay []contract.EngineMessage,
) (CompactionIndexReceipt, bool) {
	if c == nil || c.compactionIndex == nil {
		return CompactionIndexReceipt{}, false
	}
	probe, ok := c.compactionIndex.(compactionReadbackProbe)
	if !ok {
		return CompactionIndexReceipt{}, false
	}
	receipt, err := probe.ReadbackCompactionSummary(context.Background(), sessionID, CompactionIndexRequest{
		Overflow:      overflow,
		ReplayHistory: replay,
		RequestID:     "",
	})
	if err != nil {
		// 试了但失败：与「回执里没有模型读后感」同一结论——这次不折。
		return CompactionIndexReceipt{}, true
	}
	return receipt, true
}

// hasModelSummary 报告回执里有没有**模型**读后感：只有 replay 才算一次成功的
// 模型回读，local 是本地确定性折叠（读不到就退化成它）。
func (r CompactionIndexReceipt) hasModelSummary() bool {
	return r.SummarySource == CompactionSummarySourceReplay && strings.TrimSpace(r.Summary) != ""
}

// compactionSummaryAvailable 探测这次折叠能不能拿到模型读后感。索引面未装配、
// 或宿主没实现探针 → 返回 true（沿用既有行为：折叠照常）。
func (c *Coordinator) compactionSummaryAvailable() bool {
	if c == nil || c.compactionIndex == nil {
		return true
	}
	probe, ok := c.compactionIndex.(compactionSummaryProbe)
	if !ok {
		return true
	}
	return probe.CompactionSummaryAvailable()
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
