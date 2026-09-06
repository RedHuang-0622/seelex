package main

// Build layout regression guards.
//
// Single source of truth for artifact directories is scripts/build-layout.ps1
// (PowerShell) plus the mirror table in .claude/build-convention.md. These
// tests make the "each partition lands in a fixed folder" rule executable:
//   - canonical partitions must exist in the single-source file and the doc;
//   - legacy artifact locations (the "today one folder, tomorrow another"
//     churn) must never reappear in build scripts, the Makefile, CI or docs.

import (
	"os"
	"strings"
	"testing"
)

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

func TestBuildLayoutSingleSource(t *testing.T) {
	t.Parallel()
	layout := readRepoFile(t, "scripts/build-layout.ps1")

	// PowerShell single-source: canonical partition properties and paths.
	requiredLayout := []string{
		`DistRoot`, `ArchiveRoot`, `DevBaselineDir`, `DevQuickDir`, `DevQuickCliExe`,
		`StageDir`, `StageExe`, `StageVersionFile`, `SmokeDir`, `StashDir`,
		`StashPrevious`, `DeployLog`, `AllowedDistEntries`,
		`tmp\build`, `stage-gui`, `seelex-gui.previous.exe`, `deploy.log`,
		`seelex-gui-dev`, `archive`, `dev\seelex.exe`, `AllowedDistEntries = @(`,
	}
	for _, token := range requiredLayout {
		if !strings.Contains(layout, token) {
			t.Errorf("scripts/build-layout.ps1 missing canonical token %q", token)
		}
	}

	// Spec doc must mirror the same partition table.
	doc := readRepoFile(t, ".claude/build-convention.md")
	requiredDoc := []string{
		`dist/archive`, `dist/seelex-gui-dev`, `dist/dev`, `dist/stage-gui`,
		`tmp/build/smoke`, `tmp/build/stash`, `tmp/build/deploy.log`,
	}
	for _, token := range requiredDoc {
		if !strings.Contains(doc, token) {
			t.Errorf(".claude/build-convention.md missing canonical partition %q", token)
		}
	}

	// scripts/README must also document the canonical table.
	readme := readRepoFile(t, "scripts/README.md")
	for _, token := range requiredDoc {
		if !strings.Contains(readme, token) {
			t.Errorf("scripts/README.md missing canonical partition %q", token)
		}
	}
}

func TestNoLegacyBuildDirsInSources(t *testing.T) {
	t.Parallel()
	forbidden := []string{
		`staging-gui`,
		`tmp/build/stage-gui`, `tmp\build\stage-gui`,
		`tmp\smoke`, `tmp/smoke`,
		`tmp\stash`, `tmp/stash`,
		`tmp\deploy.log`, `tmp/deploy.log`,
		`seelex-dev`,
		`GUI_PACKAGE_ROOT`,
	}
	// Executable build sources must never reference the legacy locations.
	// Documentation (scripts/README.md, .claude/build-convention.md) may name
	// them to explain what is banned, so docs are asserted on canonical tokens
	// in TestBuildLayoutSingleSource instead.
	scanned := []string{
		"scripts/build-layout.ps1",
		"scripts/seelex-flow.ps1",
		"scripts/build.ps1",
		"scripts/build.sh",
		"scripts/build-dev.sh",
		"scripts/build-gui.ps1",
		"Makefile",
		".github/workflows/release.yml",
		".githooks/post-commit",
	}
	for _, rel := range scanned {
		content := readRepoFile(t, rel)
		for _, token := range forbidden {
			if strings.Contains(content, token) {
				t.Errorf("%s references legacy build location %q; move it to the canonical partition (see scripts/build-layout.ps1)", rel, token)
			}
		}
	}
}
