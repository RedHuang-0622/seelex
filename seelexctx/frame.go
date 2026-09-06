// 帧摘要两章节契约与纯函数（docs/2026-09-06-compaction-dag/design.md §3.2）。
//
// CompactFrame.Summary 仍是单字符串，但内部固定两章节：
//
//	## 上一压缩栈摘要 (Previous Compact Stack)   —— 链锚点（元数据视图）
//	## 压缩内容 (Compacted Context)               —— 模型主要消费的厚内容
//
// 模型请求只渲染栈顶帧的 Chapter 2（FrameChapter2）；Chapter 1 的锚点与
// requestID 索引在 CompactStack 记录中维护，默认不进请求正文。旧记录
// Summary 不含章节标记时，FrameChapter2 退化为返回整段摘要（向后兼容）。
package seelexctx

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// ── 章节标题（帧摘要固定两章节）────────────────────────────────────

const (
	// CompactChapter1Title 是 Chapter 1（链锚点）的 Markdown 标题。
	CompactChapter1Title = "上一压缩栈摘要 (Previous Compact Stack)"
	// CompactChapter2Title 是 Chapter 2（厚内容）的 Markdown 标题。
	CompactChapter2Title = "压缩内容 (Compacted Context)"
)

// ── 摘要来源 / 锚点质量标记（详设 §4.5 兜底标记）────────────────────

const (
	// CompactSummarySourceReplay 表示 Chapter 2 由前缀重放生成（厚摘要）。
	CompactSummarySourceReplay = "replay"
	// CompactSummarySourceLocal 表示 Chapter 2 由本地确定性折叠生成（薄摘要）。
	CompactSummarySourceLocal = "local"
	// CompactAnchorSourceOK 表示链锚点完整（含一句话摘要）。
	CompactAnchorSourceOK = "ok"
	// CompactAnchorSourceDegraded 表示链锚点降级：prev_segment_id 与
	// request 首尾照记，一句话摘要留空。
	CompactAnchorSourceDegraded = "degraded"
)

// ── Chapter 2 小节骨架（DSH 风格；空章节写 (none)，不丢骨架）────────

const (
	Chapter2SectionGoal         = "目标 (Goal)"
	Chapter2SectionKeyConcepts  = "关键概念 (Key Concepts)"
	Chapter2SectionFilesAndCode = "文件与代码 (Files and Code)"
	Chapter2SectionErrorsFixes  = "错误与修复 (Errors and Fixes)"
	Chapter2SectionPending      = "待办 (Pending)"
	Chapter2SectionCurrentWork  = "当前工作 (Current Work)"
	Chapter2SectionNextStep     = "下一步 (Next Step)"
	Chapter2SectionConstraints  = "关键约束 (Constraints)"

	// compactEmptySection 是空章节的固定占位（(none)）。
	compactEmptySection = "(none)"
	// maxOneLineRunes 是锚点一句话摘要的 rune 上限（截断加省略号）。
	maxOneLineRunes = 80
)

// LocalFoldKind 区分本地确定性折叠的 Current Work 文案（controller/gap 共用）。
type LocalFoldKind int

const (
	// CompactFoldOverflow 是窗口外溢出轮次折叠（控制器路径）。
	CompactFoldOverflow LocalFoldKind = iota
	// CompactFoldGap 是真空区补压缩折叠（gap 路径）。
	CompactFoldGap
)

// LocalFoldOptions 是本地确定性折叠的全部输入（controller/gap 共用）。
type LocalFoldOptions struct {
	// Overflow 本次新覆盖的完整协议单元（原文消息）。
	Overflow []historyUnit
	// UnitCount 本次新覆盖的单元数（0 → 用 len(Overflow)）。
	UnitCount int
	// Kind 决定 Current Work 的轮次文案（溢出 / 真空区）。
	Kind LocalFoldKind
	// PrevTop 上一栈顶帧（非 nil 时把其 Chapter 2 正文并入 Current Work，
	// 维持本地折叠路径的栈顶自足；不复制 Chapter 1 锚点，避免递归嵌套）。
	PrevTop *sessionstore.CompactFrame
	// Record 会话记录快照（任务/计划栈用于目标、概念与约束小节）。
	Record sessionstore.SessionContextRecord
}

// LocalChapter2 生成 Chapter 2 正文（不含外层 "## 压缩内容" 标题）：
// 确定性本地折叠，按 DSH 小节骨架输出；空小节写 (none)。
func LocalChapter2(opts LocalFoldOptions) string {
	sections := []string{
		sectionHeading(Chapter2SectionGoal) + sectionBody(localGoal(opts.Record)),
		sectionHeading(Chapter2SectionKeyConcepts) + sectionBody(localKeyConcepts(opts.Record)),
		sectionHeading(Chapter2SectionFilesAndCode) + sectionBody(localFilesAndCode(opts.Overflow)),
		sectionHeading(Chapter2SectionErrorsFixes) + sectionBody(compactEmptySection),
		sectionHeading(Chapter2SectionPending) + sectionBody(compactEmptySection),
		sectionHeading(Chapter2SectionCurrentWork) + sectionBody(localCurrentWork(opts)),
		sectionHeading(Chapter2SectionNextStep) + sectionBody(compactEmptySection),
		sectionHeading(Chapter2SectionConstraints) + sectionBody(localConstraints(opts.Record)),
	}
	return strings.Join(sections, "\n\n")
}

// Chapter2Skeleton 返回全部小节为 (none) 的骨架正文（保留章节骨架，
// 供输出缺失/空结果的占位使用）。
func Chapter2Skeleton() string {
	return LocalChapter2(LocalFoldOptions{})
}

func sectionHeading(title string) string { return "### " + title + "\n" }

// sectionBody 归一化小节正文：空 → (none)；去首尾空白。
func sectionBody(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return compactEmptySection
	}
	return body
}

func localGoal(record sessionstore.SessionContextRecord) string {
	if len(record.TaskStack) == 0 {
		return ""
	}
	return strings.TrimSpace(record.TaskStack[len(record.TaskStack)-1].Objective)
}

func localKeyConcepts(record sessionstore.SessionContextRecord) string {
	if len(record.PlanStack) == 0 {
		return ""
	}
	top := record.PlanStack[len(record.PlanStack)-1]
	title := strings.TrimSpace(top.Title)
	if title == "" {
		return top.Status
	}
	if top.Status != "" {
		return title + " (" + top.Status + ")"
	}
	return title
}

// localFilesAndCode 从溢出单元提取工具名（assistant ToolCalls 与 tool 消息，
// 保持首次出现顺序；无 → 空走 (none)）。
func localFilesAndCode(overflow []historyUnit) string {
	seen := make(map[string]struct{})
	var names []string
	add := func(name string) {
		if name == "" {
			return
		}
		if _, duplicate := seen[name]; duplicate {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	for _, unit := range overflow {
		for _, message := range unit.messages {
			for _, call := range message.ToolCalls {
				add(call.Function.Name)
			}
			if message.Role == "tool" {
				add(message.Name)
			}
		}
	}
	if len(names) == 0 {
		return ""
	}
	lines := make([]string, 0, len(names))
	for _, name := range names {
		lines = append(lines, "- 工具: "+name)
	}
	return strings.Join(lines, "\n")
}

// localCurrentWork 渲染当前工作小节：轮次计数 + 逐单元行 + 上一栈顶的
// Chapter 2 正文（本地折叠保持栈顶自足的近似手段）。
func localCurrentWork(opts LocalFoldOptions) string {
	count := opts.UnitCount
	if count <= 0 {
		count = len(opts.Overflow)
	}
	var builder strings.Builder
	if count > 0 || len(opts.Overflow) > 0 {
		roundLabel := "溢出轮次"
		if opts.Kind == CompactFoldGap {
			roundLabel = "真空区轮次"
		}
		builder.WriteString(fmt.Sprintf("%s: %d 个完整协议单元", roundLabel, count))
		if len(opts.Overflow) > 0 {
			builder.WriteByte('\n')
			for _, unit := range opts.Overflow {
				builder.WriteString(renderUnitLine(unit.messages))
			}
		}
	}
	if opts.PrevTop != nil {
		if previousBody := strings.TrimSpace(FrameChapter2(*opts.PrevTop)); previousBody != "" {
			builder.WriteString("先前压缩摘要: ")
			builder.WriteString(previousBody)
		}
	}
	return strings.TrimSpace(builder.String())
}

func localConstraints(record sessionstore.SessionContextRecord) string {
	var parts []string
	if len(record.PlanStack) > 0 {
		top := record.PlanStack[len(record.PlanStack)-1]
		parts = append(parts, "计划状态: "+top.Status)
	}
	if len(record.TaskStack) > 0 {
		top := record.TaskStack[len(record.TaskStack)-1]
		parts = append(parts, "任务状态: "+top.Status)
	}
	return strings.Join(parts, "\n")
}

// ── 帧摘要拼装与截取 ──────────────────────────────────────────────

// RenderFrameSummary 拼装帧 Summary：Chapter 1（锚点正文，无标题）+ Chapter 2
// （正文，无标题）。两者都会被包上固定 Markdown 标题，形成两章节单字符串。
func RenderFrameSummary(anchor string, chapter2 string) string {
	return "## " + CompactChapter1Title + "\n" + strings.TrimSpace(anchor) +
		"\n\n## " + CompactChapter2Title + "\n" + strings.TrimSpace(chapter2)
}

// RenderAnchorChapter 生成 Chapter 1 锚点正文（无 "## " 标题）：上一帧的
// segment_id + request 首尾 + 一句话摘要；prev 为 nil（首帧）→ 显式占位。
func RenderAnchorChapter(prev *sessionstore.CompactFrame) string {
	if prev == nil {
		return "（首帧：无上一压缩栈）"
	}
	var builder strings.Builder
	builder.WriteString("segment_id: ")
	builder.WriteString(prev.SegmentID)
	builder.WriteByte('\n')
	builder.WriteString("request: ")
	if prev.RequestFrom != "" && prev.RequestTo != "" {
		builder.WriteString(prev.RequestFrom + " .. " + prev.RequestTo)
	} else {
		builder.WriteString("(none)")
	}
	builder.WriteByte('\n')
	builder.WriteString("一句话: ")
	builder.WriteString(OneLineSummary(*prev))
	return builder.String()
}

// FrameChapter1 返回帧 Summary 的 Chapter 1 正文；旧记录（无章节标记）
// 返回空串。
func FrameChapter1(frame sessionstore.CompactFrame) string {
	return frameChapter(frame.Summary, "## "+CompactChapter1Title)
}

// FrameChapter2 返回帧 Summary 的 Chapter 2 正文；旧记录（无章节标记）
// 退化为整段摘要（模型可见范围保持兼容）。
func FrameChapter2(frame sessionstore.CompactFrame) string {
	if body := frameChapter(frame.Summary, "## "+CompactChapter2Title); body != "" {
		return body
	}
	return strings.TrimSpace(frame.Summary)
}

// frameChapter 截取 title 之后的正文，止于下一个 "## " 标题。
func frameChapter(summary, title string) string {
	if summary == "" {
		return ""
	}
	index := strings.Index(summary, title)
	if index < 0 {
		return ""
	}
	body := summary[index+len(title):]
	if next := strings.Index(body, "\n## "); next >= 0 {
		body = body[:next]
	}
	return strings.TrimSpace(body)
}

// OneLineSummary 从帧内容提取一句话摘要（锚点用）：优先取 Chapter 2 正文
// 第一条非空、非标题、非注释内容行，rune 超限截断；不可提取 → 空串
// （调用方据此标记 anchor_source=degraded）。
func OneLineSummary(frame sessionstore.CompactFrame) string {
	candidates := append(strings.Split(FrameChapter2(frame), "\n"),
		strings.Split(frame.Summary, "\n")...)
	for _, line := range candidates {
		line = strings.TrimSpace(line)
		if line == "" || line == compactEmptySection {
			continue
		}
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "<!--") ||
			strings.HasPrefix(line, ">") || strings.HasPrefix(line, "- ") {
			continue
		}
		return truncateRunes(line, maxOneLineRunes)
	}
	return ""
}

// FrameAnchorSource 计算链锚点质量标记：无前驱 → 空；有前驱且能提取
// 一句话 → ok；否则 degraded。
func FrameAnchorSource(prev *sessionstore.CompactFrame) string {
	if prev == nil {
		return ""
	}
	if OneLineSummary(*prev) == "" {
		return CompactAnchorSourceDegraded
	}
	return CompactAnchorSourceOK
}

// ChatQueueRequestLabels 把 ChatQueue 单元累计索引映射为 request 覆盖标签
// （chat-N，1 基）。帧 RequestFrom/RequestTo 只作超上下文索引元数据，
// 不进模型正文；事件流携带 TaskID 的路径（gap）优先用真实 requestID。
func ChatQueueRequestLabels(from, to int) (string, string) {
	if to < from {
		return "", ""
	}
	if from < 0 {
		from = 0
	}
	return fmt.Sprintf("chat-%d", from+1), fmt.Sprintf("chat-%d", to+1)
}

func truncateRunes(value string, maxRunes int) string {
	if utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxRunes-1]) + "…"
}
