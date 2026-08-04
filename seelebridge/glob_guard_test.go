package seelebridge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── glob/grep 遍历护栏（2026-08-04 修复：**/* 全树遍历卡顿）────────────

// TestGlobSkipsHeavyDirs 验证 glob 遍历跳过重目录（node_modules/dist 等）
// 与隐藏目录——**/* 不再全树卡顿，结果不包含被跳过目录内的文件。
func TestGlobSkipsHeavyDirs(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	root := t.TempDir()
	for _, dir := range []string{"src", "node_modules/pkg", "dist", ".git", "docs"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"src/main.go":           "package main",
		"node_modules/pkg/x.js": "x",
		"dist/app.js":           "app",
		".git/config":           "config",
		"docs/README.md":        "docs",
	}
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := runtime.BindProjectRoot(root); err != nil {
		t.Fatal(err)
	}
	out, err := runtime.Agent().DirectDispatch(context.Background(), "glob", `{"pattern":"**/*"}`)
	if err != nil {
		t.Fatal(err)
	}
	var results []string
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("glob output must be JSON array: %s (%v)", out, err)
	}
	joined := strings.Join(results, "|")
	// display 路径使用平台分隔符（Windows 反斜杠），统一 ToSlash 比较。
	joinedSlash := filepath.ToSlash(joined)
	for _, want := range []string{"src/main.go", "docs/README.md"} {
		if !strings.Contains(joinedSlash, want) {
			t.Errorf("glob missing %q: %v", want, results)
		}
	}
	for _, forbidden := range []string{"node_modules", "dist/", ".git", "pkg/x.js", "app.js"} {
		if strings.Contains(joinedSlash, forbidden) {
			t.Errorf("glob must skip heavy dir, got %q in %v", forbidden, results)
		}
	}
}

// TestMatchGlobPatternRecursive 验证 glob 匹配器：** 递归、正斜杠语义、
// * 单段、? 单字符（Windows 路径反斜杠经 ToSlash 归一）。
func TestMatchGlobPatternRecursive(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"**/*", "src/main.go", true},
		{"**/*", "a/b/c/d.txt", true},
		{"**/*", "single.go", true},
		{"**/*.go", "src/main.go", true},
		{"**/*.go", "a/b/c/main.go", true},
		{"**/*.go", "src/main.js", false},
		{"src/*.go", "src/main.go", true},
		{"src/*.go", "src/sub/main.go", false},
		{"*.go", "main.go", true},
		{"*.go", "src/main.go", false},
		{"**", "anything/deep", true},
		{"docs/**/*.md", "docs/sub/deep/readme.md", true},
	}
	for _, tc := range cases {
		if got := matchGlobPattern(tc.pattern, tc.path); got != tc.want {
			t.Errorf("matchGlobPattern(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}
