package seelebridge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectScopeResolvesOnlyInsideBoundRoot(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "nested", "marker.txt")
	if err := os.MkdirAll(filepath.Dir(inside), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inside, []byte("project marker"), 0o644); err != nil {
		t.Fatal(err)
	}
	scope := NewProjectScope()
	if _, err := scope.ResolveRead("marker.txt"); err == nil {
		t.Fatal("unbound scope must fail closed")
	}
	if err := scope.Bind(root); err != nil {
		t.Fatal(err)
	}
	resolved, err := scope.ResolveRead(filepath.Join("nested", "marker.txt"))
	if err != nil || resolved != inside {
		t.Fatalf("resolved=%q err=%v, want %q", resolved, err, inside)
	}
	if _, err := scope.ResolveRead(filepath.Join("..", "outside.txt")); err == nil {
		t.Fatal("parent traversal must be rejected")
	}
	if _, err := scope.ResolveWrite(filepath.Join("..", "outside.txt")); err == nil {
		t.Fatal("write traversal must be rejected")
	}
	sibling := root + "-sibling"
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := scope.ResolveRead(sibling); err == nil {
		t.Fatal("sibling prefix must not be treated as inside root")
	}
}

func TestProjectScopeRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	scope := NewProjectScope()
	if err := scope.Bind(root); err != nil {
		t.Fatal(err)
	}
	if _, err := scope.ResolveRead(filepath.Join("escape", "secret.txt")); err == nil {
		t.Fatal("symlink escape must be rejected")
	}
	if _, err := scope.ResolveWrite(filepath.Join("escape", "new.txt")); err == nil {
		t.Fatal("write through symlink escape must be rejected")
	}
}
