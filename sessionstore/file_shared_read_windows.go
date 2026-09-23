//go:build windows

package sessionstore

import (
	"io"
	"io/fs"
	"os"
	"syscall"
)

// openSharedRead 以只读 + `FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE`
// 打开只读共享文件（模块 head / message 分片），并返回可直接读的句柄。
//
// 为什么要自己写这一层：Go 的 `os.OpenFile` / `os.ReadFile` 在 Windows 上固定使用
// `share=READ|WRITE`（**不带 `DELETE`**），而本仓库的 head 是由 `writeAtomic`
// 的 rename 原子发布、分片会被 LRU 淘汰 / reap / 压缩重写删除。C2（message 读路径
// 不再持 `messageMu`）之前，热读路径与写者同锁，这个窗口被锁序列化掉了；解耦之后
// 读者真并发，默认共享位立刻变成两个缺陷：
//
//  1. **读者 open 在发布窗口里失败**：rename 内部以 DELETE 访问占住目标，缺
//     `FILE_SHARE_DELETE` 的 open 拿到 `ERROR_SHARING_VIOLATION`（32）。实测：
//     4 写 + 4 读并发时根包 `TestStorageConcurrentSessionLockProfile` 的
//     `LoadEventTail` 头读报 `metadata/message.json: The process cannot access the
//     file because it is being used by another process`；同形状单点实验里默认读
//     5248 次失败 141 次，改成带 `FILE_SHARE_DELETE` 读 9130 次零失败。
//  2. **读者持柄挡住回收删除**：`os.Remove`（LRU 淘汰 / reap 未索引分片 / 压缩
//     重写）需要 DELETE 共享位，否则 `ERROR_SHARING_VIOLATION`（32）。实测：2 个
//     `os.OpenFile(O_RDWR)` 读者让 2192 次删除失败 2077 次；带 `FILE_SHARE_DELETE`
//     的句柄下删除全部成功。
//
// 边界（别夸大）：**rename 覆盖仍要求目标无任何打开句柄**（实测任何共享位下都返回
// `ERROR_ACCESS_DENIED` / 5），所以写侧 `renameBackoff` 依旧必要——本函数解决的是
// 读者自己失败与读者挡住删除这两类，不是"读者不再让写者退避"。语义上仍是快照读：
// rename 成功后新 open 拿到新文件，已打开的句柄继续读**旧版本的完整内容**（不是
// 半成品）。
func openSharedRead(path string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(handle), path), nil
}

// readSharedFile 读整个只读共享文件（模块 head / 目录枚举 meta），是
// `os.ReadFile` + 上面共享位的等价物；错误保持 `*fs.PathError` 形状，
// `errors.Is(err, fs.ErrNotExist)` 口径不变。
func readSharedFile(path string) ([]byte, error) {
	file, err := openSharedRead(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, &fs.PathError{Op: "read", Path: path, Err: err}
	}
	return data, nil
}
