package memory

import (
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"
)

// RenderMemoryBlock 把选中的记忆渲染为单个「相关记忆」PromptBlock。
// 以 maxTokens 为总预算（逐条按剩余预算截断，EstimateTokens 估算）；
// 无选中 → 返回 nil。块内明确标注 score 只用于排序、不作为事实。
func RenderMemoryBlock(selected []Candidate, maxTokens int) *seelectx.PromptBlock {
	if len(selected) == 0 {
		return nil
	}
	if maxTokens <= 0 {
		maxTokens = DefaultOptions().MaxTokens
	}
	var builder strings.Builder
	builder.WriteString("## 相关记忆 (Related Memories)\n")
	builder.WriteString("> 从历史压缩段选取的与当前请求相关的过往记忆；排序分数仅用于选取，不作为事实。\n")
	remaining := maxTokens - seelectx.EstimateTokens(builder.String())
	if remaining <= 0 {
		return blockFrom(&builder)
	}
	for _, candidate := range selected {
		if remaining <= 0 {
			break
		}
		line := renderCandidateLine(candidate, remaining)
		if line == "" {
			continue // 固定开销超预算：跳过该条（不截断到空）
		}
		builder.WriteString(line)
		remaining -= seelectx.EstimateTokens(line)
	}
	return blockFrom(&builder)
}

// renderCandidateLine 渲染一条记忆：段标识 + 摘要（按 budget token 截断）
// + 证据引用。固定部分本身超预算 → 返回空串（调用方跳过）。
func renderCandidateLine(candidate Candidate, budget int) string {
	head := "- " + candidate.SegmentID + fmt.Sprintf(" [%d..%d]: ", candidate.From, candidate.To)
	headTokens := seelectx.EstimateTokens(head)
	if headTokens > budget {
		return ""
	}
	var line strings.Builder
	line.WriteString(head)
	summary := strings.TrimSpace(candidate.Summary)
	if summary != "" {
		line.WriteString(truncateTokens(summary, budget-headTokens))
	} else {
		line.WriteString("(无摘要)")
	}
	if len(candidate.Evidence) > 0 {
		refs := make([]string, 0, len(candidate.Evidence))
		for _, evidence := range candidate.Evidence {
			if evidence.Ref != "" {
				refs = append(refs, evidence.Ref)
			}
		}
		if len(refs) > 0 {
			evidenceText := "; 证据: " + strings.Join(refs, ", ")
			if seelectx.EstimateTokens(line.String()+evidenceText) <= budget {
				line.WriteString(evidenceText)
			}
		}
	}
	line.WriteByte('\n')
	return line.String()
}

// blockFrom 从 builder 构造 memory PromptBlock。
func blockFrom(builder *strings.Builder) *seelectx.PromptBlock {
	content := builder.String()
	return &seelectx.PromptBlock{
		Name:     "memory",
		Messages: []types.Message{{Role: "user", Content: &content}},
	}
}

// truncateTokens 按 token 预算截断文本（EstimateTokens 估算；末尾加省略标记）。
func truncateTokens(text string, maxTokens int) string {
	if maxTokens <= 0 {
		return ""
	}
	if seelectx.EstimateTokens(text) <= maxTokens {
		return text
	}
	const marker = "…"
	if seelectx.EstimateTokens(marker) > maxTokens {
		return ""
	}
	var out strings.Builder
	for _, r := range text {
		next := out.String() + string(r)
		if seelectx.EstimateTokens(next+marker) > maxTokens {
			break
		}
		out.WriteRune(r)
	}
	return out.String() + marker
}
