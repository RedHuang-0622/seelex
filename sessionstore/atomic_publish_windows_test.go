//go:build windows

// 原子发布在 Windows 上的持柄契约：目标文件被瞬时句柄占住时，rename 必须靠
// 有界退避自救；超出预算则显式失败且不留残 tmp（H1/H7 同一形态）。
package sessionstore

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

// holdExclusive 以 share mode 0 打开 path（任何 rename 覆盖都会失败），返回释放函数。
// Go 的 os.OpenFile 总带 share read/write，故直接用 CreateFile 申请独占。
func holdExclusive(t *testing.T, path string) func() {
	t.Helper()
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ, 0, nil,
		syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { _ = syscall.CloseHandle(handle) }) })
	return func() { once.Do(func() { _ = syscall.CloseHandle(handle) }) }
}

func TestWriteAtomicSurvivesTransientHandle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "head.json")
	if err := os.WriteFile(path, []byte("v0"), 0o600); err != nil {
		t.Fatal(err)
	}
	release := holdExclusive(t, path)
	go func() {
		time.Sleep(25 * time.Millisecond)
		release()
	}()
	begin := time.Now()
	if err := writeAtomic(path, []byte("v1"), 0o600); err != nil {
		t.Fatalf("瞬时持柄（25ms，在重试预算内）未被救回: %v", err)
	}
	spent := time.Since(begin)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "v1" {
		t.Fatalf("head content = %q want v1", data)
	}
	if spent < 20*time.Millisecond {
		t.Fatalf("发布只用了 %v：重试没有真正等待持柄者松开", spent)
	}
	t.Logf("发布在 %v 内完成（含退避等待）", spent)
}

func TestWriteAtomicGivesUpWithinBudgetAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "head.json")
	if err := os.WriteFile(path, []byte("v0"), 0o600); err != nil {
		t.Fatal(err)
	}
	release := holdExclusive(t, path)
	// 持柄时间刻意超过全部退避预算 → 必须显式失败。
	timer := time.AfterFunc(renameBackoff[len(renameBackoff)-1]*3, release)
	defer timer.Stop()
	err := writeAtomic(path, []byte("v1"), 0o600)
	if err == nil {
		release()
		t.Fatal("超出重试预算的发布被当作成功")
	}
	// share=0 的句柄连普通读也会挡住，检查后置条件前必须先松开。
	release()
	t.Logf("超预算发布按预期失败: %v", err)
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".tmp" {
			t.Fatalf("失败发布残留 tmp: %s", entry.Name())
		}
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "v0" {
		t.Fatalf("失败发布改动了目标内容: %q", data)
	}
}
