package main

import (
	"flag"
	"testing"

	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/seelex/application"
)

func TestParseFrontendMode(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "tui", want: "tui"},
		{input: " GUI ", want: "gui"},
		{input: "BACKEND", want: "backend"},
	} {
		got, err := parseFrontendMode(test.input)
		if err != nil {
			t.Fatalf("parseFrontendMode(%q): %v", test.input, err)
		}
		if got != test.want {
			t.Fatalf("parseFrontendMode(%q) = %q, want %q", test.input, got, test.want)
		}
	}
	if _, err := parseFrontendMode("browser"); err == nil {
		t.Fatal("parseFrontendMode accepted an unsupported frontend")
	}
}

func TestParsePermissionMode(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		input string
		want  toolspermission.Mode
	}{
		{input: "manual", want: toolspermission.ModeManual},
		{input: " FULL_ACCESS ", want: toolspermission.ModeFullAccess},
	} {
		got, err := parsePermissionMode(test.input)
		if err != nil {
			t.Fatalf("parsePermissionMode(%q): %v", test.input, err)
		}
		if got != test.want {
			t.Fatalf("parsePermissionMode(%q) = %q, want %q", test.input, got, test.want)
		}
	}
	if _, err := parsePermissionMode("unsafe"); err == nil {
		t.Fatal("parsePermissionMode accepted an unsupported permission mode")
	}
}

func TestReleaseVersionIsNotStale(t *testing.T) {
	t.Parallel()
	if Version != "dev" {
		t.Fatalf("source version = %q, want dev; releases must inject their tag with ldflags", Version)
	}
}

func TestSafeFlagDefaults(t *testing.T) {
	t.Parallel()
	if got := flag.Lookup("permission").DefValue; got != "manual" {
		t.Fatalf("permission default = %q, want manual", got)
	}
	if got := flag.Lookup("frontend").DefValue; got != "tui" {
		t.Fatalf("frontend default = %q, want tui", got)
	}
}

func TestDefaultManualRulesAllowSafeControlPlaneTools(t *testing.T) {
	checker := toolspermission.NewPermissionChecker(toolspermission.PermissionConfig{
		Mode: toolspermission.ModeManual, Rules: defaultManualRules(),
	})
	for _, name := range []string{
		"todolist_init", "todolist_add", "todolist_done", "todolist_status",
		"task_complete", "task_failed", "task_needs_user_decision",
	} {
		if result := checker.Check(name, `{}`); result != toolspermission.ResultAllow {
			t.Fatalf("safe control-plane tool %q permission = %v, want allow", name, result)
		}
	}
	if result := checker.Check("bash", `{"command":"pwd"}`); result != toolspermission.ResultAsk {
		t.Fatalf("bash permission = %v, want ask in manual mode", result)
	}
}

type permissionRuntimeRecorder struct {
	config     toolspermission.PermissionConfig
	handler    toolspermission.ApprovalHandler
	fullAccess []bool
}

func (runtime *permissionRuntimeRecorder) SetPermissionConfig(
	config toolspermission.PermissionConfig,
	handler toolspermission.ApprovalHandler,
) {
	runtime.config = config
	runtime.handler = handler
}

func (runtime *permissionRuntimeRecorder) SetFullAccess(on bool) {
	runtime.fullAccess = append(runtime.fullAccess, on)
}

func TestFullAccessStartupKeepsManualPermissionBaseline(t *testing.T) {
	originalMode := *permissionMode
	*permissionMode = string(toolspermission.ModeFullAccess)
	t.Cleanup(func() { *permissionMode = originalMode })

	runtime := &permissionRuntimeRecorder{}
	if err := setupPermissionGate(runtime, application.NewApprovalBroker(nil)); err != nil {
		t.Fatal(err)
	}
	if runtime.config.Mode != toolspermission.ModeManual {
		t.Fatalf("baseline permission mode = %q, want manual", runtime.config.Mode)
	}
	if runtime.handler == nil {
		t.Fatal("full-access startup discarded the manual approval bridge")
	}
	if len(runtime.fullAccess) != 1 || !runtime.fullAccess[0] {
		t.Fatalf("full-access startup toggles = %#v, want [true]", runtime.fullAccess)
	}
}






