package command

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

type fakeRegistry struct {
	commands map[string]contract.Command
}

func newFakeRegistry() *fakeRegistry {
	return &fakeRegistry{commands: map[string]contract.Command{}}
}

func (registry *fakeRegistry) Register(command contract.Command) error {
	if _, exists := registry.commands[command.Name()]; exists {
		return &duplicateCommandError{name: command.Name()}
	}
	registry.commands[command.Name()] = command
	return nil
}

func (registry *fakeRegistry) All() []contract.Command {
	all := make([]contract.Command, 0, len(registry.commands))
	for _, command := range registry.commands {
		all = append(all, command)
	}
	return all
}

type duplicateCommandError struct{ name string }

func (err *duplicateCommandError) Error() string { return "duplicate " + err.name }

func TestRegisterBuiltinsAndExecuteReadOnlyCommands(t *testing.T) {
	registry := newFakeRegistry()
	cleared := false
	application := New(registry, Deps{
		Model: func() (string, string) { return "deepseek", "example" },
		History: func() []contract.EngineMessage {
			return []contract.EngineMessage{{Role: "user"}, {Role: "assistant"}, {Role: "user"}}
		},
		Trace: func() string { return "trace tree" },
		Sessions: func() []model.SessionInfo {
			return []model.SessionInfo{{ID: "s1", Name: "one", UpdatedAt: time.Now(), TokenCount: 7}}
		},
		Diag:         func() string { return "diag text" },
		Clear:        func() { cleared = true },
		SigilCommand: "/", SigilPlugin: "+", SigilSkill: "@",
	})
	if err := application.Register(); err != nil {
		t.Fatalf("register: %v", err)
	}
	for _, name := range []string{"help", "clear", "model", "history", "trace", "sessions", "diag", "exit"} {
		if _, ok := registry.commands[name]; !ok {
			t.Fatalf("missing command %q", name)
		}
	}

	history, err := registry.commands["history"].Execute(context.Background(), nil)
	if err != nil || history.Notice != "共 3 条 (assistant: 1, user: 2)" {
		t.Fatalf("history = %+v, err=%v", history, err)
	}
	if _, err := registry.commands["clear"].Execute(context.Background(), nil); err != nil || !cleared {
		t.Fatalf("clear not called: cleared=%v err=%v", cleared, err)
	}
	exit, err := registry.commands["exit"].Execute(context.Background(), nil)
	if err != nil || !exit.Exit {
		t.Fatalf("exit = %+v, err=%v", exit, err)
	}
	help, err := registry.commands["help"].Execute(context.Background(), nil)
	if err != nil || !strings.Contains(help.Notice, "/help") || !strings.Contains(help.Notice, "+") {
		t.Fatalf("help = %q, err=%v", help.Notice, err)
	}
}

func TestRegisterStopsOnDuplicate(t *testing.T) {
	registry := newFakeRegistry()
	application := New(registry, Deps{})
	if err := application.Register(); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := application.Register(); err == nil || !strings.Contains(err.Error(), "help") {
		t.Fatalf("duplicate register error = %v", err)
	}
}

func TestMutationCommandsKeepSessionAndPolicyBoundaries(t *testing.T) {
	registry := newFakeRegistry()
	began, switchedPlugin, switchedEffort := 0, 0, 0
	application := New(registry, Deps{
		Mutation: MutationDeps{
			Compact:         func(context.Context) (string, error) { return "compacted", nil },
			BeginNewSession: func() error { began++; return nil },
			Capabilities:    func() (bool, string) { return true, "" },
			SessionInteraction: func() *protocol.Interaction {
				return &protocol.Interaction{ID: "resume"}
			},
			Resume:         func(string) error { return nil },
			CurrentSession: func() (string, bool) { return "parent", false },
			Fork:           func(string) (string, error) { return "child", nil },
			AccountInteraction: func() *protocol.Interaction {
				return &protocol.Interaction{ID: "pool"}
			},
			Plugins: func() ([]model.PluginInfo, model.PluginInfo, bool) {
				return []model.PluginInfo{{Name: "one", Description: "plugin one"}}, model.PluginInfo{Name: "one"}, true
			},
			SwitchPlugin: func(_ context.Context, name string) error {
				if name != "one" {
					t.Fatalf("plugin name = %q", name)
				}
				switchedPlugin++
				return nil
			},
			Effort: func() string { return "high" },
			SwitchEffort: func(_ context.Context, level string) error {
				if level != "max" {
					t.Fatalf("effort = %q", level)
				}
				switchedEffort++
				return nil
			},
			Permission:      func() string { return "auto" },
			PermissionTiers: func() []dto.PermissionTierInfo { return dto.PermissionTiers() },
			SetPermission:   func(string) (string, error) { return "manual", nil },
		},
	})
	if err := application.RegisterMutations(); err != nil {
		t.Fatalf("register mutations: %v", err)
	}
	if _, err := registry.commands["new"].Execute(context.Background(), nil); err != nil || began != 1 {
		t.Fatalf("new: began=%d err=%v", began, err)
	}
	if result, err := registry.commands["resume"].Execute(context.Background(), nil); err != nil || result.Interaction == nil || result.Interaction.ID != "resume" {
		t.Fatalf("resume interaction = %+v err=%v", result, err)
	}
	if result, err := registry.commands["fork"].Execute(context.Background(), nil); err != nil || !strings.Contains(result.Notice, "child") {
		t.Fatalf("fork = %+v err=%v", result, err)
	}
	if result, err := registry.commands["plugin"].Execute(context.Background(), []string{"one"}); err != nil || switchedPlugin != 1 || !strings.Contains(result.Notice, "one") {
		t.Fatalf("plugin = %+v err=%v calls=%d", result, switchedEffort, switchedPlugin)
	}
	if result, err := registry.commands["effort"].Execute(context.Background(), []string{"MAX"}); err != nil || switchedEffort != 1 || !strings.Contains(result.Notice, "max") {
		t.Fatalf("effort = %+v err=%v calls=%d", result, err, switchedEffort)
	}
	if result, err := registry.commands["permission"].Execute(context.Background(), []string{"manual"}); err != nil || !strings.Contains(result.Notice, "manual") {
		t.Fatalf("permission = %+v err=%v", result, err)
	}
	if result, err := registry.commands["compact"].Execute(context.Background(), nil); err != nil || result.Notice != "compacted" {
		t.Fatalf("compact = %+v err=%v", result, err)
	}
}
