// Package schedule 承载**定时任务定义**的全局 JSONL 落盘：一个路径、一条 append-only
// 通道、一行 JSON、一个换行。写侧只做"追加一整行"，残尾修复留给读侧。
//
// 粒度的选择是这条通道的全部要点：**任务定义是全局资产，一行一变更**，落在进程级
// JSONL 文件里（默认 `<store>/scheduled-tasks.jsonl`）。它刻意**不**按项目分区、
// 也**不**按会话分片——同一份任务列表在任何工作区下都该是同一份。触发产生的会话
// 记录仍走会话自己的存储与读写纪律，本包不碰会话正文。
//
// 物理位置：2026-10-16 按 plan §2.12.1 从 `backend/engine/schedule/persistence.go` 归位
// 到 `store/schedule/`（同一份实现，未新增第二份）。
package schedule

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// FileStore 是 contract.ScheduledTaskPersistence 的全局 JSONL 实现。
//
// 端口由实现方自己钉住：装配处换了实现也改不动这条断言，接口漂移会在这里立刻编译失败。
var _ contract.ScheduledTaskPersistence = (*FileStore)(nil)

type FileStore struct {
	mu   sync.Mutex
	path string
}

// NewFileStore 构造全局 JSONL 任务定义存储（目录不存在时在首次写入时创建）。
func NewFileStore(path string) *FileStore { return &FileStore{path: path} }

// Path 返回落盘路径（空 = 未配置，调度器此时不持久化）。
func (store *FileStore) Path() string {
	if store == nil {
		return ""
	}
	return store.path
}

// Append 追加一行变更：先补齐目录，再以 O_APPEND 追加一整行 JSON。
// 失败返回错误——调用方据此决定"这次登记/取消算不算成立"。
func (store *FileStore) Append(record dto.ScheduledTaskRecord) error {
	if store == nil || strings.TrimSpace(store.path) == "" {
		return nil
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = time.Now().UTC()
	}
	line, err := json.Marshal(record)
	if err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(store.path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(store.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
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

// Load 读回当前任务集：逐行解码，后写覆盖先写（按 ID），墓碑行删除该 ID。
// 文件不存在 = 空集（不是错误）；半行残尾与解不动的行跳过（append-only
// 的崩溃恢复语义：没有收尾换行的一行就是没提交完的一行）。
func (store *FileStore) Load() ([]dto.ScheduledTaskRecord, error) {
	if store == nil || strings.TrimSpace(store.path) == "" {
		return nil, nil
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	data, err := os.ReadFile(store.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	// 残尾（最后一行没有收尾换行）= 没写完的一行，按未提交丢弃；后面即使
	// 碰巧还能解出来，也不让它进任务集。
	if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
		if cut := bytes.LastIndexByte(data, '\n'); cut >= 0 {
			data = data[:cut+1]
		} else {
			data = nil
		}
	}
	latest := map[string]dto.ScheduledTaskRecord{}
	order := make([]string, 0, 8)
	for _, line := range bytes.Split(data, []byte("\n")) {
		raw := strings.TrimSpace(string(line))
		if raw == "" {
			continue
		}
		var record dto.ScheduledTaskRecord
		if err := json.Unmarshal(line, &record); err != nil {
			continue // 读不动的行（残尾/手改）不进任务集
		}
		id := strings.TrimSpace(record.ID)
		if id == "" {
			continue
		}
		if _, seen := latest[id]; !seen {
			order = append(order, id)
		}
		record.ID = id
		latest[id] = record
	}
	records := make([]dto.ScheduledTaskRecord, 0, len(latest))
	for _, id := range order {
		record := latest[id]
		if record.Deleted {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}
