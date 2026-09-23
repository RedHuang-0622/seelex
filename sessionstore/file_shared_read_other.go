//go:build !windows

package sessionstore

import "os"

// openSharedRead 在非 Windows 平台直接走 `os.Open`：POSIX 没有共享位概念，
// `rename`/`unlink` 只改目录项，已经打开的 fd 继续读旧内容——正是 Windows 版
// （`file_shared_read_windows.go`）用 `FILE_SHARE_DELETE` 恢复的语义。
func openSharedRead(path string) (*os.File, error) {
	return os.Open(path)
}

// readSharedFile 在非 Windows 平台等价于 `os.ReadFile`。
func readSharedFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
