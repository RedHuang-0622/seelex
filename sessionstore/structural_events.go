// v8 EVENT 通道：结构性操作摘要（compacted/fork/subagent/interrupted/
// rolled_back/…）。
//
// 事实模型（my_design §4/附录 A）：
//   - EVENT 不参与模型上下文装配（I4）；anchor_message_id = 事件发生在
//     该行之后（从下一事件行起生效，含端点语义的另一面）；
//   - append-only，分片按事件数（默认 100），head 在 metadata/event.json；
//   - 幂等：commit_id + 事件指纹去重（T-EV-02）；
//   - 固定写序 message → event；崩溃允许 event 短窗口落后于 message，读
//     者以 message/compact 为准（R2-EVENT-1）。
package sessionstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// structuralEventKind 是结构性操作类别（沿用 my_design 附录 A.1 名单）。
type structuralEventKind string

const (
	structuralEventCompacted    structuralEventKind = "compacted"
	structuralEventFork         structuralEventKind = "fork"
	structuralEventSubagent     structuralEventKind = "subagent"
	structuralEventInterrupted  structuralEventKind = "interrupted"
	structuralEventRolledBack   structuralEventKind = "rolled_back"
	structuralEventRequestBegin structuralEventKind = "request_begin"
	structuralEventRequestEnd   structuralEventKind = "request_end"
	structuralEventTurnBegin    structuralEventKind = "turn_begin"
	structuralEventTurnEnd      structuralEventKind = "turn_end"
	structuralEventTokenUsage   structuralEventKind = "token_usage"
	structuralEventArchived     structuralEventKind = "session_archived"
)

// structuralEvent 是一行结构性事件摘要。
type structuralEvent struct {
	EventID uint64              `json:"event_id"`
	Kind    structuralEventKind `json:"kind"`
	// AnchorMessageID = 事件发生在该行之后（anchor 是 message 坐标）。
	AnchorMessageID string          `json:"anchor_message_id,omitempty"`
	AnchorSeq       uint64          `json:"anchor_seq,omitempty"`
	FrameID         string          `json:"frame_id,omitempty"`
	CommitID        string          `json:"commit_id"`
	Payload         json.RawMessage `json:"payload,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

// eventHeadRecord 是 metadata/event.json payload。
type eventHeadRecord struct {
	SessionID string      `json:"session_id"`
	LastID    uint64      `json:"last_event_id"`
	Shards    []shardInfo `json:"shards,omitempty"`
	Total     uint64      `json:"total"`
}

func (store *storeEngine) structuralEventDir(key Key) string {
	return filepath.Join(store.sessionRoot(key), "event")
}

func (store *storeEngine) structuralEventShardPath(key Key, fromID, toID uint64) string {
	return filepath.Join(store.structuralEventDir(key), fmt.Sprintf("event_%d_%d.jsonl", fromID, toID))
}

func emptyEventHead(key Key) eventHeadRecord {
	return eventHeadRecord{SessionID: key.SessionID}
}

// structuralEventCommit 追加一提交的 EVENT 行并原子发布 event.json。
func (store *storeEngine) structuralEventCommit(key Key, commitID string, events []structuralEvent) (eventHeadRecord, error) {
	store.eventMu.Lock()
	defer store.eventMu.Unlock()
	if _, err := store.ensureLayoutGuide(key); err != nil {
		return eventHeadRecord{}, err
	}
	if commitID == "" {
		commitID = randomID()
	}
	head, err := store.readEventHeadLocked(key)
	if err != nil {
		return eventHeadRecord{}, err
	}
	if err := store.reapEventUnpublishedLocked(key, head); err != nil {
		return eventHeadRecord{}, err
	}
	existing, err := store.readEventShardsLocked(key, head)
	if err != nil {
		return eventHeadRecord{}, err
	}
	seenExisting := make(map[string]bool, len(existing))
	for _, row := range existing {
		seenExisting[structuralEventFingerprint(row)] = true
	}
	delta, err := structuralEventDeltaLocked(head, events, commitID, seenExisting)
	if err != nil {
		return eventHeadRecord{}, err
	}
	if len(delta) == 0 {
		return head, nil
	}
	if err := store.appendEventsLocked(key, &head, delta); err != nil {
		return eventHeadRecord{}, err
	}
	if _, err := store.publishModuleHead(key, moduleEvent, commitID, head, time.Now().UTC()); err != nil {
		return eventHeadRecord{}, err
	}
	return head, store.registerModule(key, moduleEvent, store.modulePath(key, moduleEvent))
}

func (store *storeEngine) readEventHeadLocked(key Key) (eventHeadRecord, error) {
	headFile, err := store.readModuleHeadFile(key, moduleEvent)
	if errors.Is(err, fs.ErrNotExist) {
		return emptyEventHead(key), nil
	}
	if err != nil {
		return eventHeadRecord{}, err
	}
	return decodeHeadPayload[eventHeadRecord](headFile)
}

func (store *storeEngine) readEventHead(key Key) (eventHeadRecord, error) {
	store.eventMu.Lock()
	defer store.eventMu.Unlock()
	return store.readEventHeadLocked(key)
}

// reapEventUnpublishedLocked 删除 head 未索引的事件分片并截掉尾分片超出
// head 的行（与 message 相同的发布点恢复语义）。
func (store *storeEngine) reapEventUnpublishedLocked(key Key, head eventHeadRecord) error {
	dir := store.structuralEventDir(key)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	indexed := make(map[string]bool, len(head.Shards))
	for _, shard := range head.Shards {
		indexed[filepath.Clean(filepath.Join(dir, shard.Path))] = true
	}
	for _, entry := range entries {
		if entry.IsDir() || !isEventShardFile(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if !indexed[filepath.Clean(path)] {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}
	if len(head.Shards) == 0 {
		return nil
	}
	last := head.Shards[len(head.Shards)-1]
	return truncateStructuralEventsAfter(filepath.Join(dir, last.Path), head.LastID)
}

func isEventShardFile(name string) bool {
	var fromID, toID uint64
	if _, err := fmt.Sscanf(name, "event_%d_%d.jsonl", &fromID, &toID); err != nil {
		return false
	}
	return fromID > 0 && toID >= fromID
}

func truncateStructuralEventsAfter(path string, headID uint64) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	if err := truncateCrashTail(file); err != nil {
		return err
	}
	rows, err := readStructuralEventsFile(file)
	if err != nil {
		return err
	}
	if len(rows) == 0 || rows[len(rows)-1].EventID <= headID {
		return nil
	}
	keep := 0
	for index, row := range rows {
		if row.EventID > headID {
			break
		}
		keep = index + 1
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	segments := bytes.Split(data, []byte{'\n'})
	end := 0
	for index := 0; index < keep && index < len(segments); index++ {
		end += len(segments[index]) + 1
	}
	return file.Truncate(int64(end))
}

func readStructuralEventsFile(file *os.File) ([]structuralEvent, error) {
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if stat.Size() == 0 {
		return nil, nil
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	return decodeStructuralEvents(data), nil
}

func decodeStructuralEvents(data []byte) []structuralEvent {
	segments := bytes.Split(data, []byte{'\n'})
	rows := make([]structuralEvent, 0, len(segments))
	for index, segment := range segments {
		if index == len(segments)-1 && len(bytes.TrimSpace(segment)) > 0 {
			continue
		}
		segment = bytes.TrimSpace(segment)
		if len(segment) == 0 {
			continue
		}
		var row structuralEvent
		if json.Unmarshal(segment, &row) != nil {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

// structuralEventDeltaLocked 计算新增 EVENT 行（EventID=0 → 续号；≤ head 的重复
// 提交跳过；commit 内同指纹去重）。
func structuralEventDeltaLocked(head eventHeadRecord, events []structuralEvent, commitID string, seenExisting map[string]bool) ([]structuralEvent, error) {
	if len(events) == 0 {
		return nil, nil
	}
	next := head.LastID
	seen := make(map[string]bool, len(events))
	for fingerprint := range seenExisting {
		seen[fingerprint] = true
	}
	delta := make([]structuralEvent, 0, len(events))
	for _, row := range events {
		row.CommitID = commitID
		fingerprint := structuralEventFingerprint(row)
		if seen[fingerprint] {
			continue
		}
		if row.EventID == 0 {
			next++
			row.EventID = next
		} else if row.EventID <= head.LastID {
			continue
		} else if row.EventID <= next {
			return nil, fmt.Errorf("session storage: event ids must be strictly increasing (id %d)", row.EventID)
		} else {
			next = row.EventID
		}
		seen[fingerprint] = true
		delta = append(delta, row)
	}
	return delta, nil
}

// readEventShardsLocked 读取 head 内全部已发布 EVENT 行。
func (store *storeEngine) readEventShardsLocked(key Key, head eventHeadRecord) ([]structuralEvent, error) {
	var out []structuralEvent
	for _, shard := range head.Shards {
		rows, err := readStructuralEventsAt(filepath.Join(store.structuralEventDir(key), shard.Path))
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

func structuralEventFingerprint(event structuralEvent) string {
	var builder bytes.Buffer
	builder.WriteString(string(event.Kind))
	builder.WriteByte(0)
	builder.WriteString(event.AnchorMessageID)
	builder.WriteByte(0)
	builder.WriteString(fmt.Sprintf("%d", event.AnchorSeq))
	builder.WriteByte(0)
	builder.WriteString(event.FrameID)
	builder.WriteByte(0)
	if event.Payload != nil {
		builder.Write(event.Payload)
	}
	sum := sha256.Sum256(builder.Bytes())
	return hex.EncodeToString(sum[:])
}

func (store *storeEngine) appendEventsLocked(key Key, head *eventHeadRecord, delta []structuralEvent) error {
	dir := store.structuralEventDir(key)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := ""
	if len(head.Shards) > 0 {
		path = filepath.Join(dir, head.Shards[len(head.Shards)-1].Path)
	}
	for len(delta) > 0 {
		if path == "" {
			planned := store.shardRows
			if len(delta) < planned {
				planned = len(delta)
			}
			path = store.structuralEventShardPath(key, delta[0].EventID, delta[planned-1].EventID)
		}
		existing, err := readStructuralEventsAt(path)
		if err != nil {
			return err
		}
		if len(existing) >= store.shardRows {
			path = ""
			continue
		}
		batch := delta
		room := store.shardRows - len(existing)
		if len(batch) > room {
			batch = batch[:room]
		}
		if err := appendStructuralEvents(path, batch); err != nil {
			return err
		}
		all := append(existing, batch...)
		if len(head.Shards) > 0 && filepath.Clean(path) == filepath.Clean(filepath.Join(dir, head.Shards[len(head.Shards)-1].Path)) {
			head.Shards[len(head.Shards)-1] = shardInfo{
				Path: filepath.Base(path), FromSeq: all[0].EventID, ToSeq: all[len(all)-1].EventID,
				Count: len(all), SHA256: fileSHA256(path),
			}
		} else {
			head.Shards = append(head.Shards, shardInfo{
				Path: filepath.Base(path), FromSeq: all[0].EventID, ToSeq: all[len(all)-1].EventID,
				Count: len(all), SHA256: fileSHA256(path),
			})
		}
		head.LastID = all[len(all)-1].EventID
		head.Total += uint64(len(batch))
		delta = delta[len(batch):]
	}
	return nil
}

func readStructuralEventsAt(path string) ([]structuralEvent, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readStructuralEventsFile(file)
}

func appendStructuralEvents(path string, rows []structuralEvent) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := truncateCrashTail(file); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		file.Close()
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

// readEvents 读取 EVENT（[from, to] 含端点；0 = 全量）。
func (store *storeEngine) readEvents(key Key, fromID, toID uint64) ([]structuralEvent, error) {
	store.eventMu.Lock()
	defer store.eventMu.Unlock()
	head, err := store.readEventHeadLocked(key)
	if err != nil {
		return nil, err
	}
	if toID == 0 || toID > head.LastID {
		toID = head.LastID
	}
	if fromID == 0 {
		fromID = 1
	}
	if toID == 0 || fromID > toID {
		return []structuralEvent{}, nil
	}
	var out []structuralEvent
	for _, shard := range head.Shards {
		if shard.ToSeq < fromID || shard.FromSeq > toID {
			continue
		}
		rows, err := readStructuralEventsAt(filepath.Join(store.structuralEventDir(key), shard.Path))
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if row.EventID >= fromID && row.EventID <= toID {
				out = append(out, row)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EventID < out[j].EventID })
	return out, nil
}
