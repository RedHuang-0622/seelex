package workspace

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// decodeFileContent 解出预览读取的正文（base64 通道）。
func decodeFileContent(t *testing.T, content dto.FileContent) string {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString(content.Base64)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	return string(decoded)
}

// writefile_test.go — 「文件详情」编辑保存的服务端门禁回归：与 ReadFile 同一条
// 可见性边界（读不动的文件同样写不动），非文本一律拒绝，写入原子发布且保留原
// 权限位。这些断言是前端「保存后读回同步基线」这条不变量的地基：保存报成功，
// 磁盘上就必须真的是那一份内容。

func TestWriteFileWritesContentAndReportsMetadata(t *testing.T) {
	root := t.TempDir()
	writeTreeFile(t, root, "src/main.go", "package main\n")

	got, err := NewRepo().WriteFile(root, "src/main.go", []byte("package main\n\nfunc main() {}\n"))
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got.Path != "src/main.go" || got.Size != int64(len("package main\n\nfunc main() {}\n")) {
		t.Fatalf("unexpected result: %+v", got)
	}
	// 磁盘为准：读回来的就是刚写的那一份（保存成功 ⇔ 内容落地）。
	onDisk, err := os.ReadFile(filepath.Join(root, "src", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != "package main\n\nfunc main() {}\n" {
		t.Fatalf("disk content = %q", onDisk)
	}
	// 读面同步可见（不是写进别的路径）。
	content, err := NewRepo().ReadFile(root, "src/main.go", 0)
	if err != nil {
		t.Fatal(err)
	}
	if decoded := decodeFileContent(t, content); decoded != "package main\n\nfunc main() {}\n" {
		t.Fatalf("ReadFile after write = %q", decoded)
	}
}

func TestWriteFileOverwritesExistingFileAndEmptyContent(t *testing.T) {
	root := t.TempDir()
	repo := NewRepo()
	writeTreeFile(t, root, "docs/notes.md", "old\n")
	got, err := repo.WriteFile(root, "docs/notes.md", []byte("# 新正文\n"))
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got.Path != "docs/notes.md" {
		t.Fatalf("unexpected path: %q", got.Path)
	}
	onDisk, err := os.ReadFile(filepath.Join(root, "docs", "notes.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != "# 新正文\n" {
		t.Fatalf("disk content = %q", onDisk)
	}

	// 清空文件也是合法编辑（空串不是"没内容"）。
	if _, err := repo.WriteFile(root, "docs/notes.md", nil); err != nil {
		t.Fatalf("WriteFile(empty): %v", err)
	}
	if onDisk, err = os.ReadFile(filepath.Join(root, "docs", "notes.md")); err != nil || len(onDisk) != 0 {
		t.Fatalf("expected empty file: content=%q err=%v", onDisk, err)
	}

	// 目标不存在（从没打开过 / 已被删除）：显式失败，不悄悄新建同名文件。
	if _, err := repo.WriteFile(root, "docs/missing.md", []byte("x")); err == nil {
		t.Fatal("WriteFile created a new file, want explicit failure")
	}
	if _, statErr := os.Lstat(filepath.Join(root, "docs", "missing.md")); statErr == nil {
		t.Fatal("WriteFile 在目标缺失时仍创建了文件")
	}
}

func TestWriteFilePreservesModeAndLeavesNoTempFiles(t *testing.T) {
	root := t.TempDir()
	writeTreeFile(t, root, "run.sh", "#!/bin/sh\necho old\n")
	full := filepath.Join(root, "run.sh")
	before, err := os.Lstat(full)
	if err != nil {
		t.Fatal(err)
	}
	wantMode := before.Mode().Perm()

	if _, err := NewRepo().WriteFile(root, "run.sh", []byte("#!/bin/sh\necho new\n")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	info, err := os.Lstat(full)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != wantMode {
		t.Fatalf("mode = %v, want %v（保存不得改写权限位）", info.Mode().Perm(), wantMode)
	}
	// 原子发布的临时文件必须已被 rename 消费掉（不留垃圾在工作区里）。
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("leftover temp file: %s", entry.Name())
		}
	}
}

func TestWriteFileRejectsEscapesIgnoredAndSensitivePaths(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{
		"config/accounts.yaml",
		"config/accounts.local.yaml",
		"node_modules/pkg/index.js",
		".git/config",
		"dist/bundle.js",
	} {
		writeTreeFile(t, root, rel, "x")
	}
	repo := NewRepo()
	cases := []string{
		"", ".", "../outside.txt", "sub/../../outside.txt",
		filepath.Join(root, "a.txt"),
		"config/accounts.yaml", "config/accounts.local.yaml",
		"node_modules/pkg/index.js", ".git/config", "dist/bundle.js",
	}
	for _, rel := range cases {
		if _, err := repo.WriteFile(root, rel, []byte("pwned")); err == nil {
			t.Fatalf("WriteFile(%q) succeeded, want rejection（读面看不见就必须写不动）", rel)
		}
	}
	// 被拒绝的敏感文件内容未被动过。
	raw, err := os.ReadFile(filepath.Join(root, "config", "accounts.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "x" {
		t.Fatalf("敏感文件被改写：%q", raw)
	}
}

func TestWriteFileRejectsDirectoryAndSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	repo := NewRepo()
	if _, err := repo.WriteFile(root, "docs", []byte("x")); err == nil {
		t.Fatal("WriteFile succeeded on a directory")
	}

	outside := t.TempDir()
	writeTreeFile(t, outside, "secret.txt", "secret")
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if _, err := repo.WriteFile(root, "link.txt", []byte("pwned")); err == nil {
		t.Fatal("WriteFile succeeded through a symlink")
	}
	raw, err := os.ReadFile(filepath.Join(outside, "secret.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "secret" {
		t.Fatalf("符号链接目标被改写：%q", raw)
	}
}

func TestWriteFileRejectsBinaryTargets(t *testing.T) {
	root := t.TempDir()
	writeTreeFile(t, root, "logo.bin", "AB\x00CD")
	if _, err := NewRepo().WriteFile(root, "logo.bin", []byte("text")); err == nil {
		t.Fatal("WriteFile succeeded on a binary file（文本正文落到二进制上只会毁文件）")
	}
	raw, err := os.ReadFile(filepath.Join(root, "logo.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, []byte("AB\x00CD")) {
		t.Fatalf("二进制文件被改写：%q", raw)
	}
}

func TestWriteFileRejectsOversizeContent(t *testing.T) {
	root := t.TempDir()
	writeTreeFile(t, root, "big.txt", "keep")
	oversized := bytes.Repeat([]byte("a"), fileWriteHardLimit+1)
	if _, err := NewRepo().WriteFile(root, "big.txt", oversized); err == nil {
		t.Fatal("WriteFile accepted content above the hard limit（超上限必须拒绝，不能截断毁文件）")
	}
	raw, err := os.ReadFile(filepath.Join(root, "big.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "keep" {
		t.Fatalf("超限写入改动了原文件：%q", raw)
	}
}

func TestWriteFileMissingRoot(t *testing.T) {
	if _, err := NewRepo().WriteFile(filepath.Join(t.TempDir(), "missing"), "a.txt", []byte("x")); err == nil {
		t.Fatal("WriteFile succeeded on missing root")
	}
}
