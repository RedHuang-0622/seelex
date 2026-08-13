package tools

import "testing"

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
