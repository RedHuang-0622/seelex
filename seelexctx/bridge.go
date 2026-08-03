// Package seelexctx 提供 seele 会话上下文承袭能力。
//
// 子包：
//
//	snapshot/   — ContextSnapshot 类型 + Format + Validate + Builder
//	provider/   — Provider 接口 + EngineProvider + TraceProvider
//	compactor/  — 基于 token 预算的上下文压缩
//	merger/     — 双向上下文合并（MergeBack）
//
// 本文件提供 Export / ExportWithGoal / Import 向后兼容 API，委托给子包实现。
package seelexctx

import (
	"context"
	"log"
	"time"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/seelexctx/provider"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// SessionState 是 Import 需要的会话可变面（*session.Session 满足）。
type SessionState interface {
	History() []types.Message
	ClearHistory()
	SetSystemPrompt(string)
}

// ── 向后兼容导出 ──────────────────────────────────────────────

// Export 从会话导出上下文快照（兼容旧 API，eng 可以是 *session.Session）。
func Export(eng provider.SessionSource) *snapshot.ContextSnapshot {
	if eng == nil {
		return &snapshot.ContextSnapshot{ExportedAt: time.Now()}
	}
	snap, _ := provider.NewEngineProvider(eng).Export(context.TODO())
	if snap == nil {
		return &snapshot.ContextSnapshot{
			SourceSessionID: eng.SessionID(),
			ExportedAt:      time.Now(),
		}
	}
	return snap
}

// ExportWithGoal 导出并显式设置目标（兼容旧 API）。
func ExportWithGoal(eng provider.SessionSource, goal string) *snapshot.ContextSnapshot {
	if eng == nil {
		return &snapshot.ContextSnapshot{ExportedAt: time.Now(), Goal: goal}
	}
	snap, _ := provider.NewEngineProviderWithGoal(eng, goal).Export(context.TODO())
	if snap == nil {
		return &snapshot.ContextSnapshot{
			SourceSessionID: eng.SessionID(),
			ExportedAt:      time.Now(),
			Goal:            goal,
		}
	}
	return snap
}

// ExportSnapshot 导出完整上下文快照：EngineProvider（Goal / MessageCount）
// + TraceProvider（Findings / Decisions / TokenEstimate）组合。
// trace 可为 nil（无遥测时退化为 Engine 导出）。
// 注意：EngineProvider 读取会话 History（需会话锁）——只在 ChatStream 之外
// 使用（会话切换等）；运行中的子代理上下文读取走 ExportSnapshotFromData。
func ExportSnapshot(eng provider.SessionSource, trace provider.TraceSource, goal string) *snapshot.ContextSnapshot {
	if eng == nil {
		return &snapshot.ContextSnapshot{ExportedAt: time.Now(), Goal: goal}
	}
	combined := &snapshot.ContextSnapshot{
		SourceSessionID: eng.SessionID(),
		ExportedAt:      time.Now(),
		Goal:            goal,
	}
	engineSnap, err := provider.NewEngineProviderWithGoal(eng, goal).Export(context.TODO())
	if err == nil && engineSnap != nil {
		combined.MessageCount = engineSnap.MessageCount
		if goal == "" {
			combined.Goal = engineSnap.Goal
		}
	}
	if trace == nil {
		return combined
	}
	traceSnap, err := provider.NewTraceProviderWithGoal(trace, eng.SessionID(), goal).Export(context.TODO())
	if err != nil || traceSnap == nil {
		return combined
	}
	combined.Findings = traceSnap.Findings
	combined.Decisions = traceSnap.Decisions
	combined.TokenEstimate = traceSnap.TokenEstimate
	combined.Escape = traceSnap.Escape
	return combined
}

// ExportSnapshotFromData 从无锁数据面构造完整快照：调用方提供
// sessionID / goal / messageCount（application 镜像等不依赖会话锁的源），
// Findings / Decisions / TokenEstimate 仍从遥测 trace 提取。
// 父证据注入在 plan_run 执行期间发生——此时主会话被 ChatStream 全程持锁，
// 任何 History() 访问都会死锁，因此必须走本导出而非 ExportSnapshot。
func ExportSnapshotFromData(sessionID, goal string, messageCount int, trace provider.TraceSource) *snapshot.ContextSnapshot {
	combined := &snapshot.ContextSnapshot{
		SourceSessionID: sessionID,
		ExportedAt:      time.Now(),
		Goal:            goal,
		MessageCount:    messageCount,
	}
	if trace == nil {
		return combined
	}
	traceSnap, err := provider.NewTraceProviderWithGoal(trace, sessionID, goal).Export(context.TODO())
	if err != nil || traceSnap == nil {
		return combined
	}
	combined.Findings = traceSnap.Findings
	combined.Decisions = traceSnap.Decisions
	combined.TokenEstimate = traceSnap.TokenEstimate
	combined.Escape = traceSnap.Escape
	return combined
}

// ── 向后兼容导入 ──────────────────────────────────────────────

// Import 将上下文快照注入到目标会话的 system prompt 中。
// 注入前自动做预算检查：历史超阈值则用 TrimHistory 截断。
func Import(eng SessionState, snap *snapshot.ContextSnapshot) {
	cfg := DefaultContextConfig()
	hist := eng.History()
	if NeedCompression(hist, cfg.CompressThreshold) {
		log.Printf("[seelexctx] history %d msgs exceeds %d tokens, trimming to %d",
			len(hist), cfg.CompressThreshold, cfg.MaxTokens)
		trimmed := TrimHistory(hist, cfg.MaxTokens)
		eng.ClearHistory()
		for _, m := range trimmed {
			if m.Role == "system" && m.Content != nil {
				eng.SetSystemPrompt(*m.Content)
			}
		}
		hist = trimmed
	}

	currentPrompt := getCurrentSystemPrompt(eng)
	contextBlock := snap.Format()
	newPrompt := currentPrompt
	if currentPrompt != "" {
		newPrompt += "\n\n" + contextBlock
	} else {
		newPrompt = contextBlock
	}
	eng.SetSystemPrompt(newPrompt)
}

func getCurrentSystemPrompt(eng SessionState) string {
	for _, m := range eng.History() {
		if m.Role == "system" && m.Content != nil {
			return *m.Content
		}
	}
	return ""
}
