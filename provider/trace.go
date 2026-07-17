package provider

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/RedHuang-0622/Seele/engine"
	"github.com/RedHuang-0622/Seele/seelectx/tracer"
	"github.com/RedHuang-0622/seelex/snapshot"
)

// TraceProvider 实现 Provider 接口，从 tracer.Tree 提取结构信息。
type TraceProvider struct {
	eng  *engine.Engine
	goal string
}

func NewTraceProvider(eng *engine.Engine) *TraceProvider {
	if eng == nil { panic("provider: TraceProvider requires non-nil engine") }
	return &TraceProvider{eng: eng}
}

func (p *TraceProvider) Name() string { return "trace" }

// Export 从 ExportTrace() 获取追踪树并提取结构信息。
func (p *TraceProvider) Export(_ context.Context) (*snapshot.ContextSnapshot, error) {
	tree := p.eng.ExportTrace()
	snap := &snapshot.ContextSnapshot{
		SourceSessionID: p.eng.SessionID(),
		ExportedAt:      time.Now(),
		Goal:            p.goal,
	}
	if tree == nil || tree.Root == nil {
		return snap, nil
	}
	snap.Decisions = make([]snapshot.Decision, 0, 8)
	snap.Findings = make([]string, 0, 8)

	walkTree(tree.Root, func(node *tracer.Node) {
		switch node.Kind {
		case tracer.SpanLLMCall:
			extractLLMInfo(node, snap, tree.TraceID)
		case tracer.SpanToolDispatch:
			extractToolDecision(node, snap)
		}
	})

	if len(snap.Decisions) == 0 { snap.Decisions = nil }
	if len(snap.Findings) == 0 { snap.Findings = nil }
	return snap, nil
}

// ── 树遍历 ────────────────────────────────────────────────────

func walkTree(node *tracer.Node, fn func(*tracer.Node)) {
	if node == nil { return }
	fn(node)
	for _, child := range node.Children { walkTree(child, fn) }
}

// ── llm_call 提取 ─────────────────────────────────────────────

func extractLLMInfo(node *tracer.Node, snap *snapshot.ContextSnapshot, traceID string) {
	if tokenStr, ok := node.Attrs["total_tokens"]; ok {
		if n, err := strconv.Atoi(tokenStr); err == nil { snap.TokenEstimate += n }
	}
	rt := node.Attrs["response_type"]
	switch rt {
	case "text":
		snap.Findings = append(snap.Findings, fmt.Sprintf("LLM 在 %s 中返回文本响应", node.Name))
	case "tool_calls":
		if tcStr, ok := node.Attrs["tool_count"]; ok {
			snap.Findings = append(snap.Findings, fmt.Sprintf("LLM 在 %s 中发起 %s 个工具调用", node.Name, tcStr))
		}
	}
	if model, ok := node.Attrs["model"]; ok {
		snap.Findings = append(snap.Findings, fmt.Sprintf("追踪 %s: 使用模型 %s", traceID, model))
	}
}

// ── tool_dispatch 提取 ─────────────────────────────────────────

func extractToolDecision(node *tracer.Node, snap *snapshot.ContextSnapshot) {
	toolName := node.Attrs["tool"]
	if toolName == "" { return }
	dec := snapshot.Decision{
		What: fmt.Sprintf("调用工具 %s", toolName),
		Why:  fmt.Sprintf("在 %s 中执行", node.Name),
	}
	if args := node.Attrs["arguments"]; args != "" {
		dec.Alternatives = []string{snapshot.Truncate(args, 100)}
	}
	if node.Status == tracer.StatusError {
		if errMsg, ok := node.Attrs["error"]; ok {
			dec.Why = fmt.Sprintf("执行 %s 时出错: %s", node.Name, errMsg)
		}
	}
	snap.Decisions = append(snap.Decisions, dec)
}
