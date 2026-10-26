package core

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	view_state "github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

func TestPolicyApplicationIsAssembled(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("policyApplication")
	if !ok {
		t.Fatal("serviceComponents 未装配 policyApplication")
	}
	if field.Type != reflect.TypeOf((*policyApplication)(nil)) {
		t.Fatalf("policyApplication 字段类型 = %s", field.Type)
	}
}

func TestPolicyApplicationDelegatesPolicyOperations(t *testing.T) {
	var calls []string
	application := newPolicyApplication(policyApplicationDeps{
		ActivateSkill: func(name string) (model.SkillInfo, error) {
			calls = append(calls, "skill:"+name)
			return model.SkillInfo{Name: name}, nil
		},
		SetReasoningEffort: func(effort string) error {
			calls = append(calls, "reasoning:"+effort)
			return nil
		},
		SetLoopLimit: func(limit int) error {
			calls = append(calls, "loop")
			return nil
		},
		SetReleaseWindow: func(window int) (int, error) {
			calls = append(calls, "set-window")
			return 7, nil
		},
		ReleaseWindow: func() int {
			calls = append(calls, "get-window")
			return 7
		},
		SetPermissionTier: func(tier string) (string, error) {
			calls = append(calls, "tier:"+tier)
			return tier, nil
		},
	})

	skill, err := application.ActivateSkill("review")
	if err != nil || skill.Name != "review" {
		t.Fatalf("ActivateSkill = %+v, %v", skill, err)
	}
	if err := application.SetReasoningEffort("high"); err != nil {
		t.Fatal(err)
	}
	if err := application.SetLoopLimit(48); err != nil {
		t.Fatal(err)
	}
	if window, err := application.SetSessionReleaseWindow(7); err != nil || window != 7 {
		t.Fatalf("SetSessionReleaseWindow = %d, %v", window, err)
	}
	if window := application.SessionReleaseWindow(); window != 7 {
		t.Fatalf("SessionReleaseWindow = %d", window)
	}
	if !application.SetFullAccess(true) {
		t.Fatal("SetFullAccess(true) must report enabled")
	}
	if got := len(calls); got != 6 {
		t.Fatalf("calls = %v", calls)
	}
}

func TestPolicyApplicationFullAccessError(t *testing.T) {
	tierErr := errors.New("persist failed")
	application := newPolicyApplication(policyApplicationDeps{
		SetPermissionTier: func(tier string) (string, error) {
			if tier != dto.PermissionTierFull {
				t.Fatalf("tier = %q", tier)
			}
			return "", tierErr
		},
	})

	if application.SetFullAccess(true) {
		t.Fatal("SetFullAccess must report false on error")
	}
}

func TestPolicyApplicationSetPermissionTierFullPath(t *testing.T) {
	var calls []string
	application := newPolicyApplication(policyApplicationDeps{
		CurrentViewSessionID: func() string { return "session-a" },
		PersistTier: func(sessionID, tier string) error {
			calls = append(calls, "persist:"+tier)
			return nil
		},
		SetUnitTier: func(sessionID, tier string) {
			calls = append(calls, "unit:"+tier)
		},
		SetApprovalAuto: func(sessionID string, full bool) {
			calls = append(calls, "approval-full:"+boolWord(full))
		},
		SetRuntimeTier: func(sessionID, tier string) error {
			calls = append(calls, "runtime:"+tier)
			return nil
		},
		EffectiveTier: func(sessionID string) string {
			calls = append(calls, "effective")
			return dto.PermissionTierFull
		},
		ResolveApprovals: func(sessionID string) {
			calls = append(calls, "resolve")
		},
		PublishTier: func(sessionID, tier string) {
			calls = append(calls, "publish:"+tier)
		},
	})

	effective, err := application.SetPermissionTier("full")

	if err != nil || effective != dto.PermissionTierFull {
		t.Fatalf("SetPermissionTier = %q, %v", effective, err)
	}
	want := "persist:full,unit:full,approval-full:true,runtime:full,effective,resolve,publish:full"
	if strings.Join(calls, ",") != want {
		t.Fatalf("calls = %v, want %s", calls, want)
	}
}

func TestPolicyApplicationSetPermissionTierStopsOnPersistAndRuntimeErrors(t *testing.T) {
	persistErr := errors.New("persist")
	runtimeErr := errors.New("runtime")
	tests := []struct {
		name       string
		persistErr error
		runtimeErr error
		want       error
		unitCalled bool
	}{
		{name: "persist", persistErr: persistErr, want: persistErr},
		{name: "runtime", runtimeErr: runtimeErr, want: runtimeErr, unitCalled: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			unitCalled := false
			application := newPolicyApplication(policyApplicationDeps{
				CurrentViewSessionID: func() string { return "session-a" },
				PersistTier:          func(string, string) error { return test.persistErr },
				SetUnitTier:          func(string, string) { unitCalled = true },
				SetApprovalAuto:      func(string, bool) {},
				SetRuntimeTier:       func(string, string) error { return test.runtimeErr },
				EffectiveTier:        func(string) string { return dto.PermissionTierManual },
				ResolveApprovals:     func(string) {},
				PublishTier:          func(string, string) {},
			})

			_, err := application.SetPermissionTier("auto")

			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if unitCalled != test.unitCalled {
				t.Fatalf("unitCalled = %v, want %v", unitCalled, test.unitCalled)
			}
		})
	}
}

func TestPolicyApplicationSetPermissionTierDoesNotResolveNonFull(t *testing.T) {
	application := newPolicyApplication(policyApplicationDeps{
		CurrentViewSessionID: func() string { return "session-a" },
		PersistTier:          func(string, string) error { return nil },
		SetUnitTier:          func(string, string) {},
		SetApprovalAuto:      func(string, bool) {},
		SetRuntimeTier:       func(string, string) error { return nil },
		EffectiveTier:        func(string) string { return dto.PermissionTierAuto },
		ResolveApprovals: func(string) {
			t.Fatal("非 full 档不得自动放行审批")
		},
		PublishTier: func(string, string) {},
	})

	if _, err := application.SetPermissionTier("auto"); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyApplicationSwitchEffortRunsFullPath(t *testing.T) {
	var calls []string
	application := newPolicyApplication(policyApplicationDeps{
		EnsureViewIdle: func(label string) (string, error) {
			calls = append(calls, "idle")
			return "session-a", nil
		},
		CycleEffort: func() (string, error) {
			calls = append(calls, "cycle")
			return "high", nil
		},
		ApplyEffort: func(level string) error {
			calls = append(calls, "apply:"+level)
			return nil
		},
		SubagentPolicy: func() dto.SubagentPolicy {
			calls = append(calls, "policy")
			return dto.SubagentPolicy{Effort: "policy-high"}
		},
		SetSubagentPolicy: func(policy dto.SubagentPolicy) {
			calls = append(calls, "subagent:"+policy.Effort)
		},
		SyncReasoningEffort: func(level string) int {
			calls = append(calls, "reasoning:"+level)
			return 1
		},
		CurrentEffort: func() string {
			calls = append(calls, "current")
			return "high"
		},
		RenderPrompt: func() string {
			calls = append(calls, "prompt")
			return "system prompt"
		},
		SetSystemPrompt: func(sessionID, prompt string) {
			calls = append(calls, "set-prompt:"+sessionID)
		},
		CommitEffort: func(sessionID, effort, reasoning string, loop int) {
			calls = append(calls, "commit:"+effort)
		},
	})

	if err := application.SwitchEffort("cycle"); err != nil {
		t.Fatalf("SwitchEffort: %v", err)
	}

	want := "idle,cycle,apply:high,policy,subagent:policy-high,current,reasoning:high,prompt,set-prompt:session-a,commit:high"
	if strings.Join(calls, ",") != want {
		t.Fatalf("calls = %v, want %s", calls, want)
	}
}

func TestPolicyApplicationSwitchEffortStopsOnIdleCycleAndApplyErrors(t *testing.T) {
	idleErr := errors.New("idle")
	cycleErr := errors.New("cycle")
	applyErr := errors.New("apply")
	tests := []struct {
		name     string
		idleErr  error
		cycleErr error
		applyErr error
		want     error
	}{
		{name: "idle", idleErr: idleErr, want: idleErr},
		{name: "cycle", cycleErr: cycleErr, want: cycleErr},
		{name: "apply", applyErr: applyErr, want: applyErr},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			application := newPolicyApplication(policyApplicationDeps{
				EnsureViewIdle: func(string) (string, error) {
					if test.idleErr != nil {
						return "", test.idleErr
					}
					return "session-a", nil
				},
				CycleEffort: func() (string, error) {
					if test.cycleErr != nil {
						return "", test.cycleErr
					}
					return "high", nil
				},
				ApplyEffort:    func(string) error { return test.applyErr },
				SubagentPolicy: func() dto.SubagentPolicy { return dto.SubagentPolicy{} },
				SetSubagentPolicy: func(dto.SubagentPolicy) {
					if test.applyErr != nil {
						t.Fatal("apply 失败后不得下发 subagent policy")
					}
				},
				SyncReasoningEffort: func(string) int { return 0 },
				CurrentEffort:       func() string { return "high" },
				RenderPrompt:        func() string { return "" },
				SetSystemPrompt:     func(string, string) {},
				CommitEffort:        func(string, string, string, int) {},
			})

			err := application.SwitchEffort("cycle")

			if !errors.Is(err, test.want) {
				t.Fatalf("SwitchEffort error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestPolicyApplicationSwitchPluginActivatesAndRebuilds(t *testing.T) {
	var calls []string
	application := newPolicyApplication(policyApplicationDeps{
		AnyChatRunning: func() bool {
			calls = append(calls, "running")
			return false
		},
		ActivatePlugin: func(ctx context.Context, name string) error {
			calls = append(calls, "activate:"+name)
			return nil
		},
		DeactivatePlugin: func(context.Context) error {
			t.Fatal("非 off 不得 deactivate")
			return nil
		},
		ClearHistory: func() { calls = append(calls, "clear") },
		ResetPrompt:  func(text string) { calls = append(calls, "prompt:"+text) },
		CurrentPlugin: func() (model.PluginInfo, bool) {
			calls = append(calls, "current")
			return model.PluginInfo{Name: "review", Prompt: " plugin prompt "}, true
		},
		ReapplyEffort:         func() { calls = append(calls, "effort") },
		SetPluginSystemPrompt: func(text string) { calls = append(calls, "system:"+text) },
		RenderPrompt:          func() string { calls = append(calls, "render"); return "rendered" },
		ResetConversation:     func(message string) { calls = append(calls, "reset:"+message) },
		CollectProjection: func(context.Context) view_state.RuntimeStateProjection {
			calls = append(calls, "collect")
			return view_state.RuntimeStateProjection{}
		},
		CommitProjection: func(context.Context, view_state.RuntimeStateProjection) {
			calls = append(calls, "commit")
		},
	})

	if err := application.SwitchPlugin(context.Background(), "review"); err != nil {
		t.Fatalf("SwitchPlugin: %v", err)
	}
	want := "running,activate:review,clear,current,prompt:plugin prompt,effort,render,system:rendered,reset:已切换到 review 插件,collect,commit"
	if strings.Join(calls, ",") != want {
		t.Fatalf("calls = %v, want %s", calls, want)
	}
}

func TestPolicyApplicationSwitchPluginOffAndErrors(t *testing.T) {
	t.Run("off", func(t *testing.T) {
		var calls []string
		application := newPolicyApplication(policyApplicationDeps{
			AnyChatRunning: func() bool { return false },
			DeactivatePlugin: func(context.Context) error {
				calls = append(calls, "deactivate")
				return nil
			},
			ClearHistory:          func() { calls = append(calls, "clear") },
			ResetPrompt:           func(text string) { calls = append(calls, "prompt") },
			ReapplyEffort:         func() { calls = append(calls, "effort") },
			SetPluginSystemPrompt: func(string) { calls = append(calls, "system") },
			RenderPrompt:          func() string { return "rendered" },
			ResetConversation:     func(message string) { calls = append(calls, "reset:"+message) },
			CollectProjection:     func(context.Context) view_state.RuntimeStateProjection { return view_state.RuntimeStateProjection{} },
			CommitProjection:      func(context.Context, view_state.RuntimeStateProjection) { calls = append(calls, "commit") },
		})
		if err := application.SwitchPlugin(context.Background(), "off"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(calls, ","), "reset:已停用插件") {
			t.Fatalf("calls = %v", calls)
		}
	})

	t.Run("running", func(t *testing.T) {
		application := newPolicyApplication(policyApplicationDeps{
			AnyChatRunning: func() bool { return true },
			ActivatePlugin: func(context.Context, string) error {
				t.Fatal("running 不得 activate")
				return nil
			},
		})
		err := application.SwitchPlugin(context.Background(), "review")
		if err == nil || !strings.Contains(err.Error(), "有会话正在运行") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("activate-error", func(t *testing.T) {
		activateErr := errors.New("activate failed")
		application := newPolicyApplication(policyApplicationDeps{
			AnyChatRunning: func() bool { return false },
			ActivatePlugin: func(context.Context, string) error { return activateErr },
			ClearHistory:   func() { t.Fatal("activate 失败后不得清历史") },
		})
		err := application.SwitchPlugin(context.Background(), "review")
		if !errors.Is(err, activateErr) || !strings.Contains(err.Error(), "activate plugin") {
			t.Fatalf("error = %v", err)
		}
	})
}

func boolWord(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
