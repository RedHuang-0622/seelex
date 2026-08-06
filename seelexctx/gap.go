// 真空区覆盖：滑动窗口与压缩内容之间的断档填补（2026-08-06）。
//
// 背景：Session 每次 Chat 前按尾窗预算 Load（token + 轮数双限），只装载
// 最新若干完整轮次；窗口外轮次依赖 CompactStack 摘要承接。但压缩只在会话
// 活跃期的软/硬阈值事件上触发——上次压缩点（栈顶帧 To）之后、本次尾窗
// 之前从未被压缩的轮次，既不在窗口（没被装载）也不在任何压缩帧（没被
// 压缩），冷启动/恢复后从模型请求中永久丢失。这就是滑动窗口与压缩内容
// 之间的真空区。
//
// 方案：Load 时以完整事件流（append-only，含已压缩轮次）为真相源，把
// 未覆盖区间 [栈顶 To+1 .. 尾窗起点-1] 压缩为合并帧（From/To 连续、摘要
// 覆盖真空区轮次、原文可选归档），PushCompact 到会话压缩栈。调参（更早
// 压缩或更大窗口）只能缩小真空区，结构上无法消除：事件在压缩触发间隙
// 持续累积，会话结束时仍可能有未压缩轮次落入尾窗外。本模块在任何参数
// 下都保证「压缩栈覆盖 + 窗口」连续无断档。
package seelexctx

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// GapCoverageOptions 是真空区覆盖的全部输入。
type GapCoverageOptions struct {
	// AllEvents 会话完整事件流（append-only 真相源，含已压缩轮次）。
	AllEvents []sessionstore.Event
	// TailEvents 本次尾窗实际装载的窗口事件（selectEventTail 结果）。
	TailEvents []sessionstore.Event

	// Record 当前压缩栈记录（Snapshot）。
	Record sessionstore.SessionContextRecord

	// Stacks 压缩栈写入目标（nil → 只计算不推送）。
	Stacks CompactStackStore

	// Tokens token 计数（nil → ConservativeTokenCounter）。
	Tokens TokenCounter

	// Turns 真空区轮次原文归档（nil → 不归档，压缩不可读回）。
	Turns TurnArchiver

	// SessionID 帧 SegmentID 溯源前缀（可空）。
	SessionID string
}

// GapCoverageResult 报告覆盖结果。
type GapCoverageResult struct {
	// Covered 本次是否发现并覆盖了真空区。
	Covered bool
	// Frame 推送（或将要推送）的合并帧。
	Frame sessionstore.CompactFrame
	// UncoveredUnits 本次新覆盖的单元数。
	UncoveredUnits int
}

// CoverHistoryGap 检测并覆盖真空区：完整事件单元 vs 压缩栈顶 To vs 尾窗
// 装载量。发现未覆盖区间时构造合并帧（接续栈顶覆盖、摘要含真空区轮次、
// 原文可选归档）并 PushCompact；无真空区或栈顶已覆盖 → Covered=false。
func CoverHistoryGap(ctx context.Context, opts GapCoverageOptions) (GapCoverageResult, error) {
	if len(opts.AllEvents) == 0 {
		return GapCoverageResult{}, nil
	}
	tokens := opts.Tokens
	if tokens == nil {
		tokens = ConservativeTokenCounter{}
	}
	allUnits := sessionstore.CompleteEventUnits(opts.AllEvents)
	tailUnits := sessionstore.CompleteEventUnits(opts.TailEvents)
	totalUnits := len(allUnits)
	loadedUnits := len(tailUnits)

	// 覆盖终点 = 栈顶帧 To（累计单元索引；空栈 = -1）。
	coverageEnd := -1
	if len(opts.Record.CompactStack) > 0 {
		top := opts.Record.CompactStack[len(opts.Record.CompactStack)-1]
		coverageEnd = top.To
	}
	// 尾窗起点前一个单元 = 真空区终点（totalUnits-loadedUnits-1）。
	gapEnd := totalUnits - loadedUnits - 1
	if gapEnd < 0 {
		return GapCoverageResult{}, nil // 全部单元都在窗口内
	}
	gapStart := coverageEnd + 1
	if gapStart > gapEnd {
		return GapCoverageResult{}, nil // 栈顶已覆盖到尾窗起点
	}
	// 保守 clamp：跨空间的单元计数可能轻微错位，只补明确未覆盖的区间。
	if gapStart < 0 {
		gapStart = 0
	}
	uncovered := allUnits[gapStart : gapEnd+1]

	frame, err := buildGapFrame(ctx, opts, uncovered, gapEnd)
	if err != nil {
		return GapCoverageResult{}, fmt.Errorf("seelexctx: build gap frame: %w", err)
	}
	// 去重：新帧 To 不大于栈顶 To（跨 Load 重复覆盖同一区间）→ 跳过。
	if prevTo := coverageEnd; frame.To <= prevTo {
		return GapCoverageResult{}, nil
	}
	if opts.Stacks != nil {
		if err := opts.Stacks.PushCompact(frame); err != nil {
			return GapCoverageResult{}, fmt.Errorf("seelexctx: push gap frame: %w", err)
		}
	}
	return GapCoverageResult{Covered: true, Frame: frame, UncoveredUnits: len(uncovered)}, nil
}

// buildGapFrame 构造真空区合并帧：Summary 内嵌上一栈顶摘要 + 真空区轮次
// 渲染（与控制器 buildCompactFrame 同款合并语义）；From 接续栈顶起点，
// To = 真空区终点（累计单元索引）；Evidence 提取工具结果句柄；Turns 注入
// 时归档原文并携带读回句柄。
func buildGapFrame(
	ctx context.Context,
	opts GapCoverageOptions,
	uncovered [][]sessionstore.Event,
	gapEnd int,
) (sessionstore.CompactFrame, error) {
	var prevTop *sessionstore.CompactFrame
	if len(opts.Record.CompactStack) > 0 {
		top := opts.Record.CompactStack[len(opts.Record.CompactStack)-1]
		prevTop = &top
	}
	var builder strings.Builder
	builder.WriteString("真空区补压缩: 上次压缩后、窗口前未压缩的轮次\n")
	builder.WriteString(fmt.Sprintf("真空区轮次: %d 个完整协议单元", len(uncovered)))
	builder.WriteByte('\n')
	for _, unit := range uncovered {
		builder.WriteString(renderEventUnitLine(unit))
	}
	if prevTop != nil && strings.TrimSpace(prevTop.Summary) != "" {
		builder.WriteString("先前压缩摘要: ")
		builder.WriteString(prevTop.Summary)
		builder.WriteByte('\n')
	}
	summary := strings.TrimSpace(builder.String())

	frame := sessionstore.CompactFrame{
		SegmentID:    gapSegmentID(opts.SessionID),
		From:         0,
		To:           gapEnd,
		Summary:      summary,
		Evidence:     gapEvidence(uncovered),
		CompressedAt: time.Now(),
	}
	if prevTop != nil {
		frame.From = prevTop.From // 合并帧覆盖从栈顶起点开始的连续段
	}
	if opts.Turns != nil {
		ref, err := opts.Turns.StoreTurn(ctx, frame.SegmentID, gapUnitMessages(uncovered))
		if err != nil {
			return sessionstore.CompactFrame{}, err
		}
		frame.Evidence = append(frame.Evidence, sessionstore.EvidenceRef{
			Ref:     ref,
			Summary: "vacuum turns original (read_compressed_turn)",
		})
		frame.Summary += fmt.Sprintf("\n真空区轮次原文可经 read_compressed_turn(segment_id=%s) 读回", frame.SegmentID)
	}
	return frame, nil
}

// gapSegmentID 生成真空区帧段标识（会话溯源前缀，与控制器同风格）。
func gapSegmentID(sessionID string) string {
	prefix := "compact-gap"
	if sessionID != "" {
		prefix = fmt.Sprintf("compact-gap-%s", sessionID)
	}
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixMilli())
}

// gapEvidence 从真空区单元提取证据引用：事件携带 ResultRef 时直接引用，
// 否则按工具调用 ID 推导 result 句柄。
func gapEvidence(uncovered [][]sessionstore.Event) []sessionstore.EvidenceRef {
	var evidence []sessionstore.EvidenceRef
	for _, unit := range uncovered {
		for _, event := range unit {
			if event.Role != "tool" {
				continue
			}
			ref := event.ResultRef
			if ref == "" && event.ToolCallID != "" {
				ref = "result:" + event.ToolCallID
			}
			if ref != "" {
				evidence = append(evidence, sessionstore.EvidenceRef{Ref: ref})
			}
		}
	}
	return evidence
}

// renderEventUnitLine 渲染一个事件单元的单行摘要（与控制器 renderUnitLine
// 同风格：用户输入前 80 字符 + 工具名）。
func renderEventUnitLine(unit []sessionstore.Event) string {
	var builder strings.Builder
	for _, event := range unit {
		switch {
		case event.Role == "user" && event.Content != "":
			content := event.Content
			if len(content) > 80 {
				content = content[:80] + "..."
			}
			builder.WriteString("- 用户: ")
			builder.WriteString(content)
			builder.WriteByte('\n')
		case event.Role == "assistant":
			for _, call := range event.ToolCalls {
				builder.WriteString("- 工具调用: ")
				builder.WriteString(call.Name)
				builder.WriteByte('\n')
			}
		case event.Role == "tool" && event.Name != "":
			builder.WriteString("- 工具结果: ")
			builder.WriteString(event.Name)
			builder.WriteByte('\n')
		}
	}
	return builder.String()
}

// gapUnitMessages 把真空区单元展平为 types.Message（原文归档载荷）。
func gapUnitMessages(uncovered [][]sessionstore.Event) []types.Message {
	total := 0
	for _, unit := range uncovered {
		total += len(unit)
	}
	messages := make([]types.Message, 0, total)
	for _, unit := range uncovered {
		for _, event := range unit {
			message := types.Message{
				Role:             event.Role,
				ReasoningContent: event.ReasoningContent,
				ToolCallID:       event.ToolCallID,
				Name:             event.Name,
			}
			if event.Content != "" {
				content := event.Content
				message.Content = &content
			}
			for _, call := range event.ToolCalls {
				message.ToolCalls = append(message.ToolCalls, types.ToolCall{
					ID: call.ID, Type: "function", Function: types.ToolCallFunction{
						Name: call.Name, Arguments: call.Arguments,
					},
				})
			}
			messages = append(messages, message)
		}
	}
	return messages
}
