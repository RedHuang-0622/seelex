package scheduler

import (
	"context"
	"errors"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	switchworkspace "github.com/RedHuang-0622/seelex/middle/switch/workspace"
)

func TestSchedulerValidatesBeforeRuntime(t *testing.T) {
	called := false
	application := NewScheduler(SchedulerDeps{
		ValidateWorkspace: func(string) error { return errors.New("workspace") },
		ValidatePlugins:   func([]string) error { t.Fatal("plugins validated after workspace error"); return nil },
		ScheduleTask: func(context.Context, dto.ScheduledTaskSpec) (*dto.ScheduledTaskStatus, error) {
			called = true
			return nil, nil
		},
	})
	if _, err := application.ScheduleTask(context.Background(), dto.ScheduledTaskSpec{}); err == nil {
		t.Fatal("ScheduleTask accepted invalid workspace")
	}
	if called {
		t.Fatal("Runtime scheduler called after validation failure")
	}
}

func TestRunDefaultsFillMissingParameters(t *testing.T) {
	spec := WithRunDefaults(dto.ScheduledTaskSpec{})
	if spec.ReasoningEffort == "" || spec.LoopLimit == 0 {
		t.Fatalf("defaults not applied: %+v", spec)
	}
	if err := ValidateRunParams(spec); err != nil {
		t.Fatalf("ValidateRunParams: %v", err)
	}
}

func TestRunAssemblyAppliesSessionContextBeforePlugins(t *testing.T) {
	var calls []string
	ctx, err := NewRun(RunDeps{
		EnsureSessionUnit: func(string) { calls = append(calls, "unit") },
		PersistTier: func(string, string) error {
			calls = append(calls, "persist")
			return nil
		},
		ApplyStoredTier: func(string, string) { calls = append(calls, "tier") },
		ApplyRunParams: func(string, dto.ScheduledTaskSpec) error {
			calls = append(calls, "params")
			return nil
		},
		WithPluginAssembly: func(ctx context.Context, plugins []string) (context.Context, error) {
			calls = append(calls, "plugins")
			return ctx, nil
		},
	}).AssembleScheduledRun(context.Background(), "session-a", dto.ScheduledTaskSpec{
		Plugins: []string{"plugin-a"},
	})
	if err != nil || len(calls) != 5 || calls[0] != "unit" || calls[4] != "plugins" {
		t.Fatalf("ctx=%v calls=%v err=%v", ctx, calls, err)
	}
}

func TestScheduledSessionPublishesBeforeSubmit(t *testing.T) {
	var calls []string
	session, err := NewSession(SessionDeps{
		LifecycleState: func() (bool, bool) { return false, false },
		ResolveWorkspace: func(string) (*model.WorkspaceInfo, error) {
			return nil, nil
		},
		NewSessionID: func(string) string { return "sched-1" },
		OpenSession: func(string, *model.WorkspaceInfo) (string, error) {
			return "session-1", nil
		},
		Projection: func() switchworkspace.Projection { return switchworkspace.Projection{} },
		LockView:   func() { calls = append(calls, "lock") },
		ApplyWorkspaceProjectionLocked: func(switchworkspace.Projection) {
			calls = append(calls, "apply")
		},
		BumpLocked: func() uint64 { calls = append(calls, "bump"); return 2 },
		UnlockView: func() { calls = append(calls, "unlock") },
		PublishSnapshotChanged: func(uint64, string) {
			calls = append(calls, "publish")
		},
		TouchResident:         func(string) { calls = append(calls, "resident") },
		RequestCatalogRefresh: func() { calls = append(calls, "catalog") },
		AssembleRun: func(ctx context.Context, sessionID string, task dto.ScheduledTaskSpec) (context.Context, error) {
			calls = append(calls, "assemble")
			return ctx, nil
		},
		SubmitConversation: func(context.Context, string, string) error {
			calls = append(calls, "submit")
			return nil
		},
		DrainingError: errors.New("draining"),
	}).StartScheduledSession(context.Background(), dto.ScheduledTaskSpec{Prompt: "run"})
	if err != nil || session != "session-1" {
		t.Fatalf("session=%q err=%v", session, err)
	}
	want := []string{"lock", "apply", "bump", "unlock", "publish", "resident", "catalog", "assemble", "submit"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v", calls)
	}
	for index := range want {
		if calls[index] != want[index] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
}
