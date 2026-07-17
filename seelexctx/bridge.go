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

	"github.com/RedHuang-0622/Seele/engine"
	"github.com/RedHuang-0622/seelex/provider"
	"github.com/RedHuang-0622/seelex/snapshot"
)

// ── 向后兼容导出 ──────────────────────────────────────────────

// Export 从 Engine 导出上下文快照（兼容旧 API）。
func Export(eng *engine.Engine) *snapshot.ContextSnapshot {
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
func ExportWithGoal(eng *engine.Engine, goal string) *snapshot.ContextSnapshot {
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

// ── 向后兼容导入 ──────────────────────────────────────────────

// Import 将上下文快照注入到目标 Engine 的 system prompt 中。
// 注入前自动做预算检查：历史超阈值则用 TrimHistory 截断。
func Import(eng *engine.Engine, snap *snapshot.ContextSnapshot) {
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

func getCurrentSystemPrompt(eng *engine.Engine) string {
	for _, m := range eng.History() {
		if m.Role == "system" && m.Content != nil {
			return *m.Content
		}
	}
	return ""
}
