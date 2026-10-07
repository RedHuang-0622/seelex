package scheduler

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// ── 定时任务的全局持久化（JSONL）────────────────────────────────────────
//
// 粒度的选择是这条通道的全部要点：**任务定义是全局资产，一行一变更**，
// 落在一个进程级 JSONL 文件里（默认 `<store>/scheduled-tasks.jsonl`）。
// 它刻意**不**按项目分区、也**不**按会话分片——同一份任务列表在任何工作区
// 下都该是同一份（"每天九点巡检"不因为切了项目就换一套）。
//
// 与之相对的是**触发产生的会话记录**：那仍然走会话自己的存储与读写纪律
// （按项目作用域分区、按会话隔离、按提交口径落盘）。本文件不碰会话正文，
// 只存"有哪些任务、周期/锚点/工作区/启用状态是什么"。
//
// 形态是 append-only：登记写一行、取消写一行墓碑（`deleted`），读取时按
// id 后写覆盖先写重放，得到当前任务集。因此没有整文件重写、也不需要把
// 索引和正文凑成一次原子提交；崩溃只会留下**残尾**（未换行收尾的半行），
// 读侧按"未收尾即未提交"跳过——与会话事件行的恢复语义同一口径。
//
// 只持久化**定义与启用状态**，不持久化上次运行结果（`LastResult` /
// `LogTail` / `RunCount`）：那些是运行期展示面，且命令输出可能含敏感内容，
// 不该因为"重启后还想看见上次结果"就落到盘上。

// ScheduledTaskRecord 是 JSONL 里的一行：一条任务定义的**一次变更**。
// Deleted=true 时是墓碑行（取消），只认 ID。
type ScheduledTaskRecord struct {
	ID        string                `json:"id"`
	Spec      dto.ScheduledTaskSpec `json:"spec"`
	Enabled   bool                  `json:"enabled"`
	Deleted   bool                  `json:"deleted,omitempty"`
	UpdatedAt time.Time             `json:"updated_at"`
}

// Persistence 是任务定义的全局持久化端口（调度器只依赖它，不认识文件系统）。
// Append 追加一次变更；Load 返回重放后的**当前任务集**（已丢弃墓碑与残尾）。
type Persistence interface {
	Append(record ScheduledTaskRecord) error
	Load() ([]ScheduledTaskRecord, error)
}

// FileStore 是 Persistence 的全局 JSONL 实现：一个路径、一条 append-only
// 通道、一行 JSON、一个换行。写侧只做"追加一整行"，把半行修复留给读侧。
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
func (store *FileStore) Append(record ScheduledTaskRecord) error {
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
func (store *FileStore) Load() ([]ScheduledTaskRecord, error) {
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
	latest := map[string]ScheduledTaskRecord{}
	order := make([]string, 0, 8)
	for _, line := range bytes.Split(data, []byte("\n")) {
		raw := strings.TrimSpace(string(line))
		if raw == "" {
			continue
		}
		var record ScheduledTaskRecord
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
	records := make([]ScheduledTaskRecord, 0, len(latest))
	for _, id := range order {
		record := latest[id]
		if record.Deleted {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}
