package promptassets

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSetDirOverridesAndFallsBack 锁定外部覆盖层的三条口径：
//  1. 目录里同名文件命中 → 用磁盘那份（用户改过的优先）；
//  2. 目录里缺的那份 → 逐文件回退内嵌默认（只覆盖一份也能用）；
//  3. 空文件视为"未配置" → 回退内嵌，而不是把该段抹成空。
func TestSetDirOverridesAndFallsBack(t *testing.T) {
	embeddedIdentity := SystemIdentity()
	embeddedInstructions := SystemInstructions()
	if embeddedIdentity == "" || embeddedInstructions == "" {
		t.Fatal("embedded defaults must not be empty")
	}
	t.Cleanup(func() { SetDir("") })

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "system"), 0o755); err != nil {
		t.Fatal(err)
	}
	identityPath := filepath.Join(dir, "system", "identity.md")
	if err := os.WriteFile(identityPath, []byte("OVERRIDDEN IDENTITY"), 0o644); err != nil {
		t.Fatal(err)
	}

	SetDir(dir)
	if got := Dir(); got != dir {
		t.Fatalf("Dir() = %q, want %q", got, dir)
	}
	if got := SystemIdentity(); got != "OVERRIDDEN IDENTITY" {
		t.Fatalf("identity = %q, want the disk copy", got)
	}
	// instructions.md 不在目录里：逐文件回退内嵌，而不是整段消失。
	if got := SystemInstructions(); got != embeddedInstructions {
		t.Fatalf("instructions must fall back per file, got %d bytes", len(got))
	}

	// 空文件 = 未配置：回退内嵌，避免一次误清空把 system 段抹掉。
	if err := os.WriteFile(identityPath, []byte("   \n\t"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := SystemIdentity(); got != embeddedIdentity {
		t.Fatalf("blank override must fall back to the embedded copy, got %q", got)
	}

	// 空串 = 全内嵌。
	SetDir("")
	if got := SystemIdentity(); got != embeddedIdentity {
		t.Fatalf("SetDir(\"\") must restore embedded defaults")
	}
}

// TestDefaultFilesCoverEveryRenderedAsset 钉住"落盘清单 = 渲染清单"：
// 启动期按 DefaultFiles 逐份落盘，少列一份就意味着那一份永远无法被外部覆盖。
func TestDefaultFilesCoverEveryRenderedAsset(t *testing.T) {
	found := map[string]bool{}
	for _, rel := range DefaultFiles() {
		found[rel] = true
	}
	for _, want := range []string{
		"system/identity.md",
		"system/instructions.md",
		"effort/system.md",
		"plan/preflight.md",
		"plan/replan.md",
		"subagent/charter.md",
	} {
		if !found[want] {
			t.Fatalf("DefaultFiles() missing %q (got %v)", want, DefaultFiles())
		}
	}
	if !found[Entry] {
		t.Fatalf("Entry %q must be part of DefaultFiles()", Entry)
	}
}
