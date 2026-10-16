package security

import (
	"path/filepath"
	"testing"
)

// TestProjectScopeKeepsPerSessionRoots 钉住"项目根按会话分格"：视图会话的默认根
// 与后台会话的根互不覆盖，工具按会话键解析自己的项目（工作区污染回归）。
// 未绑定会话键回退默认根（旧会话语义），解绑只影响目标键。
func TestProjectScopeKeepsPerSessionRoots(t *testing.T) {
	viewRoot := t.TempDir()
	sessionRoot := t.TempDir()
	scope := NewProjectScope()

	if err := scope.Bind(viewRoot); err != nil {
		t.Fatalf("bind view root: %v", err)
	}
	if err := scope.BindFor("session-a", sessionRoot); err != nil {
		t.Fatalf("bind session root: %v", err)
	}

	// 会话 A 的解析落在自己的根内，与视图根无关。
	resolved, err := scope.ResolveWriteFor("session-a", filepath.Join("out", "note.txt"))
	if err != nil {
		t.Fatalf("resolve session write: %v", err)
	}
	if want := filepath.Join(sessionRoot, "out", "note.txt"); resolved != want {
		t.Fatalf("会话 A 的写路径 = %q, want %q", resolved, want)
	}
	viewResolved, err := scope.ResolveWriteFor(DefaultScopeKey, filepath.Join("out", "note.txt"))
	if err != nil {
		t.Fatalf("resolve view write: %v", err)
	}
	if want := filepath.Join(viewRoot, "out", "note.txt"); viewResolved != want {
		t.Fatalf("视图默认根的写路径 = %q, want %q", viewResolved, want)
	}

	// 会话 A 不能越界到视图根（两个根互为兄弟目录时最容易串）。
	if _, err := scope.ResolveReadFor("session-a", filepath.Join("..", filepath.Base(viewRoot), "leak.txt")); err == nil {
		t.Fatal("会话 A 不得解析到视图会话的项目根")
	}
	// 未绑定会话键回退默认根（旧会话语义，不 fail closed 破坏既有流程）。
	if root := scope.RootFor("session-unknown"); root != filepath.Clean(viewRoot) {
		t.Fatalf("未绑定会话键的根 = %q, want 回退默认根 %q", root, filepath.Clean(viewRoot))
	}

	// 解绑只影响目标键：会话 A 落回默认根，默认根自身不变。
	scope.UnbindFor("session-a")
	if root := scope.RootFor("session-a"); root != filepath.Clean(viewRoot) {
		t.Fatalf("解绑后会话 A 的根 = %q, want 回退默认根 %q", root, filepath.Clean(viewRoot))
	}
	if root := scope.Root(); root != filepath.Clean(viewRoot) {
		t.Fatalf("解绑会话根不应影响默认根：%q", root)
	}
}
