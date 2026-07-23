package application

import (
	"testing"
)

func TestPromptStack_PushAndRender(t *testing.T) {
	ps := NewPromptStack()
	ps.Push("base", "base", "base prompt")
	ps.Push("effort", "high", "high instructions")
	ps.Push("skill", "goal", "goal prompt")

	rendered := ps.Render()
	if !contains(rendered, "base prompt") {
		t.Errorf("Render missing base prompt")
	}
	if !contains(rendered, "high instructions") {
		t.Errorf("Render missing effort instructions")
	}
	if !contains(rendered, "goal prompt") {
		t.Errorf("Render missing skill prompt")
	}
	if !contains(rendered, "---") {
		t.Errorf("Render missing separator")
	}
}

func TestPromptStack_Pop(t *testing.T) {
	ps := NewPromptStack()
	ps.Push("base", "base", "base prompt")
	ps.Push("skill", "goal", "goal prompt")
	ps.Push("skill", "code", "code prompt")

	// Pop by name
	if !ps.Pop("goal") {
		t.Error("Pop goal should return true")
	}
	// code should still be there
	if !contains(ps.Render(), "code prompt") {
		t.Error("code prompt should remain after popping goal")
	}
	// goal should be gone
	if contains(ps.Render(), "goal prompt") {
		t.Error("goal prompt should be removed after pop")
	}
}

func TestPromptStack_PopKind(t *testing.T) {
	ps := NewPromptStack()
	ps.Push("base", "base", "base prompt")
	ps.Push("skill", "goal", "goal prompt")
	ps.Push("skill", "code", "code prompt")
	ps.Push("effort", "high", "high instructions")

	// PopKind removes the LAST skill (LIFO)
	name := ps.PopKind("skill")
	if name != "code" || !contains(ps.Render(), "goal prompt") {
		t.Errorf("PopKind should return last skill 'code', got %q", name)
	}

	// Effort still there
	if !ps.Has("effort") {
		t.Error("effort should remain")
	}
}

func TestPromptStack_Reset(t *testing.T) {
	ps := NewPromptStack()
	ps.Push("base", "base", "base prompt")
	ps.Push("skill", "goal", "goal prompt")

	ps.Reset("new base")
	if ps.Count() != 1 {
		t.Errorf("Reset should leave 1 layer, got %d", ps.Count())
	}
	if ps.Render() != "new base" {
		t.Errorf("Reset should set new base text, got %q", ps.Render())
	}
}

func TestPromptStack_ClearKind(t *testing.T) {
	ps := NewPromptStack()
	ps.Push("base", "base", "base prompt")
	ps.Push("skill", "goal", "goal prompt")
	ps.Push("skill", "code", "code prompt")

	ps.ClearKind("skill")
	if ps.Has("skill") {
		t.Error("skills should be cleared")
	}
	if !ps.Has("base") {
		t.Error("base should remain")
	}
}

func TestPromptStack_Describe(t *testing.T) {
	ps := NewPromptStack()
	ps.Push("base", "base", "base")
	desc := ps.Describe()
	if desc != "base" {
		t.Errorf("empty describe should be 'base', got %q", desc)
	}

	ps.Push("effort", "high", "high")
	desc = ps.Describe()
	if desc != "E:high" {
		t.Errorf("with effort should be 'E:high', got %q", desc)
	}

	ps.Push("skill", "goal", "goal")
	desc = ps.Describe()
	if desc != "E:high  goal" {
		t.Errorf("expected 'E:high  goal', got %q", desc)
	}
}

func TestPromptStack_Empty(t *testing.T) {
	ps := NewPromptStack()
	if ps.Render() != "" {
		t.Error("empty stack should render empty")
	}
	if ps.PopKind("anything") != "" {
		t.Error("PopKind on empty stack should return empty")
	}
}

func TestEffortApply_ValidLevels(t *testing.T) {
	ps := NewPromptStack()
	ps.Push("base", "base", "base prompt")
	eng := &mockEngine{}
	em := NewEffortManager(ps, eng)

	levels := []string{"lite", "medium", "high", "max"}
	for _, level := range levels {
		if err := em.Apply(level); err != nil {
			t.Errorf("Apply %q should not error: %v", level, err)
		}
		if em.Current() != level {
			t.Errorf("Current() = %q, want %q", em.Current(), level)
		}
	}
}

func TestEffortApply_InvalidLevel(t *testing.T) {
	ps := NewPromptStack()
	ps.Push("base", "base", "base prompt")
	em := NewEffortManager(ps, &mockEngine{})

	if err := em.Apply("invalid"); err == nil {
		t.Error("Apply invalid level should error")
	}
}

func TestEffortApply_UpdatesPromptStack(t *testing.T) {
	ps := NewPromptStack()
	ps.Push("base", "base", "base prompt")
	eng := &mockEngine{}
	em := NewEffortManager(ps, eng)

	em.Apply("high")
	if !ps.Has("effort") {
		t.Error("effort layer should exist after Apply")
	}

	em.Apply("lite")
	if ps.Has("effort") {
		t.Error("effort layer should be removed for lite effort")
	}
}

func TestEffortApply_SetsMaxLoops(t *testing.T) {
	ps := NewPromptStack()
	ps.Push("base", "base", "base prompt")
	eng := &mockEngine{}
	em := NewEffortManager(ps, eng)

	em.Apply("lite")
	if eng.maxLoops != 20 {
		t.Errorf("lite effort should set MaxLoops=20, got %d", eng.maxLoops)
	}
	em.Apply("medium")
	if eng.maxLoops != 64 {
		t.Errorf("medium effort should set MaxLoops=64, got %d", eng.maxLoops)
	}
	em.Apply("high")
	if eng.maxLoops != 512 {
		t.Errorf("high effort should set MaxLoops=512, got %d", eng.maxLoops)
	}
	em.Apply("max")
	if eng.maxLoops != 1024 {
		t.Errorf("max effort should set MaxLoops=1024, got %d", eng.maxLoops)
	}
}

// --- mocks ---

type mockEngine struct {
	maxLoops int
	prompt   string
}

func (m *mockEngine) SetMaxLoops(n int)        { m.maxLoops = n }
func (m *mockEngine) SetSystemPrompt(p string) { m.prompt = p }

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
