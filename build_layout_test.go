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
		// P6: Linux GUI delivery tree (cgo build; cannot be cross-built on Windows).
		`LinuxGuiDir`, `LinuxGuiExe`, `linux-amd64-gui`,
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
		`dist/linux-amd64-gui`,
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

// TestBrandLogoShippedPerDeliveryFolder 钉住"品牌图每个交付文件夹各存一份"。
//
// 品牌源图 gui/icons/seelex-logo.png 入库（唯一事实源），交付树的副本由打包步骤
// 复制产生。没有这条护栏时，最容易的漂移是：有人把 cp 删掉、或又去引用本机下载
// 目录里的原始文件（C:\Users\<user>\Downloads\Seelex_logo.png），交付文件夹就
// 不再自足——从交付物出发的构建/打包只能靠"那台机器上恰好有那个文件"。
func TestBrandLogoShippedPerDeliveryFolder(t *testing.T) {
	t.Parallel()

	if _, err := os.Stat("gui/icons/seelex-logo.png"); err != nil {
		t.Fatalf("品牌源图 gui/icons/seelex-logo.png 必须入库（唯一事实源）: %v", err)
	}

	makefile := readRepoFile(t, "Makefile")
	for _, token := range []string{
		`BRAND_LOGO := gui/icons/seelex-logo.png`,
		`cp "$(BRAND_LOGO)" "$$outdir/seelex-logo.png"`, // P1 平台树
	} {
		if !strings.Contains(makefile, token) {
			t.Errorf("Makefile missing brand-logo packaging token %q", token)
		}
	}

	linuxGui := readRepoFile(t, "scripts/build-linux-gui.sh")
	if !strings.Contains(linuxGui, `gui/icons/seelex-logo.png`) {
		t.Errorf("scripts/build-linux-gui.sh 必须以 gui/icons/seelex-logo.png 为品牌图来源")
	}
	if !strings.Contains(linuxGui, `cp "$BRAND_LOGO" "$OUTDIR/seelex-logo.png"`) {
		t.Errorf("scripts/build-linux-gui.sh 必须把品牌图复制进 P6 交付树（dist/linux-amd64-gui/seelex-logo.png）")
	}

	// 交付树里不得出现对下载目录的依赖（唯一事实源是入库的品牌图）。
	// 只钉"路径形态"：裸的"下载"是常用动词（"省掉容器里的 60MB 下载"），会误伤注释。
	for _, rel := range []string{"Makefile", "scripts/build-linux-gui.sh"} {
		content := readRepoFile(t, rel)
		for _, banned := range []string{`Downloads`, `下载/`, `下载\`, `\下载`} {
			if strings.Contains(content, banned) {
				t.Errorf("%s references a local download location %q; ship the tracked brand image instead", rel, banned)
			}
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
