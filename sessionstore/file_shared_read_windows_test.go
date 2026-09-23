//go:build windows

package sessionstore

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// Windows 常量（syscall 未导出 DELETE 访问权与 ERROR_SHARING_VIOLATION）。
const (
	// deleteAccess = DELETE：rename/删除内部以它打开目标。
	deleteAccess = 0x00010000
	// errorSharingViolation = ERROR_SHARING_VIOLATION(32)：缺 DELETE 共享位时的失败码。
	errorSharingViolation = syscall.Errno(32)
)

// Windows 只读共享句柄契约（2026-09-23，C2 读路径解耦的配套修复）。
//
// 背景：C2 让 message 读路径不再持 `messageMu`，读者第一次与写者的 rename 发布 /
// 淘汰删除真并发。Go 默认读句柄用 `share=READ|WRITE`（不带 `DELETE`），于是读者
// 会在发布窗口里 open 失败（`ERROR_SHARING_VIOLATION`），并挡住 LRU/reap 的删除。
// 生产侧统一走 `openSharedRead` / `readSharedFile`（带 `FILE_SHARE_DELETE`）。
//
// 两条测试都用确定性形态钉住契约（不靠耗时/调度），各自带一条反向护栏说明"缺
// DELETE 共享位"就是缺陷来源——即这层封装不是空操作。

// holdForDelete 以「GENERIC_READ|DELETE 访问 + 全共享位」打开 path，等价于 rename
// 内部占住目标的那一刻（rename 以 DELETE 访问打开目标；目标上带 DELETE 访问的句柄
// 会让缺 `FILE_SHARE_DELETE` 的读者拿到 ERROR_SHARING_VIOLATION）。
func holdForDelete(t *testing.T, path string) *os.File {
	t.Helper()
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ|deleteAccess,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	return os.NewFile(uintptr(handle), path)
}

// TestReadSharedFileSurvivesPublishWindow 钉住缺陷 1：目标被"发布中"的 DELETE 访问
// 句柄占住时，生产读入口必须仍然读得到——这正是默认共享位会失败的时刻。
func TestReadSharedFileSurvivesPublishWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "message.json")
	content := []byte(`{"schema_version":1,"module":"message"}`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	holder := holdForDelete(t, path)
	defer holder.Close()

	got, err := readSharedFile(path)
	if err != nil {
		t.Fatalf("发布窗口内读失败（缺 FILE_SHARE_DELETE 的形态）: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("读到 %q, want %q", got, content)
	}

	// 反向护栏：同一时刻 Go 默认读句柄必然失败（生产缺陷的确定性复现）。若将来
	// Go 给默认句柄加上 FILE_SHARE_DELETE，本断言会红——那是好事，说明封装可退役。
	if _, err := os.ReadFile(path); err == nil {
		t.Fatal("Go 默认读句柄在发布窗口内竟然成功：封装前提（缺 FILE_SHARE_DELETE）已失效")
	} else if !errors.Is(err, errorSharingViolation) {
		t.Logf("默认读句柄失败于 %v（期望 ERROR_SHARING_VIOLATION，Windows 版本差异可接受）", err)
	}
}

// TestOpenSharedReadDoesNotBlockEvictionDelete 钉住缺陷 2：读者持柄时，淘汰/回收
// 的 `os.Remove` 必须照样成功，且读者手上仍是打开那一刻的完整旧内容。
func TestOpenSharedReadDoesNotBlockEvictionDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "message_1_100.jsonl")
	content := []byte("{\"seq\":1}\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	reader, err := openSharedRead(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	if err := os.Remove(path); err != nil {
		t.Fatalf("读者持柄挡住了淘汰删除（LRU/reap 会失败）: %v", err)
	}
	data := make([]byte, len(content))
	count, err := reader.Read(data)
	if err != nil && count == 0 {
		t.Fatalf("删除之后读者句柄失效: %v", err)
	}
	if string(data[:count]) != string(content) {
		t.Fatalf("读者读到 %q, want %q（打开那一刻的完整旧版本）", data[:count], content)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("删除后路径仍在: %v", statErr)
	}

	// 反向护栏：默认共享位的读者持柄时同一删除必然失败。
	guard := filepath.Join(t.TempDir(), "message_1_100.jsonl")
	if err := os.WriteFile(guard, content, 0o600); err != nil {
		t.Fatal(err)
	}
	guardReader, err := os.Open(guard)
	if err != nil {
		t.Fatal(err)
	}
	defer guardReader.Close()
	if err := os.Remove(guard); err == nil {
		t.Fatal("Go 默认读句柄竟然没挡住删除：封装前提（缺 FILE_SHARE_DELETE）已失效")
	} else if !errors.Is(err, errorSharingViolation) {
		t.Logf("默认读句柄下的删除失败于 %v（期望 ERROR_SHARING_VIOLATION，Windows 版本差异可接受）", err)
	}
}
