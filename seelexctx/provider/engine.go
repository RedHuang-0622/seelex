package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// SessionSource 是会话语义的最小读取面（*session.Session 满足）。
// seelexctx 不依赖具体的 Seele 会话实现，适配新装配模型。
type SessionSource interface {
	History() []types.Message
	SessionID() string
}

// EngineProvider 实现 Provider 接口，从会话（session.Session）导出。
type EngineProvider struct {
	eng  SessionSource
	goal string
}

func NewEngineProvider(eng SessionSource) *EngineProvider {
	if eng == nil {
		panic("provider: EngineProvider requires non-nil session source")
	}
	return &EngineProvider{eng: eng}
}

func NewEngineProviderWithGoal(eng SessionSource, goal string) *EngineProvider {
	if eng == nil {
		panic("provider: EngineProvider requires non-nil session source")
	}
	return &EngineProvider{eng: eng, goal: goal}
}

func (p *EngineProvider) Name() string { return "engine" }

// Export 从会话导出上下文。
func (p *EngineProvider) Export(_ context.Context) (*snapshot.ContextSnapshot, error) {
	hist := p.eng.History()
	snap := &snapshot.ContextSnapshot{
		SourceSessionID: p.eng.SessionID(),
		ExportedAt:      time.Now(),
		MessageCount:    len(hist),
	}
	if p.goal == "" {
		for _, m := range hist {
			if m.Role == "user" && m.Content != nil {
				snap.Goal = snapshot.Truncate(*m.Content, 200)
				break
			}
		}
	} else {
		snap.Goal = p.goal
	}
	return snap, nil
}

// HistoryProvider 实现 Provider 接口，从 seelectx.DurableHistory 导出
// （sessionstore 适配；plan.md §2.5：provider 读 DurableHistory /
// SessionRecord，不再依赖旧 engine.History()）。
type HistoryProvider struct {
	history   seelectx.DurableHistory
	sessionID string
	goal      string
}

// NewHistoryProvider 构造基于 DurableHistory 的导出器。
func NewHistoryProvider(history seelectx.DurableHistory, sessionID string) *HistoryProvider {
	return &HistoryProvider{history: history, sessionID: sessionID}
}

// NewHistoryProviderWithGoal 构造并显式设置目标。
func NewHistoryProviderWithGoal(history seelectx.DurableHistory, sessionID, goal string) *HistoryProvider {
	return &HistoryProvider{history: history, sessionID: sessionID, goal: goal}
}

// Name 实现 Provider。
func (p *HistoryProvider) Name() string { return "durable-history" }

// Export 从持久化历史导出上下文快照（Load 失败显式返回错误）。
func (p *HistoryProvider) Export(ctx context.Context) (*snapshot.ContextSnapshot, error) {
	if p == nil || p.history == nil {
		return nil, fmt.Errorf("provider: history provider requires durable history")
	}
	hist, err := p.history.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("provider: load durable history: %w", err)
	}
	snap := &snapshot.ContextSnapshot{
		SourceSessionID: p.sessionID,
		ExportedAt:      time.Now(),
		MessageCount:    len(hist),
	}
	if p.goal == "" {
		for _, m := range hist {
			if m.Role == "user" && m.Content != nil {
				snap.Goal = snapshot.Truncate(*m.Content, 200)
				break
			}
		}
	} else {
		snap.Goal = p.goal
	}
	return snap, nil
}
