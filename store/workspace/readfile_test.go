package workspace

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTreeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadFileReturnsBytesWithMetadata(t *testing.T) {
	root := t.TempDir()
	writeTreeFile(t, root, "src/main.go", "package main\nfunc main() {}\n")

	repo := NewRepo()
	got, err := repo.ReadFile(root, "src/main.go", 0)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(got.Base64)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	if string(decoded) != "package main\nfunc main() {}\n" {
		t.Fatalf("unexpected content: %q", decoded)
	}
	if got.Name != "main.go" || got.Path != "src/main.go" || got.Size != int64(len(decoded)) {
		t.Fatalf("unexpected metadata: %+v", got)
	}
	if got.Truncated || !got.TextLike {
		t.Fatalf("expected text-like untruncated read: %+v", got)
	}
}

func TestReadFileTruncatesAtLimit(t *testing.T) {
	root := t.TempDir()
	writeTreeFile(t, root, "notes.md", "0123456789")

	got, err := NewRepo().ReadFile(root, "notes.md", 4)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _ := base64.StdEncoding.DecodeString(got.Base64)
	if string(decoded) != "0123" {
		t.Fatalf("expected truncated prefix, got %q", decoded)
	}
	if !got.Truncated || got.Size != 10 || got.Limit != 4 {
		t.Fatalf("unexpected truncation metadata: %+v", got)
	}
}

func TestReadFileClampsLimitBounds(t *testing.T) {
	root := t.TempDir()
	writeTreeFile(t, root, "a.txt", "x")

	// limit ≤ 0 → 默认上限。
	got, err := NewRepo().ReadFile(root, "a.txt", -1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Limit != filePreviewDefaultLimit {
		t.Fatalf("expected default limit, got %d", got.Limit)
	}
	// 超硬上限 → 钳制。
	got, err = NewRepo().ReadFile(root, "a.txt", filePreviewHardLimit+1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Limit != filePreviewHardLimit {
		t.Fatalf("expected hard limit clamp, got %d", got.Limit)
	}
}

func TestReadFileRejectsEscapesAndAbsolutePaths(t *testing.T) {
	root := t.TempDir()
	repo := NewRepo()
	for _, rel := range []string{"", ".", "../outside.txt", "sub/../../outside.txt", filepath.Join(root, "a.txt")} {
		if _, err := repo.ReadFile(root, rel, 0); err == nil {
			t.Fatalf("ReadFile(%q) succeeded, want error", rel)
		}
	}
}

func TestReadFileRejectsDirectoryAndSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	repo := NewRepo()
	if _, err := repo.ReadFile(root, "docs", 0); err == nil {
		t.Fatal("ReadFile succeeded on a directory")
	}

	// 符号链接直接拒绝（无论指向根内还是根外，防跟随逃逸）。
	outside := t.TempDir()
	writeTreeFile(t, outside, "secret.txt", "secret")
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if _, err := repo.ReadFile(root, "link.txt", 0); err == nil {
		t.Fatal("ReadFile succeeded through a symlink")
	}
}

func TestReadFileRejectsSensitiveAndIgnoredPaths(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{
		"config/accounts.yaml",
		"config/accounts.local.yaml",
		"node_modules/pkg/index.js",
		".git/config",
		"dist/bundle.js",
		".venv/bin/python",
	} {
		writeTreeFile(t, root, rel, "x")
	}
	repo := NewRepo()
	for _, rel := range []string{
		"config/accounts.yaml",
		"config/accounts.local.yaml",
		"node_modules/pkg/index.js",
		".git/config",
		"dist/bundle.js",
		".venv/bin/python",
	} {
		if _, err := repo.ReadFile(root, rel, 0); err == nil {
			t.Fatalf("ReadFile(%q) succeeded, want rejection", rel)
		}
	}
}

func TestReadFileDetectsBinary(t *testing.T) {
	root := t.TempDir()
	// 含 NUL → 二进制。
	writeTreeFile(t, root, "a.bin", "AB\x00CD")
	got, err := NewRepo().ReadFile(root, "a.bin", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.TextLike {
		t.Fatal("expected binary detection for NUL bytes")
	}
	// 正常 UTF-8 中文 → 文本。
	writeTreeFile(t, root, "b.md", "# 标题\n中文正文\n")
	got, err = NewRepo().ReadFile(root, "b.md", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !got.TextLike {
		t.Fatal("expected text-like detection for UTF-8 Chinese")
	}
	// 控制字节占比高 → 二进制。
	writeTreeFile(t, root, "c.bin", strings.Repeat("\x01\x02", 200))
	got, err = NewRepo().ReadFile(root, "c.bin", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.TextLike {
		t.Fatal("expected binary detection for control bytes")
	}
}

func TestReadFileMissingRoot(t *testing.T) {
	if _, err := NewRepo().ReadFile(filepath.Join(t.TempDir(), "missing"), "a.txt", 0); err == nil {
		t.Fatal("ReadFile succeeded on missing root")
	}
}
