package seelebridge

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// 装配层压缩（application/core/context_runtime，回合开始前那条路径）把帧推进会话
// 压缩栈的落地点。
//
// 为什么需要这一层：压缩栈（SessionContextRecord.CompactStack）此前只有两个生产
// 写入方——回合内控制器（seelexctx/controller.go）与真空区覆盖（seelexctx/gap.go）。
// 而实际最常发生的压缩是装配层那条（95% 软线 / /compact / compact_context），它只写
// 快照侧的 ContextCompactions 与内容存储里的帧正文，**不推栈**。栈空则三处同时失效：
// 记忆块初筛返回 nil、search_history 退化为尾部扫描最近 300 单元、真空区覆盖被主动
// 跳过。本文件接通的就是这一条。
//
// 帧的规范形状（JSON 元数据 + Markdown 读后感）由调用方渲染进内容存储；这里只负责
// **栈帧**这一半——栈帧是权威，回读正文是它的投影。

// CompactionFrameRequest 是装配层压缩推帧的入参（wire 中立类型已由 adapter 转换完
// 毕，本层只见 types.Message，不反向依赖 application/contract）。
type CompactionFrameRequest struct {
	// Overflow 是被折出保留窗口的完整协议单元原文。
	Overflow []types.Message
	// UnitCount 是 Overflow 里的完整协议单元数（0 → 由 DAG 切分推导）。
	UnitCount int
	// ReplayHistory 是上一次真实请求的历史字节（前缀重放素材）。空 → 本地压缩。
	ReplayHistory []types.Message
	// PrecomputedSummary 是调用方在压缩之前已经拿到的模型读后感（装配层读数闸，
	// 见 context_runtime 的 compactionReadbackProbe）：非空 → 不再调用模型，直接
	// 用它当这次的 Chapter 2。
	//
	// 语义是 **Chapter 2 正文**：读数闸的回执天然带整份两章节摘要，交到这里之前
	// 由调用侧归一化（Chapter2Body）——整份摘要当正文用会让帧出现双重标题，且
	// 再读回来时正文被裁成空。
	PrecomputedSummary string
	// PrecomputedSummarySource / PrecomputedSummaryNote 是上面那份预读读后感的
	// 来源与原因（空 Source → 按 replay）。两者必须一起带下来：预读若落在本地
	// 确定性压缩上，帧要如实写 local + 那条原因，而不是盖成 replay 再把原因丢掉。
	PrecomputedSummarySource string
	PrecomputedSummaryNote   string
	// EventFrom/EventTo 是被折区间的 transcript 事件序号（含端点；0 = 无边界可记）。
	// 这是装配层唯一能给出的**权威**区间事实，检索侧据此按 Seq 反查单元下标。
	EventFrom uint64
	EventTo   uint64
	// MessageFrom/MessageTo 是被折区间的 UI 消息号（可空）。
	MessageFrom string
	MessageTo   string
}

// CompactionFrameReceipt 是推帧回执。
type CompactionFrameReceipt struct {
	SegmentID     string
	Summary       string
	SummarySource string
	// SummaryNote 说明"这次为什么没有模型摘要"（空 = 这次真有模型摘要）。只有落到
	// 本地确定性压缩的帧才有：它把 `summary_source=local` 的三种来路（开关关闭 /
	// 无重放素材 / 重放调用失败）分开，并带上失败时的真实报错——否则读帧的人只能靠
	// 猜，而"模型为什么没被叫到"正是这条链上唯一无法从帧形状反推的事实。
	SummaryNote string
}

// frameSummaryNote 从帧证据里读出降级原因（ref 前缀 `compact-local:` 的那条，由
// seelexctx 的 chapter2Node 写入）。没有这条证据 → 返回空串：不编原因，
// 也不把"没有解释"说成"没有降级"。
func frameSummaryNote(frame sessionstore.CompactFrame) string {
	for _, evidence := range frame.Evidence {
		if strings.HasPrefix(evidence.Ref, seelexctx.LocalCompactEvidenceRefPrefix) {
			return strings.TrimSpace(evidence.Summary)
		}
	}
	return ""
}

// CompactionSummaryAvailable 报告装配层压缩这次能不能拿到**模型生成的读后感**
// （前缀重放厚摘要）。它是 context_runtime 的窄可选探针 compactionSummaryProbe 的
// 实现：false → 这次压缩注定落成本地确定性压缩（帧只有元数据、对检索无用，却会
// 作废一段 provider 前缀缓存），装配层因此**不折上下文、不推压缩栈顶**，只把这次
// 判据如实留痕（用户口径 2026-10-01：只有出了读后感才更新 compact stack top）。
//
// 与 MainCompactionDAG 同源：两者读同一个 compactionSummarizer——探针 true 就等于
// DAG 的 Summarizer 非 nil，探针 false 就等于 chapter2Node 一定走本地压缩。
func (r *Runtime) CompactionSummaryAvailable() bool {
	return r.compactionSummarizer() != nil
}

// ReadbackCompactionSummary 在压缩**之前**试一次前缀重放厚摘要，把「这次到底有
// 没有模型读后感」从结构判断（CompactionSummaryAvailable）升级成实测读数。
//
// 它是 context_runtime 的窄可选探针 compactionReadbackProbe 的实现：装配层据此
// 决定这次压缩要不要发生——读不到模型读后感就不折上下文（用户口径 2026-10-02：
// 失败的压缩不该覆盖 agent 已经看见的上下文，只有出了读后感才让 agent 从新的
// compact 栈顶开始上下文）。
//
// **不得有副作用**：这里只跑 DAG 拿 Chapter 2 与降级事实，不归档原文、不压栈
// ——压缩还没决定要不要发生。回执里的 segment_id 因此为空：真正的读回入口由随后
// 的 PushCompactionFrame 给出。同一次压缩不会重复调用模型：读数拿到的正文经
// CompactionFrameRequest.PrecomputedSummary 原样交给推帧。
//
// 回执的 Summary 是 **Chapter 2 正文**（不是整份两章节摘要）——它的下一个消费者
// 是"这次的 Chapter 2"那个字段，边界在这里对齐；SummarySource / SummaryNote 同样
// 原样带下去，推帧据此如实标注来源与"为什么没有模型读后感"。
func (r *Runtime) ReadbackCompactionSummary(
	ctx context.Context,
	_ string,
	request CompactionFrameRequest,
) (CompactionFrameReceipt, error) {
	if r == nil {
		return CompactionFrameReceipt{}, errors.New("compaction index: runtime is unavailable")
	}
	store := r.sessionContextStore()
	if store == nil {
		return CompactionFrameReceipt{}, errors.New("compaction index: 会话上下文存储未绑定（压缩栈不可用）")
	}
	frame, err := r.MainCompactionDAG().Execute(ctx, seelexctx.CompactionInput{
		Record:             store.Snapshot(),
		Messages:           request.Overflow,
		UnitCount:          request.UnitCount,
		History:            request.ReplayHistory,
		PrecomputedSummary: request.PrecomputedSummary,
		Kind:               seelexctx.LocalCompactOverflow,
	})
	if err != nil {
		return CompactionFrameReceipt{}, fmt.Errorf("compaction index: readback summary: %w", err)
	}
	// Summary 交回的是 **Chapter 2 正文**，不是整份帧摘要：调用方（装配层）把它
	// 存成 precomputedSummary、再当"这次的 Chapter 2"交回推帧（见
	// CompactionFrameRequest.PrecomputedSummary）。整份摘要当正文用会让帧出现双重
	// 标题，而且再读回来时正文被裁成空——现场那句「压缩成功，帧却是一具空骨架」
	// 就是这条边界错位。整份帧摘要仍留在 frame.Summary 里（推帧回执才用它）。
	return CompactionFrameReceipt{
		Summary:       seelexctx.FrameChapter2(frame),
		SummarySource: frame.SummarySource,
		SummaryNote:   frameSummaryNote(frame),
	}, nil
}

// PushCompactionFrame 生成一帧并压入该会话的压缩栈，返回回执。
//
// 三步都不允许"静默半成"：DAG 失败 → 返回错误（调用方只记日志、压缩照常）；
// 归档失败 → 同样返回错误，因为一帧没有原文归档就没有 read_compressed_turn 入口，
// 推上去只是让检索命中一个读不回来的区间；PushCompact 失败 → 返回错误（链锚点或
// 区间校验不通过时它会给出具体的拒绝原因，不猜）。
func (r *Runtime) PushCompactionFrame(
	ctx context.Context,
	sessionID string,
	request CompactionFrameRequest,
) (CompactionFrameReceipt, error) {
	if r == nil {
		return CompactionFrameReceipt{}, errors.New("compaction index: runtime is unavailable")
	}
	store := r.sessionContextStore()
	if store == nil {
		return CompactionFrameReceipt{}, errors.New("compaction index: 会话上下文存储未绑定（压缩栈不可用）")
	}
	frame, err := r.MainCompactionDAG().Execute(ctx, seelexctx.CompactionInput{
		Record:    store.Snapshot(),
		Messages:  request.Overflow,
		UnitCount: request.UnitCount,
		History:   request.ReplayHistory,
		// 压缩之前已经拿到的模型读后感（装配层读数闸）：非空则 chapter2 不再调用
		// 模型，同一次压缩只发生一次模型调用。来源与原因一起带下去——预读落在本地
		// 确定性压缩上时，帧要如实写 local + 那条原因（见 CompactionFrameRequest）。
		PrecomputedSummary:       request.PrecomputedSummary,
		PrecomputedSummarySource: request.PrecomputedSummarySource,
		PrecomputedSummaryNote:   request.PrecomputedSummaryNote,
		Kind:                     seelexctx.LocalCompactOverflow,
		// RequestFrom/RequestTo 刻意留空。装配层折掉的前缀跨多个更早的回合，
		// 而 PushCompact 要求这两个字段按**字符串序**非倒置；回合标识不保证字典序
		// 单调（"task-9" > "task-10"），填了就可能被拒。留空是校验允许的组合，
		// 代价是下一帧的 Chapter 1 锚点这一跳写成 request: (none) —— 区间定位
		// 由下面的 EventFrom/EventTo 承担，它才是权威事实。
	})
	if err != nil {
		return CompactionFrameReceipt{}, fmt.Errorf("compaction index: build frame: %w", err)
	}
	// 区间事实由调用方给出（DAG 只在单元空间里算 From/To，它不知道 EventSeq）。
	// 两个 EventSeq 必须同零或同非零，否则 PushCompact 拒绝半填充区间。
	if request.EventFrom > 0 && request.EventTo > 0 && request.EventFrom <= request.EventTo {
		frame.EventFrom = request.EventFrom
		frame.EventTo = request.EventTo
	}
	frame.MessageFrom = request.MessageFrom
	frame.MessageTo = request.MessageTo
	// 原文归档：没有它，这一帧对模型就只是"可被检索命中但读不回来"。
	// 姿势与 gap.go 一致（Evidence 追加读回句柄 + Summary 追加工具提示），不另写一套。
	if archiver := r.getTurnArchiver(); archiver != nil {
		ref, archiveErr := archiver.StoreTurn(ctx, frame.SegmentID, request.Overflow)
		if archiveErr != nil {
			return CompactionFrameReceipt{}, fmt.Errorf("compaction index: archive turns: %w", archiveErr)
		}
		frame.Evidence = append(frame.Evidence, sessionstore.EvidenceRef{
			Ref:     ref,
			Summary: "assembly-compacted turns original (read_compressed_turn)",
		})
		frame.Summary += fmt.Sprintf("\n已压缩轮次原文可经 read_compressed_turn(segment_id=%s) 读回", frame.SegmentID)
	}
	if err := store.PushCompact(frame); err != nil {
		return CompactionFrameReceipt{}, fmt.Errorf("compaction index: push frame: %w", err)
	}
	_ = sessionID // 会话归属由绑定的 store 决定；入参只用于调用方日志与将来的多栈路由
	return CompactionFrameReceipt{
		SegmentID:     frame.SegmentID,
		Summary:       frame.Summary,
		SummarySource: frame.SummarySource,
		SummaryNote:   frameSummaryNote(frame),
	}, nil
}
