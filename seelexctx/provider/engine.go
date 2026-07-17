package provider

import (
	"context"
	"time"

	"github.com/RedHuang-0622/Seele/engine"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// EngineProvider 实现 Provider 接口，从 engine.Engine 导出。
type EngineProvider struct {
	eng  *engine.Engine
	goal string
}

func NewEngineProvider(eng *engine.Engine) *EngineProvider {
	if eng == nil {
		panic("provider: EngineProvider requires non-nil engine")
	}
	return &EngineProvider{eng: eng}
}

func NewEngineProviderWithGoal(eng *engine.Engine, goal string) *EngineProvider {
	if eng == nil {
		panic("provider: EngineProvider requires non-nil engine")
	}
	return &EngineProvider{eng: eng, goal: goal}
}

func (p *EngineProvider) Name() string { return "engine" }

// Export 从 Engine 导出上下文。
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
