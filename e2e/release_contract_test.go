package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ── 发布/构建契约（从根包 release_test.go 迁入 e2e，不依赖 main 符号）────

func TestMakefileUsesGuardedCleanBuildSequences(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot(), "Makefile"))
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
	root := repoRoot()
	scriptData, err := os.ReadFile(filepath.Join(root, "scripts", "build-gui.ps1"))
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

	workflowData, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
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
