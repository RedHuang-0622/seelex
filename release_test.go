package main

import (
	"flag"
	"os"
	"regexp"
	"strings"
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

func TestMakefileUsesGuardedCleanBuildSequences(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, required := range []string{
		"LOCAL_CONFIG ?= config/accounts.yaml",
		"release: rebuild",
		"rebuild: clean",
		"clean: guard-dist",
		"clean-gui: guard-dist guard-version",
		"guard-local-config:",
		"build-gui: dev-build-gui",
		"dev-build-gui: guard-version guard-local-config",
		"publish-build-gui: guard-version",
		"rebuild-gui: clean-gui",
		"publish-rebuild-gui: clean-gui",
		"-BuildKind Dev",
		"-BuildKind Publish",
		`-LocalConfigPath "$(LOCAL_CONFIG)"`,
		"refusing to clean unexpected DIST=",
		"refusing unsafe VERSION=",
		"local GUI account configuration is missing",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("Makefile missing guarded clean/build contract %q", required)
		}
	}
}

func TestGUIBuildKeepsLocalAndPublicConfigurationSeparate(t *testing.T) {
	t.Parallel()
	scriptData, err := os.ReadFile("scripts/build-gui.ps1")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptData)
	for _, required := range []string{
		`[string]$LocalConfigPath = ""`,
		`[string]$BuildKind = "Publish"`,
		`if ($BuildKind -eq "Dev")`,
		`publish GUI build must not receive a local account configuration`,
		`Test-Path -LiteralPath $configSource -PathType Leaf`,
		`Join-Path $PackageRoot "config/accounts.yaml"`,
		`if ($BuildKind -eq "Publish")`,
		`publish GUI package contains private or runtime-local files`,
		`README_EN.md`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("GUI build script missing local configuration contract %q", required)
		}
	}

	workflowData, err := os.ReadFile(".github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(workflowData)
	if strings.Contains(workflow, "LocalConfigPath") {
		t.Fatal("public release workflow must not package a local account configuration")
	}
	if !strings.Contains(workflow, `build-gui.ps1 -Version $env:GITHUB_REF_NAME -BuildKind Publish`) {
		t.Fatal("public release workflow must explicitly select the publish GUI build")
	}
	for _, required := range []string{
		`release tag must be SemVer`,
		`-path '*/config/accounts.yaml'`,
		`-path '*/.seelex/*'`,
		`-name '*.local.yaml'`,
		`contains(github.ref_name, '-')`,
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("release workflow missing safety contract %q", required)
		}
	}
}

func TestReleaseTagSemVerContract(t *testing.T) {
	pattern := regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
	for _, tag := range []string{"v0.0.2", "v1.2.3-alpha.1", "v1.2.3+build.7", "v1.2.3-rc.1+sha.abc"} {
		if !pattern.MatchString(tag) {
			t.Fatalf("valid release tag rejected: %s", tag)
		}
	}
	for _, tag := range []string{"v1", "v1.2", "v01.2.3", "1.2.3", "v1.2.3_release"} {
		if pattern.MatchString(tag) {
			t.Fatalf("invalid release tag accepted: %s", tag)
		}
	}
}
