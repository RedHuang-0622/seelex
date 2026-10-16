package fork

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/RedHuang-0622/Seele/workplan/codec"
	"github.com/RedHuang-0622/Seele/workplan/core/node"
	workplanTypes "github.com/RedHuang-0622/Seele/workplan/core/types"
	"github.com/RedHuang-0622/seelex/seelebridge/plan"
)

// summaryLineLimit 是单行摘要长度上限（按 rune/“字”计数，中文不因
// UTF-8 3 字节/字被压缩）；summaryMaxLines 是每子代理保留的行数。
// 汇总窗口随子代理数 ×n 自然放大（每个子代理独立块），整体是容灾上限
// 而非截断线——完整输出在子代理树/详情，模型如需更多可 read_tool_result。
const (
	// SummaryLineLimit 是单行摘要长度上限（rune 计数）。
	SummaryLineLimit = 160
	// SummaryMaxLines 是每子代理保留的摘要行数。
	SummaryMaxLines = 30
)

const (
	summaryLineLimit = SummaryLineLimit
	summaryMaxLines  = SummaryMaxLines
)

// SummaryNode 是 fork 的汇总节点：把全部前驱节点输出压缩为每子代理
// 一行的紧凑摘要（WorkflowContext.PrevResults，内核收集），作为 fork 最终
// 输出。完整输出不进入对话/历史（避免对话区被子代理大段内容灌满）——
// 子代理树（工作区）与节点详情弹窗承载完整会话/上下文/工具活动。
type SummaryNode struct {
	node.BaseNode
	input plan.SeelexNodeInput
}

// NewSummaryNode 构造 fork summary 节点。
func NewSummaryNode(spec codec.NodeSpec[plan.SeelexNodeInput]) *SummaryNode {
	return &SummaryNode{
		BaseNode: node.NewBaseNode(spec.ID, node.KindMethod),
		input:    spec.Input,
	}
}

func (n *SummaryNode) Run(_ context.Context, wc *workplanTypes.WorkflowContext) (string, error) {
	if wc == nil || len(wc.PrevResults) == 0 {
		return "", nil
	}
	keys := make([]string, 0, len(wc.PrevResults))
	for id := range wc.PrevResults {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("子代理完成情况:\n")
	for _, id := range keys {
		b.WriteString("- ")
		b.WriteString(id)
		b.WriteByte(':')
		summary, fullRunes, truncated := ResultSummaryLines(wc.PrevResults[id])
		if summary == "" {
			b.WriteString(" (无输出)\n")
			continue
		}
		b.WriteByte(' ')
		// 多行用 \n + 缩进续行展示（保持每行可读、整体有界）。
		b.WriteString(strings.ReplaceAll(summary, "\n", "\n  "))
		b.WriteByte('\n')
		if truncated {
			b.WriteString(fmt.Sprintf("  （完整输出 %d 字，超出汇总窗口已截断；完整内容见子代理树，或 read_tool_result 读回）\n", fullRunes))
		}
	}
	b.WriteString("（完整会话/上下文/工具活动见工作区子代理树，点击节点查看详情）")
	return b.String(), nil
}

// ResultSummaryLines 提取子代理输出的有界摘要（rune 计数）；返回摘要、
// 完整输出字数与是否截断——截断时由 summary 节点附注“完整输出大小”，
// 模型据此决定是否需要 read_tool_result 读回，而不是凭空重跑。
func ResultSummaryLines(output string) (summary string, fullRunes int, truncated bool) {
	if decoded := ""; json.Unmarshal([]byte(output), &decoded) == nil {
		output = decoded
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return "", 0, false
	}
	fullRunes = len([]rune(output))
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if runes := []rune(line); len(runes) > summaryLineLimit {
			// 按“字”（rune）截断，中文不再被 3 字节/字白白浪费额度。
			line = string(runes[:summaryLineLimit]) + "…"
			truncated = true
		}
		lines = append(lines, line)
		if len(lines) >= summaryMaxLines {
			truncated = true
			break
		}
	}
	return strings.Join(lines, "\n"), fullRunes, truncated
}
