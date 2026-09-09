// v8 retention / LRU（M4）。
//
// 事实模型（my_design §4 I2）：
//   - LRU 行删除只发生在 watermark 之前（连续前缀），message_id/seq 保持
//     空洞、不重编号；锚 ≤ watermark 视为已淘汰区引用，不算损坏；
//   - mode=manual 时自动淘汰被拒绝，需用户确认（T-WM-01）；
//   - 实现 = 写新分片 → 原子替换 message.json → 更新 retention.json →
//     删旧分片（T-WM-04：崩溃只能落"旧分片+旧 head"或"新 head 已发布"二选
//     一，无中间态）。
package sessionstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// v8RetentionModeManual 是当前唯一实现模式（用户确认后行删除；dry_run=true
// 为默认安全姿态，见 my_design §11）。
const v8RetentionModeManual = "manual"

// v8RetentionHead 是 metadata/retention.json payload。
type v8RetentionHead struct {
	SessionID string `json:"session_id"`
	Mode      string `json:"mode"`
	DryRun    bool   `json:"dry_run"`
	// WatermarkSeq/MessageID 与 message head 一致（已淘汰水位）。
	WatermarkSeq       uint64    `json:"watermark_seq,omitempty"`
	WatermarkMessageID string    `json:"watermark_message_id,omitempty"`
	DeletedRows        uint64    `json:"deleted_rows,omitempty"`
	CompactThreshold   int       `json:"compact_frame_threshold,omitempty"`
	RawBytesAlert      uint64    `json:"raw_bytes_alert,omitempty"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// ErrV8RetentionRequiresConfirm 表示 manual 模式自动淘汰被拒绝。
var ErrV8RetentionRequiresConfirm = errors.New("v8: LRU deletion requires user confirmation (mode=manual)")

// ErrV8ForkBeforeWatermark 表示 fork 起点早于 LRU watermark（I7）。
var ErrV8ForkBeforeWatermark = errors.New("v8: fork start must be >= LRU watermark")

var v8RetentionDefaults = struct {
	compactFrameThreshold int
	rawBytesAlert         uint64
}{
	compactFrameThreshold: 30,
	rawBytesAlert:         256 << 20,
}

func (store *v8Store) v8ReadRetentionHeadLocked(key Key) (v8RetentionHead, error) {
	return v8ReadModuleHeadPayload[v8RetentionHead](store, key, v8ModuleRetention)
}

// v8ReadRetentionHead 读取 retention head（缺失时先补建默认 manual head）。
func (store *v8Store) v8ReadRetentionHead(key Key) (v8RetentionHead, error) {
	store.retentionMu.Lock()
	defer store.retentionMu.Unlock()
	head, err := store.v8ReadRetentionHeadLocked(key)
	if err == nil {
		return head, nil
	}
	head = v8RetentionHead{
		SessionID:        key.SessionID,
		Mode:             v8RetentionModeManual,
		DryRun:           true,
		CompactThreshold: v8RetentionDefaults.compactFrameThreshold,
		RawBytesAlert:    v8RetentionDefaults.rawBytesAlert,
		UpdatedAt:        time.Now().UTC(),
	}
	if _, err := store.publishV8ModuleHead(key, v8ModuleRetention, "retention-init", head, head.UpdatedAt); err != nil {
		return v8RetentionHead{}, err
	}
	_ = store.registerV8Module(key, v8ModuleRetention, store.v8ModulePath(key, v8ModuleRetention))
	return head, nil
}

// v8LRUDelete 删除 watermark 之前的连续前缀（用户确认后调用）。
// upToSeq 含端点：删除 [watermark+1, upToSeq]。
func (store *v8Store) v8LRUDelete(key Key, upToSeq uint64, confirmed bool) (v8RetentionHead, error) {
	store.retentionMu.Lock()
	defer store.retentionMu.Unlock()
	retention, err := store.v8ReadRetentionHeadLocked(key)
	if err != nil {
		retention = v8RetentionHead{
			SessionID: key.SessionID, Mode: v8RetentionModeManual, DryRun: true,
			CompactThreshold: v8RetentionDefaults.compactFrameThreshold,
			RawBytesAlert:    v8RetentionDefaults.rawBytesAlert,
		}
	}
	if retention.Mode == v8RetentionModeManual && !confirmed {
		return v8RetentionHead{}, ErrV8RetentionRequiresConfirm
	}
	oldWatermark := retention.WatermarkSeq
	if oldWatermark >= upToSeq {
		return retention, nil
	}
	store.messageMu.Lock()
	defer store.messageMu.Unlock()
	messageHead, err := store.v8ReadMessageHeadLocked(key)
	if err != nil {
		return v8RetentionHead{}, err
	}
	if upToSeq > messageHead.LastSeq {
		upToSeq = messageHead.LastSeq
	}
	all, err := store.v8ReadRowsLocked(key, 1, 0)
	if err != nil {
		return v8RetentionHead{}, err
	}
	remaining := make([]Event, 0, len(all))
	for _, row := range all {
		if row.Seq > upToSeq {
			remaining = append(remaining, row)
		}
	}
	if len(remaining) > 0 && remaining[0].Seq > upToSeq+1 && remaining[0].Seq <= messageHead.LastSeq {
		return v8RetentionHead{}, errors.New("v8: LRU delete range is not a continuous prefix")
	}
	oldShards := append([]v8ShardInfo(nil), messageHead.Shards...)
	newHead := messageHead
	newHead.Shards = nil
	newHead.TotalRows = 0
	newHead.WatermarkSeq = upToSeq
	newHead.WatermarkMessageID = messageHead.LastMessageID
	if len(remaining) > 0 {
		newHead.WatermarkMessageID = remaining[0].MessageID
	}
	dir := store.v8MessageDir(key)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return v8RetentionHead{}, err
	}
	newFiles := make([]string, 0, 1)
	for start := 0; start < len(remaining); start += store.shardRows {
		end := start + store.shardRows
		if end > len(remaining) {
			end = len(remaining)
		}
		batch := remaining[start:end]
		shardPath := store.v8ShardPath(key, batch[0].Seq, batch[len(batch)-1].Seq)
		if err := writeV8RowsNewFile(shardPath, batch); err != nil {
			for _, path := range newFiles {
				_ = os.Remove(path)
			}
			return v8RetentionHead{}, err
		}
		newHead.Shards = append(newHead.Shards, v8ShardInfo{
			Path: filepath.Base(shardPath), FromSeq: batch[0].Seq, ToSeq: batch[len(batch)-1].Seq,
			Count: len(batch), SHA256: v8FileSHA256(shardPath),
		})
		newHead.TotalRows += uint64(len(batch))
		newFiles = append(newFiles, shardPath)
	}
	if len(newHead.Shards) == 0 {
		newHead.Shards = nil
	}
	if _, err := store.publishV8ModuleHead(key, v8ModuleMessage, "lru-"+randomID(), newHead, time.Now().UTC()); err != nil {
		for _, path := range newFiles {
			_ = os.Remove(path)
		}
		return v8RetentionHead{}, err
	}
	// head 已发布后删旧分片（失败只留孤儿文件，reader 以 head 为准）。
	for _, shard := range oldShards {
		_ = os.Remove(filepath.Join(dir, shard.Path))
	}
	retention.WatermarkSeq = upToSeq
	retention.WatermarkMessageID = newHead.WatermarkMessageID
	retention.DeletedRows += upToSeq - oldWatermark
	retention.UpdatedAt = time.Now().UTC()
	if _, err := store.publishV8ModuleHead(key, v8ModuleRetention, "lru-"+randomID(), retention, retention.UpdatedAt); err != nil {
		return v8RetentionHead{}, err
	}
	return retention, nil
}

// writeV8RowsNewFile 用新文件完整写一批行（LRU 重写用）。
func writeV8RowsNewFile(path string, rows []Event) error {
	_ = os.Remove(path)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	buffer := make([]byte, 0, 4096)
	for _, row := range rows {
		data, err := json.Marshal(row)
		if err != nil {
			file.Close()
			return err
		}
		buffer = append(buffer, data...)
		buffer = append(buffer, '\n')
	}
	if _, err := file.Write(buffer); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// v8SortShards 按 FromSeq 排序分片索引（防御读取顺序）。
func v8SortShards(shards []v8ShardInfo) {
	sort.Slice(shards, func(i, j int) bool { return shards[i].FromSeq < shards[j].FromSeq })
}
