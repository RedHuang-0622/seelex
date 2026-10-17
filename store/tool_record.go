package store

// tool_record.go — **工具记录读数**的会话级持久面（2026-10-15）。
//
// 粒度与频率（用户口径）：读数是**会话作用域**的（跟会话自己的目录走：切项目/会话切换
// 不串写，回收会话即回收读数），落盘按**批次**——一行 = 一次批量更新（"到目前为止的整份
// 累计"），读侧取**最后一行**即当前读数。为什么不做"每次工具调用 append 一行"：
// 工具调用是回合内高频事件，那样会让磁盘一直忙；批次的调用点是真相源提交时刻
// （草稿物化进真相源等，见 application/core 的 flushToolRecord）。
//
// 形状归上层：本文件**不认识**载荷字段（`payload any` 进、`json.RawMessage` 出）——
// 与 board.go 同口径：存储层不复制上层 DTO 的形状。
//
// 崩溃语义：残尾（最后一行没有收尾换行）= 没写完的一行，读侧跳过并回退到上一行；
// 解不动的坏行同样跳过。追加用 O_APPEND 一整行写出，与既有 JSONL 同一套纪律。

import (
	"context"
	"encoding/json"
	"errors"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
	"os"
	"path/filepath"
	"strings"
)

// ToolRecordFile 是工具记录读数文件名（落在会话目录根部：`<sessionRoot>/tool-record.jsonl`）。
const ToolRecordFile = "tool-record.jsonl"

// ToolRecordRepository 是工具记录读数的会话级读/写面（JSON 后端实现它）。
type ToolRecordRepository interface {
	// AppendToolRecordSnapshot 追加一行整份读数（一次批量更新 = 一行）。
	AppendToolRecordSnapshot(ctx context.Context, key Key, payload any) error
	// ReadToolRecordSnapshot 读回最后一行整份读数（没有更新过 = false）。
	ReadToolRecordSnapshot(ctx context.Context, key Key) (json.RawMessage, bool, error)
}

// 编译期钉子：JSON 后端就是这套读/写面（有就有、没有就是没装配的判据在 ToolRecordFor）。
var _ ToolRecordRepository = (*jsonRepository)(nil)

// ToolRecord 把 Repository 收窄到工具记录面（与 Teamwork 同一手法：有就有、没有就是没装配）。
func ToolRecord(repository Repository) (ToolRecordRepository, bool) {
	toolRecord, ok := repository.(ToolRecordRepository)
	return toolRecord, ok
}

// ToolRecordFor 返回**当前活跃后端**的工具记录读/写面（JSON 后端实现它；其他后端 false）。
func (router *Router) ToolRecordFor() (ToolRecordRepository, bool) {
	if router == nil {
		return nil, false
	}
	var repository ToolRecordRepository
	var ok bool
	_ = router.withRepository(func(current Repository, _ string) error {
		repository, ok = ToolRecord(current)
		return nil
	})
	return repository, ok
}

// toolRecordPath 返回该会话的工具记录文件路径。
func (store *storeEngine) toolRecordPath(key Key) string {
	return filepath.Join(store.sessionRoot(key), ToolRecordFile)
}

// AppendToolRecordSnapshot 追加一行整份读数。
//
// 锁：复用同包的**按路径分片锁**（`projectBoardLock` 只按路径分片、与看板无关；
// 等 project_board 的会话分目录重构落地后它会改名成 fileLock，届时这里同步改名）。
func (repository *jsonRepository) AppendToolRecordSnapshot(_ context.Context, key Key, payload any) error {
	if err := key.validate(); err != nil {
		return err
	}
	if repository.layout == nil {
		return errors.New("session storage: tool record requires the v8 layout engine")
	}
	line, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	path := repository.layout.toolRecordPath(key)
	lock := projectBoardLock(path)
	lock.Lock()
	defer lock.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	// 一整行 + 换行一次写出：残尾（未换行）与完整行由此可辨。
	if _, err := file.Write(append(line, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

// ReadToolRecordSnapshot 读回最后一行整份读数。
//
// 文件不存在 = 没更新过（false，不是错误）；残尾与坏行跳过（回退到**上一行完整快照**：
// 快照是整份的，回退不会丢"上一批之前的读数"）。
func (repository *jsonRepository) ReadToolRecordSnapshot(_ context.Context, key Key) (json.RawMessage, bool, error) {
	if err := key.validate(); err != nil {
		return nil, false, err
	}
	if repository.layout == nil {
		return nil, false, errors.New("session storage: tool record requires the v8 layout engine")
	}
	raw, err := storeroot.ReadSharedFile(repository.layout.toolRecordPath(key))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var last json.RawMessage
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || !json.Valid([]byte(trimmed)) {
			continue
		}
		last = json.RawMessage(trimmed)
	}
	if last == nil {
		return nil, false, nil
	}
	return last, true, nil
}
