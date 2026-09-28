package context_runtime

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// 折叠帧正文（会话内容存储里按 frame_ref 回读的那一份）的**规范形状**：
//
//	一帧 = JSON 元数据 + Markdown 读后感
//
// 读后感**不能单独作为压缩帧存在**：它只是帧的一半。另一半是"这一次折叠到底动了
// 什么"的结构化事实——版本号、原因、来源、被折区间、判据量与四区、逐关门禁耗时、
// 细筛句柄。两半合起来才构成一帧，缺元数据的摘要无法审计也无法定位原文，缺摘要的
// 元数据则对检索毫无用处。
//
// 分工按**表示法**切，不按内容切：
//
//	元数据 → JSON。机器要读（检索、审计、对拍），字段名即口径，不靠散文措辞传达事实。
//	读后感 → Markdown。人与模型要读，章节骨架见 seelexctx/frame.go 的 Chapter 2 小节。
//
// 为什么元数据不再写成散文行（v1 的形状是 `reason: x · origin: y`、
// `tokens: compared N → assembled M`）：散文行既不能被 json.Unmarshal，也就不能被
// 任何对拍用例逐字段比对；改一个措辞就改掉了一条事实的表达，而读侧无从察觉。
//
// 与栈帧（sessionstore.CompactFrame）的关系：栈帧是**权威**，它的结构体字段本身就是
// JSON 元数据、它的 Summary 字段就是读后感；本文件渲染的是栈帧的**完整回读投影**
// （快照刻意不带正文，见 application/model/state.go 的 ContextCompaction 注释），
// 不是第二份权威。因此读后感一律原样嵌入 Summary，绝不另写一份——两份措辞会漂移。
//
// 硬约束：JSON 块**只能**出现在这份回读正文里，绝不能进 CompactFrame.Summary。
// OneLineSummary 取 Chapter 2 第一条非标题行作链锚点一句话，以 `{` 开头的 JSON 行
// 会被选中并渲染进下一帧的 Chapter 1，静默污染整条帧链（守卫见
// seelexctx/frame.go 的 OneLineSummary）。

const (
	// compactionFrameMarker 标记 v2 帧正文（内容存储里的正文，不是 wire 消息；
	// 与 seelexctx 的 checkpoint/压缩帧标记无关，不会被 history_safety 清理）。
	compactionFrameMarker = "<!-- seelex:context-checkpoint-frame:v2 -->"
	// compactionFrameMarkerV1 是旧形状（元数据写成散文行）的标记。读侧据此分辨
	// 历史帧：v1 正文仍然可读，只是没有可解析的元数据块。
	compactionFrameMarkerV1 = "<!-- seelex:context-checkpoint-frame:v1 -->"
	// compactionFrameTool 是帧正文在会话内容存储里登记的工具名：前端/审计据此分辨
	// "这不是工具输出，而是折叠那一刻留下的有界 checkpoint 帧"。
	compactionFrameTool = "context_compaction_frame"
	// compactionFrameSchema 是元数据块的 schema 标识（写进 JSON，供读侧判版本）。
	compactionFrameSchema = "seelex.context-compaction-frame/v2"
)

// compactionFrameInput 是渲染帧正文所需的**事实**：全部取自这次折叠本身，
// 不做二次推算（区间取记录值、token 取判据量与装配量）。
type compactionFrameInput struct {
	Version         uint64
	Reason          string
	Origin          string
	At              time.Time
	SegmentID       string // 压缩栈帧标识；空 = 本次没有推帧（栈不可用或推帧失败）
	SummarySource   string // replay（前缀重放厚摘要）| local（本地确定性折叠）| 空 = 没有栈帧
	Summary         string // 读后感（栈帧 Summary 原样；空 → 走本地兜底材料）
	ComparedTokens  int
	AssembledTokens int
	SoftThreshold   int
	HardThreshold   int
	Evidence        string // 有界任务证据摘要（TaskExecutionState.ContextSummary；可能为空）
	PlanMessage     string // 随帧保留的 plan 尾部（可能为空）
	Injected        bool   // 帧正文是否真的进了 provider 历史（自主压缩 = 是）
	// Range 是被折出保留窗口、送进 compact_context 的 transcript 前缀边界。
	Range compactionFoldedRange
	// Layout 是这次装配的四区显式化（分区 + 各区 token 数与来源）与保留窗口决策。
	// 判据量与阈值也在这份里（ContextLayout 自带 ComparedTokens/EstimatedTokens/
	// Soft/Hard），因此元数据块**只嵌 layout 一份**，不再另立 tokens 区块重复同样的
	// 数字——同一事实两处表达就会漂移（layout.go 的单一口径纪律）。
	Layout ContextLayout
	// 逐关门禁耗时（CompactionGateTiming）**刻意不进帧正文**：正文的渲染时刻在
	// store / record 两关收口之前（record.FrameRef 要先落存储才能写记录），那份
	// 清单必然缺最后两关。放一份明知不全的副本进正文，就是本仓库反复防的
	// "报表口径与判据口径分叉"。它的两个既有家不变：进度事件（瞬态）与压缩回执
	// （options.decision.Gates，全部关收口之后才取）。
	//
	// ReadbackToolResults 是本次折叠区间内可继续细筛的工具结果句柄
	// （result:<callID>，read_tool_result 的入参）。
	ReadbackToolResults []string
	// IndexError 是推帧失败的真实原因（空 = 没失败/没尝试/无区间）。有值时正文如实
	// 写出"为什么这一帧没有细筛入口"，不留空段也不假装可读回。
	IndexError string
	// IndexSkipped 报告"索引面已就绪，但这次折叠没有折出任何完整协议单元"（例如
	// 尚未越过任何保留窗口就显式 /compact：区间为空，没有原文可归档）。它与
	// "索引面未启用"是两种事实——前者说明接线是好的、只是这次无事可做，后者说明
	// 这一跳在本宿主上根本不存在。混为一谈会让前者读起来像一次配置事故。
	IndexSkipped bool
}

// compactionFoldedRange 是被折叠区间的边界事实（记录值，不推算）。
// Label 是人读的一行（model.CompactionRangeLabel 的产物）；其余字段是机器读的原始
// 边界，两套并存是因为读侧要按 EventSeq 定位原文，人要一眼看出压了哪一段。
type compactionFoldedRange struct {
	MessageFrom string `json:"message_from,omitempty"`
	MessageTo   string `json:"message_to,omitempty"`
	EventFrom   uint64 `json:"event_from,omitempty"`
	EventTo     uint64 `json:"event_to,omitempty"`
	Units       int    `json:"units,omitempty"`
	Label       string `json:"label,omitempty"`
}

// Empty 报告这份区间没有任何可记录的边界。
func (r compactionFoldedRange) Empty() bool {
	return r.MessageFrom == "" && r.MessageTo == "" && r.EventFrom == 0 && r.EventTo == 0 && r.Units == 0
}

// compactionReadback 是细筛入口：读到这一帧的人/模型据此知道下一跳怎么调。
// 句柄缺失时 Note 如实说明原因，不留空字段让人猜。
type compactionReadback struct {
	CompressedTurn string   `json:"compressed_turn,omitempty"` // read_compressed_turn 的 segment_id 入参
	ToolResults    []string `json:"tool_results,omitempty"`    // read_tool_result 的 ref 入参
	Note           string   `json:"note,omitempty"`
}

// compactionFrameMetadata 是帧正文里那个 JSON 块的结构。字段名即口径。
type compactionFrameMetadata struct {
	Schema        string                 `json:"schema"`
	Version       uint64                 `json:"version"`
	Reason        string                 `json:"reason"`
	Origin        string                 `json:"origin"`
	At            time.Time              `json:"at"`
	SegmentID     string                 `json:"segment_id,omitempty"`
	SummarySource string                 `json:"summary_source,omitempty"`
	Injected      bool                   `json:"injected"`
	Folded        *compactionFoldedRange `json:"folded,omitempty"`
	Layout        ContextLayout          `json:"layout"`
	Readback      compactionReadback     `json:"readback"`
}

// metadata 把渲染输入投影为元数据结构（纯映射，不重算任何数字）。
func (input compactionFrameInput) metadata() compactionFrameMetadata {
	meta := compactionFrameMetadata{
		Schema:        compactionFrameSchema,
		Version:       input.Version,
		Reason:        input.Reason,
		Origin:        input.Origin,
		At:            input.At,
		SegmentID:     input.SegmentID,
		SummarySource: input.SummarySource,
		// Injected 必须如实：普通折叠走保留窗口路径，provider 历史 = 稳定 system
		// 前缀 + 保留窗口 + plan，**并没有**把读后感注入历史；只有自主压缩才注入。
		// 把两者写成同一句话，等于告诉读者"模型看得到这份摘要"，而那是假的。
		Injected: input.Injected,
		Layout:   input.Layout,
		Readback: input.readback(),
	}
	if !input.Range.Empty() {
		rangeValue := input.Range
		meta.Folded = &rangeValue
	}
	return meta
}

// readback 组装细筛入口。segment_id 缺失时如实说明为什么没有这一跳——
// 装配层折叠此前从不推帧，模型对这段区间根本拿不到 read_compressed_turn 的入参，
// 而"看起来有原文、其实没有入口"比明说没有更糟。
func (input compactionFrameInput) readback() compactionReadback {
	out := compactionReadback{
		CompressedTurn: input.SegmentID,
		ToolResults:    input.ReadbackToolResults,
	}
	switch {
	case input.SegmentID != "":
		out.Note = "原文可按 segment_id 分页读回；工具结果按 ref 读回。"
	case input.IndexError != "":
		out.Note = "本次没有压缩栈帧（推帧失败：" + input.IndexError +
			"），因此没有 read_compressed_turn 入口；原始轮次仍在会话存储里，可用 search_history 检索。"
	case input.IndexSkipped:
		out.Note = "本次没有压缩栈帧（这次折叠没有折出任何完整协议单元，没有原文可归档），" +
			"因此没有 read_compressed_turn 入口；原始轮次仍在会话存储里，可用 search_history 检索。"
	default:
		out.Note = "本次没有压缩栈帧（索引面未启用），因此没有 read_compressed_turn 入口；" +
			"原始轮次仍在会话存储里，可用 search_history 检索。"
	}
	return out
}

// compactionFrameBody 渲染折叠帧正文：v2 标记 + JSON 元数据块 + Markdown 读后感块。
//
// 三件事必须都能从这份正文里如实读到：折叠把哪一段折出了 provider 历史（JSON 的
// folded + layout）、模型现在拿到的替代物是什么（injected）、留下的有界证据与细筛
// 入口是什么（readback + 读后感）。
func compactionFrameBody(input compactionFrameInput) string {
	var builder strings.Builder
	builder.WriteString(compactionFrameMarker)
	builder.WriteString("\n# Context checkpoint frame v")
	builder.WriteString(strconv.FormatUint(input.Version, 10))
	builder.WriteString("\n\n")
	builder.WriteString("```json\n")
	builder.WriteString(marshalFrameMetadata(input.metadata()))
	builder.WriteString("\n```\n\n")
	builder.WriteString(input.readingNotes())
	return builder.String()
}

// marshalFrameMetadata 序列化元数据块。这些结构体不含 channel/func，Marshal 不会
// 失败；万一失败也绝不静默丢块——写一个仍然合法的 JSON 对象把错误说出来，
// 读的人知道这里本该有元数据，而不是以为这帧没有元数据。
func marshalFrameMetadata(meta compactionFrameMetadata) string {
	encoded, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		sanitized := strings.NewReplacer(`\`, "", `"`, "", "\n", " ").Replace(err.Error())
		return fmt.Sprintf("{\n  \"schema\": %q,\n  \"error\": %q\n}", compactionFrameSchema, sanitized)
	}
	return string(encoded)
}

// readingNotes 渲染帧的 Markdown 一半。
//
// 有栈帧摘要（Summary 非空）→ **原样嵌入**，不加外层标题：它自带
// 「上一压缩栈摘要 / 压缩内容」两章节标题（seelexctx.RenderFrameSummary），
// 再包一层就成了双重目录；而且另写一份必然与栈帧漂移。
//
// 没有栈帧摘要（开关关闭、前缀重放失败回退本地折叠、或推帧失败）→ 渲染本地兜底
// 材料。这条路是**默认路径**（limits.context_compaction_summary 默认关），所以它
// 不能是空壳：任务证据与 plan 尾部照旧给出，并明说这不是对原文的总结。
func (input compactionFrameInput) readingNotes() string {
	if summary := strings.TrimSpace(input.Summary); summary != "" {
		return summary + "\n"
	}
	var builder strings.Builder
	builder.WriteString("## 折叠材料 (Folded Material)\n\n")
	builder.WriteString("（本次没有模型生成的读后感：折叠摘要开关未开启，或前缀重放失败已回退本地折叠。" +
		"下面是任务台账的有界证据，**不是**对被折原文的总结；原文按上面 readback 段的句柄回读。）\n\n")
	builder.WriteString("### 任务证据检查点 (Task Evidence Checkpoint)\n")
	if evidence := strings.TrimSpace(input.Evidence); evidence != "" {
		builder.WriteString(evidence)
		builder.WriteString("\n")
	} else {
		builder.WriteString("（本次折叠没有可回读的任务证据摘要：objective、检查点证据与工具结果都不足。）\n")
	}
	if plan := strings.TrimSpace(input.PlanMessage); plan != "" {
		builder.WriteString("\n### 计划尾部 (Plan Tail，仍保留在 provider 历史里)\n")
		builder.WriteString(plan)
		builder.WriteString("\n")
	}
	return builder.String()
}
