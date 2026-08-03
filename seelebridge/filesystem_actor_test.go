package seelebridge

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestFileSystemActorSerializesSamePathWrites 验证同路径写互斥：N 个并发
// Edit 同一文件，全部替换必须全部生效（无 lost update）。
func TestFileSystemActorSerializesSamePathWrites(t *testing.T) {
	fs := NewFileSystemActor()
	path := filepath.Join(t.TempDir(), "target.txt")
	if err := fs.Write(path, []byte("seed")); err != nil {
		t.Fatal(err)
	}
	const writers = 8
	var group sync.WaitGroup
	for index := 0; index < writers; index++ {
		group.Add(1)
		go func(tag string) {
			defer group.Done()
			if _, err := fs.Edit(path, "seed", "seed"+tag); err != nil {
				t.Errorf("edit %s: %v", tag, err)
			}
		}(string(rune('a' + index)))
	}
	group.Wait()
	data, err := fs.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for index := 0; index < writers; index++ {
		tag := string(rune('a' + index))
		if !strings.Contains(content, tag) {
			t.Fatalf("content %q missing tag %q (lost update — write not serialized)", content, tag)
		}
	}
}

// TestFileSystemActorParallelDifferentPaths 验证不同路径并行（无全局锁阻塞）。
func TestFileSystemActorParallelDifferentPaths(t *testing.T) {
	fs := NewFileSystemActor()
	root := t.TempDir()
	var group sync.WaitGroup
	for index := 0; index < 16; index++ {
		group.Add(1)
		go func(n int) {
			defer group.Done()
			path := filepath.Join(root, "file-"+string(rune('a'+n))+".txt")
			if err := fs.Write(path, []byte("x")); err != nil {
				t.Errorf("write %d: %v", n, err)
			}
		}(index)
	}
	group.Wait()
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 16 {
		t.Fatalf("entries=%d err=%v, want 16 files", len(entries), err)
	}
}

// TestFileSystemActorEditRejectsMissingOldString 验证 old_string 缺失报错。
func TestFileSystemActorEditRejectsMissingOldString(t *testing.T) {
	fs := NewFileSystemActor()
	path := filepath.Join(t.TempDir(), "target.txt")
	if err := fs.Write(path, []byte("hello world")); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Edit(path, "absent", "x"); err == nil {
		t.Fatal("edit with missing old_string must fail")
	}
	count, err := fs.Edit(path, "world", "seelex")
	if err != nil || count != 1 {
		t.Fatalf("edit count=%d err=%v, want 1", count, err)
	}
	data, _ := fs.Read(path)
	if string(data) != "hello seelex" {
		t.Fatalf("content = %q", data)
	}
}
