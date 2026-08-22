package core

import (
	"context"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/core/input_router"
	"testing"
)

var _ = types.Message{} // used via fakeEngine implementing ChatEngine

// ── CommandRegistry ───────────────────────────────────────────

func TestCommandRegistry_RegisterAndGet(t *testing.T) {
	r := NewCommandRegistry()
	cmd := input_router.NewCommandFunc("test", "test command", func(_ context.Context, _ []string) (CommandResult, error) {
		return CommandResult{Notice: "executed"}, nil
	})
	if err := r.Register(cmd); err != nil {
		t.Fatal(err)
	}
	got, ok := r.Get("test")
	if !ok {
		t.Fatal("command not found")
	}
	if got.Name() != "test" {
		t.Errorf("expected 'test', got %q", got.Name())
	}
}

func TestCommandRegistry_RegisterDuplicate(t *testing.T) {
	r := NewCommandRegistry()
	cmd := input_router.NewCommandFunc("dup", "", func(_ context.Context, _ []string) (CommandResult, error) {
		return CommandResult{}, nil
	})
	_ = r.Register(cmd)
	err := r.Register(cmd)
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}
}

func TestCommandRegistry_RegisterEmptyName(t *testing.T) {
	r := NewCommandRegistry()
	cmd := input_router.NewCommandFunc("  ", "", func(_ context.Context, _ []string) (CommandResult, error) {
		return CommandResult{}, nil
	})
	err := r.Register(cmd)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestCommandRegistry_GetNotFound(t *testing.T) {
	r := NewCommandRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Fatal("should not find nonexistent command")
	}
}

func TestCommandRegistry_All(t *testing.T) {
	r := NewCommandRegistry()
	_ = r.Register(input_router.NewCommandFunc("b", "", func(_ context.Context, _ []string) (CommandResult, error) {
		return CommandResult{}, nil
	}))
	_ = r.Register(input_router.NewCommandFunc("a", "", func(_ context.Context, _ []string) (CommandResult, error) {
		return CommandResult{}, nil
	}))
	_ = r.Register(input_router.NewCommandFunc("c", "", func(_ context.Context, _ []string) (CommandResult, error) {
		return CommandResult{}, nil
	}))
	all := r.All()
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}
	// should be sorted: a, b, c
	if all[0].Name() != "a" || all[1].Name() != "b" || all[2].Name() != "c" {
		t.Errorf("expected sorted order, got: %q, %q, %q", all[0].Name(), all[1].Name(), all[2].Name())
	}
}

func TestCommandRegistry_AllEmpty(t *testing.T) {
	r := NewCommandRegistry()
	all := r.All()
	if len(all) != 0 {
		t.Fatalf("expected 0, got %d", len(all))
	}
}

// ── Builtin Commands (via Submit) ─────────────────────────────

func lastNotice(t *testing.T, svc *Service) string {
	t.Helper()
	snap := svc.Snapshot()
	if len(snap.Conversation) == 0 {
		return ""
	}
	last := snap.Conversation[len(snap.Conversation)-1]
	if last.Role == "system" {
		return last.Content
	}
	return ""
}
