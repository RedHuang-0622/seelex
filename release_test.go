package main

import (
	"flag"
	"os"
	"strings"
	"testing"

	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
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
	if Version == "" || Version == "v0.0.2" {
		t.Fatalf("unexpected release version %q", Version)
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
}
