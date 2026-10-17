package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoaderDirectorySkillWithResource(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "cad-review")
	mustMkdir(t, filepath.Join(skillDir, "scripts"))
	mustWrite(t, filepath.Join(skillDir, instructionFile), "---\ndescription: CAD review\n---\n\n# ignored heading\nDo the work.\n")
	mustWrite(t, filepath.Join(skillDir, "scripts", "check.sh"), "echo ok")

	skills, err := NewLoader(root).LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].Name != "cad-review" {
		t.Fatalf("unexpected skills: %#v", skills)
	}
	if skills[0].Description != "CAD review" || strings.Contains(skills[0].Prompt, "description:") {
		t.Fatalf("frontmatter not parsed: %#v", skills[0])
	}
	resource, err := skills[0].ResourcePath("scripts/check.sh")
	if err != nil || resource != filepath.Join(skillDir, "scripts", "check.sh") {
		t.Fatalf("resource=%q err=%v", resource, err)
	}
}

func TestLoaderDirectoryBeatsLegacy(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "plan"))
	mustWrite(t, filepath.Join(root, "plan", instructionFile), "# directory\n")
	mustWrite(t, filepath.Join(root, "plan.md"), "# legacy\n")

	skills, err := NewLoader(root).LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].LegacyFlatFile || skills[0].Description != "directory" {
		t.Fatalf("unexpected priority result: %#v", skills)
	}
}

func TestLoaderReadsAndDeletesLegacySkill(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "legacy.md"), "# Legacy skill\n")
	loader := NewLoader(root)
	loaded, err := loader.Load("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.LegacyFlatFile || loaded.Description != "Legacy skill" {
		t.Fatalf("unexpected legacy skill: %#v", loaded)
	}
	if err := loader.Delete("legacy"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "legacy.md")); !os.IsNotExist(err) {
		t.Fatalf("legacy file still exists: %v", err)
	}
}

func TestLoaderRootPriority(t *testing.T) {
	high, low := t.TempDir(), t.TempDir()
	mustMkdir(t, filepath.Join(high, "review"))
	mustMkdir(t, filepath.Join(low, "review"))
	mustWrite(t, filepath.Join(high, "review", instructionFile), "# high\n")
	mustWrite(t, filepath.Join(low, "review", instructionFile), "# low\n")

	skills, err := NewLoader(high, low).LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].Description != "high" {
		t.Fatalf("unexpected root priority: %#v", skills)
	}
}

func TestCreateDeleteCompletesAndUsesDirectoryFormat(t *testing.T) {
	loader := NewLoader(t.TempDir())
	done := make(chan error, 1)
	go func() { done <- loader.Create("new-skill", "desc", "prompt") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Create deadlocked")
	}
	if _, err := os.Stat(filepath.Join(loader.PrimaryDir(), "new-skill", instructionFile)); err != nil {
		t.Fatal(err)
	}
	if err := loader.Delete("new-skill"); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsUnsafeNamesAndResources(t *testing.T) {
	loader := NewLoader(t.TempDir())
	for _, name := range []string{"../escape", "Bad Name", "", "a/b"} {
		if err := loader.Create(name, "", ""); err == nil {
			t.Fatalf("Create(%q) should fail", name)
		}
	}
	s := Skill{Name: "safe", RootDir: loader.PrimaryDir()}
	if _, err := s.ResourcePath("../escape"); err == nil {
		t.Fatal("resource escape should fail")
	}
	if _, err := s.ResourcePath(filepath.Join(loader.PrimaryDir(), "absolute")); err == nil {
		t.Fatal("absolute resource should fail")
	}
	if _, err := (Skill{Name: "empty"}).ResourcePath("x"); err == nil {
		t.Fatal("empty resource root should fail")
	}
}

func TestLoaderErrors(t *testing.T) {
	loader := NewLoader(t.TempDir())
	if _, err := loader.Load("missing"); err == nil {
		t.Fatal("missing skill should fail")
	}
	if _, err := loader.Load("../bad"); err == nil {
		t.Fatal("invalid skill should fail")
	}
	if err := loader.Delete("missing"); err == nil {
		t.Fatal("missing delete should fail")
	}
	if err := loader.Create("duplicate", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := loader.Create("duplicate", "", ""); err == nil {
		t.Fatal("duplicate create should fail")
	}

	broken := filepath.Join(t.TempDir(), "broken")
	mustMkdir(t, broken)
	mustWrite(t, filepath.Join(broken, instructionFile), "---\ninvalid: [\n---\nbody\n")
	if _, err := NewLoader(filepath.Dir(broken)).LoadAll(); err == nil {
		t.Fatal("invalid frontmatter should fail")
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
