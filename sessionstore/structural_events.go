// v8 EVENT 通道：结构性操作摘要（compacted/fork/subagent/interrupted/
// rolled_back/…）。
//
// 事实模型（my_design §7/附录 A.2）：
//   - EVENT 不参与模型上下文装配（I4）；anchor_message_id = 事件发生在
//     该行之后（从下一事件行起生效，含端点语义的另一面）；
//   - **写形态 = 与 message 同构的 append-only**：一次提交只做「append 数据
//     → 原子替换 event.json（发布点）」，判定写什么只依据 head 水位，
//     **不回读历史分片**（T-EV-05）；
//   - 幂等（A.2 水位式）：event_id=0 → 引擎续号追加；event_id ≤ head 已发布
//     水位 → 跳过；commit_id 等于 head.last_commit_id → 同 commit 重复持久化，
//     整次空操作。幂等窗口 = 紧邻一次发布，需要更大窗口的生产者自带稳定坐标；
//   - 固定写序 message → event；崩溃允许 event 短窗口落后于 message，读者以
//     message/compact 为准（R2-EVENT-1）。
package sessionstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// structuralEventKind 是结构性操作类别（沿用 my_design 附录 A.1 名单）。
type structuralEventKind string

const (
	structuralEventCompacted          structuralEventKind = "compacted"
	structuralEventFork               structuralEventKind = "fork"
	structuralEventSubagent           structuralEventKind = "subagent"
	structuralEventRoleSession        structuralEventKind = "role_session"
	structuralEventInterrupted        structuralEventKind = "interrupted"
	structuralEventRolledBack         structuralEventKind = "rolled_back"
	structuralEventRequestBegin       structuralEventKind = "request_begin"
	structuralEventRequestEnd         structuralEventKind = "request_end"
	structuralEventTurnBegin          structuralEventKind = "turn_begin"
	structuralEventTurnEnd            structuralEventKind = "turn_end"
	structuralEventTokenUsage         structuralEventKind = "token_usage"
	structuralEventArchived           structuralEventKind = "session_archived"
	structuralEventScheduleRegistered structuralEventKind = "schedule.registered"
	structuralEventScheduleCancelled  structuralEventKind = "schedule.cancelled"
	structuralEventScheduleFired      structuralEventKind = "schedule.fired"
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

// eventShardInfo 是 event head 内的分片路由项。Count/Bytes 让写路径在不打开
// 文件的前提下就能定位尾分片剩余容量并判断有无崩溃残尾（message 通道的
// 分片索引仍按 H3 专项处理，这里不复用它那份额外开销）。
type eventShardInfo struct {
	Path   string `json:"path"`
	FromID uint64 `json:"from_id"`
	ToID   uint64 `json:"to_id"`
	Count  int    `json:"count"`
	Bytes  int64  `json:"bytes"`
}

// eventHeadRecord 是 metadata/event.json payload：只有发布点水位与分片路由。
type eventHeadRecord struct {
	SessionID string `json:"session_id"`
	// LastID 是已发布的最大 event_id（空 = 0）。
	LastID uint64 `json:"last_event_id"`
	// LastCommitID 是最近一次发布 EVENT 的 commit_id（A.2 幂等判据）。
	LastCommitID string           `json:"last_commit_id,omitempty"`
	Total        uint64           `json:"total"`
	Shards       []eventShardInfo `json:"shards,omitempty"`
}

// eventStats 是 EVENT 写路径的归因累加器（T-EV-05：写路径读历史分片必须为 0）。
type eventStats struct {
	commits    atomic.Uint64
	appended   atomic.Uint64
	shardReads atomic.Uint64
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
//
// 提交只读 head（水位 + 分片路由），不读任何已发布事件行；append 后 head 未
// 替换 = 未提交，下次提交由 reapEventUnpublishedLocked 截回（与 message 同）。
func (store *storeEngine) structuralEventCommit(key Key, commitID string, events []structuralEvent) (eventHeadRecord, error) {
	store.mu(key, moduleEvent).Lock()
	defer store.mu(key, moduleEvent).Unlock()
	if _, err := store.ensureLayoutGuide(key); err != nil {
		return eventHeadRecord{}, err
	}
	if commitID == "" && len(events) > 0 {
		// S17/D13：按事件身份确定性推导，不用存储层随机号。
		commitID = structuralEventsCommitID(events)
	}
	head, err := store.readEventHeadLocked(key)
	if err != nil {
		return eventHeadRecord{}, err
	}
	repaired, err := store.reapEventUnpublishedLocked(key, &head)
	if err != nil {
		return eventHeadRecord{}, err
	}
	store.event.commits.Add(1)
	delta, err := store.structuralEventDelta(head, events, commitID)
	if err != nil {
		return eventHeadRecord{}, err
	}
	if len(delta) == 0 {
		if !repaired {
			return head, nil
		}
		// 崩溃恢复修了分片索引：把修补后的 head 发布出去，使下一次提交
		// 不必再走重读恢复路径。
		if _, err := store.publishModuleHead(key, moduleEvent, commitID, head, time.Now().UTC()); err != nil {
			return eventHeadRecord{}, err
		}
		return head, nil
	}
	if err := store.appendEventsLocked(key, &head, delta); err != nil {
		return eventHeadRecord{}, err
	}
	head.LastCommitID = commitID
	if _, err := store.publishModuleHead(key, moduleEvent, commitID, head, time.Now().UTC()); err != nil {
		return eventHeadRecord{}, err
	}
	return head, nil
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
	store.mu(key, moduleEvent).Lock()
	defer store.mu(key, moduleEvent).Unlock()
	return store.readEventHeadLocked(key)
}

// structuralEventsCommitID 由结构性事件身份字段确定性推导一次提交的凭据。
func structuralEventsCommitID(events []structuralEvent) string {
	identity := make([]string, 0, len(events))
	for _, event := range events {
		identity = append(identity, string(event.Kind)+"|"+event.AnchorMessageID+
			"|"+event.FrameID+"|"+string(event.Payload))
	}
	return "ev-" + hash(strings.Join(identity, "\n"))
}

// reapEventUnpublishedLocked 清理未发布残迹（head 未索引的分片删除；尾分片
// 超出水位的行截掉）。常态判定 = 文件字节数与 head 记录一致 → 零重读；只有
// 字节数不等（崩溃残尾或未发布行）才读一次该分片。返回是否修补过分片索引。
func (store *storeEngine) reapEventUnpublishedLocked(key Key, head *eventHeadRecord) (bool, error) {
	dir := store.structuralEventDir(key)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	indexed := make(map[string]bool, len(head.Shards))
	for _, shard := range head.Shards {
		indexed[filepath.Clean(filepath.Join(dir, shard.Path))] = true
	}
	repaired := false
	for _, entry := range entries {
		if entry.IsDir() || !isEventShardFile(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if !indexed[filepath.Clean(path)] {
			if err := os.Remove(path); err != nil {
				return repaired, err
			}
		}
	}
	if len(head.Shards) == 0 {
		return repaired, nil
	}
	lastIndex := len(head.Shards) - 1
	last := head.Shards[lastIndex]
	path := filepath.Join(dir, last.Path)
	size, err := eventShardSize(path)
	if err != nil {
		return repaired, err
	}
	if size == last.Bytes {
		return repaired, nil
	}
	store.event.shardReads.Add(1)
	rows, size, err := truncateStructuralEventsAfter(path, head.LastID)
	if err != nil {
		return repaired, err
	}
	if len(rows) == 0 {
		return repaired, fmt.Errorf("session storage: event shard %s has no published rows below head %d", last.Path, head.LastID)
	}
	if rows[len(rows)-1].EventID != head.LastID {
		return repaired, fmt.Errorf("session storage: event shard %s last id %d != head %d",
			last.Path, rows[len(rows)-1].EventID, head.LastID)
	}
	head.Shards[lastIndex] = eventShardInfo{
		Path: last.Path, FromID: rows[0].EventID, ToID: head.LastID,
		Count: len(rows), Bytes: size,
	}
	head.Total = eventPublishedTotal(head.Shards)
	return true, nil
}

// eventPublishedTotal 按分片索引重算已发布事件行数。
func eventPublishedTotal(shards []eventShardInfo) uint64 {
	var total uint64
	for _, shard := range shards {
		total += uint64(shard.Count)
	}
	return total
}

func eventShardSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, fmt.Errorf("session storage: event shard %s missing", filepath.Base(path))
	}
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func isEventShardFile(name string) bool {
	var fromID, toID uint64
	if _, err := fmt.Sscanf(name, "event_%d_%d.jsonl", &fromID, &toID); err != nil {
		return false
	}
	return fromID > 0 && toID >= fromID
}

// truncateStructuralEventsAfter 截掉崩溃残尾与 event_id > headID 的行，返回
// 保留下来的行与截断后的文件字节数（调用方据此回填 head 分片索引）。
func truncateStructuralEventsAfter(path string, headID uint64) ([]structuralEvent, int64, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	if err := truncateCrashTail(file); err != nil {
		return nil, 0, err
	}
	rows, err := readStructuralEventsFile(file)
	if err != nil {
		return nil, 0, err
	}
	if len(rows) > 0 && rows[len(rows)-1].EventID > headID {
		keep := 0
		for index, row := range rows {
			if row.EventID > headID {
				break
			}
			keep = index + 1
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, 0, err
		}
		segments := bytes.Split(data, []byte{'\n'})
		end := 0
		for index := 0; index < keep && index < len(segments); index++ {
			end += len(segments[index]) + 1
		}
		if err := file.Truncate(int64(end)); err != nil {
			return nil, 0, err
		}
		rows = rows[:keep]
	}
	info, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}
	return rows, info.Size(), nil
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

// structuralEventDelta 按 A.2 水位式幂等计算待追加行：同 commit 重复持久化 =
// 整次空操作；已发布水位内的显式 event_id 跳过；event_id=0 由引擎续号。
// 判定只依据 head，不读任何已发布事件行。
func (store *storeEngine) structuralEventDelta(head eventHeadRecord, events []structuralEvent, commitID string) ([]structuralEvent, error) {
	if len(events) == 0 {
		return nil, nil
	}
	if head.LastID > 0 && head.LastCommitID == commitID {
		return nil, nil
	}
	next := head.LastID
	delta := make([]structuralEvent, 0, len(events))
	for _, row := range events {
		row.CommitID = commitID
		switch {
		case row.EventID == 0:
			next++
			row.EventID = next
		case row.EventID <= head.LastID:
			continue
		case row.EventID <= next:
			return nil, fmt.Errorf("session storage: event ids must be strictly increasing (id %d)", row.EventID)
		default:
			next = row.EventID
		}
		delta = append(delta, row)
	}
	return delta, nil
}

// appendEventsLocked 把 delta 追加进尾分片（满片滚动新文件），并按内存中的
// 分片索引维护 head。调用方持 eventMu；head 发布由调用方执行。
func (store *storeEngine) appendEventsLocked(key Key, head *eventHeadRecord, delta []structuralEvent) error {
	dir := store.structuralEventDir(key)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	shardRows := store.settings.shardRows()
	for len(delta) > 0 {
		lastIndex := len(head.Shards) - 1
		reuse := lastIndex >= 0 && head.Shards[lastIndex].Count < shardRows
		info := eventShardInfo{}
		path := ""
		if reuse {
			info = head.Shards[lastIndex]
			path = filepath.Join(dir, info.Path)
		} else {
			planned := min(shardRows, len(delta))
			info = eventShardInfo{FromID: delta[0].EventID, ToID: delta[planned-1].EventID}
			path = store.structuralEventShardPath(key, info.FromID, info.ToID)
		}
		batch := delta
		if room := shardRows - info.Count; len(batch) > room {
			batch = batch[:room]
		}
		size, err := appendStructuralEvents(path, batch)
		if err != nil {
			return err
		}
		info.Path = filepath.Base(path)
		info.Count += len(batch)
		info.ToID = batch[len(batch)-1].EventID
		info.Bytes = size
		if reuse {
			head.Shards[lastIndex] = info
		} else {
			head.Shards = append(head.Shards, info)
		}
		head.LastID = info.ToID
		head.Total += uint64(len(batch))
		store.event.appended.Add(uint64(len(batch)))
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

// appendStructuralEvents 追加完整 JSONL 行（先截崩溃残尾；再 sync），返回
// 追加后的文件字节数（供 head 记录，写路径据此免重读）。
func appendStructuralEvents(path string, rows []structuralEvent) (int64, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return 0, err
	}
	if err := truncateCrashTail(file); err != nil {
		file.Close()
		return 0, err
	}
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		file.Close()
		return 0, err
	}
	buffer := make([]byte, 0, 4096)
	for _, row := range rows {
		data, err := json.Marshal(row)
		if err != nil {
			file.Close()
			return 0, err
		}
		buffer = append(buffer, data...)
		buffer = append(buffer, '\n')
	}
	if _, err := file.Write(buffer); err != nil {
		file.Close()
		return 0, err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return 0, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return 0, err
	}
	size := info.Size()
	if err := file.Close(); err != nil {
		return 0, err
	}
	return size, nil
}

// readEvents 读取 EVENT（[from, to] 含端点；0 = 全量）。
func (store *storeEngine) readEvents(key Key, fromID, toID uint64) ([]structuralEvent, error) {
	store.mu(key, moduleEvent).Lock()
	defer store.mu(key, moduleEvent).Unlock()
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
		if shard.ToID < fromID || shard.FromID > toID {
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

// verifyEvents 校验 EVENT 通道：head 与分片索引、分片物理行数、水位一致性。
func (store *storeEngine) verifyEvents(key Key) error {
	store.mu(key, moduleEvent).Lock()
	defer store.mu(key, moduleEvent).Unlock()
	head, err := store.readEventHeadLocked(key)
	if err != nil {
		return err
	}
	shardRows := store.settings.shardRows()
	var total uint64
	var previousID uint64
	for _, shard := range head.Shards {
		if shard.FromID <= previousID || shard.ToID < shard.FromID {
			return fmt.Errorf("session storage: verify event shard %s id range [%d,%d] not after %d",
				shard.Path, shard.FromID, shard.ToID, previousID)
		}
		rows, err := readStructuralEventsAt(filepath.Join(store.structuralEventDir(key), shard.Path))
		if err != nil {
			return fmt.Errorf("session storage: verify event shard %s: %w", shard.Path, err)
		}
		if len(rows) != shard.Count {
			return fmt.Errorf("session storage: verify event shard %s rows=%d want %d", shard.Path, len(rows), shard.Count)
		}
		if len(rows) > 0 && (rows[0].EventID != shard.FromID || rows[len(rows)-1].EventID != shard.ToID) {
			return fmt.Errorf("session storage: verify event shard %s id range [%d,%d] != [%d,%d]",
				shard.Path, rows[0].EventID, rows[len(rows)-1].EventID, shard.FromID, shard.ToID)
		}
		if len(rows) > shardRows {
			return fmt.Errorf("session storage: verify event shard %s rows=%d over shard_rows=%d", shard.Path, len(rows), shardRows)
		}
		total += uint64(len(rows))
		previousID = shard.ToID
	}
	if total != head.Total {
		return fmt.Errorf("session storage: verify event total=%d want %d", total, head.Total)
	}
	// 水位 = 已发布最大 id；显式 event_id 允许留下空洞，因此行数可以少于水位。
	if len(head.Shards) > 0 && head.LastID != head.Shards[len(head.Shards)-1].ToID {
		return fmt.Errorf("session storage: verify event last_id=%d want %d",
			head.LastID, head.Shards[len(head.Shards)-1].ToID)
	}
	if len(head.Shards) == 0 && head.LastID != 0 {
		return fmt.Errorf("session storage: verify event last_id=%d with no shards", head.LastID)
	}
	return nil
}
