// v8 message 事件行存储（M1）。
//
// 事实模型（my_design §4）：
//   - message 条目 = 事件行（一行 = 一个事件行键 message_id + 会话内单调
//     seq）；分片按事件行数（默认 100 行/片）；
//   - message 区间 [message_from, message_to] 含端点；
//   - commit_id = 一次持久提交，同提交内多行共享；
//   - 提交语义 = append 数据 → 原子替换 message.json（head 是发布点）；
//     崩溃残尾（未换行收尾的半行）跳过；append 完成但 head 未发布的行
//     视为未提交，下次提交先截断再续写；
//   - 重复同一 commit_id 幂等：行不重复、head 不双跳（T-M1-08）；
//   - 正常运行期 message 只 append；重写只发生在用户确认的 LRU 行删除
//     （I1/I2，见 v8_retention.go）。
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

	frameworkStorage "github.com/RedHuang-0622/Seele/seelectx/storage"
)

// v8MessageHead 是 metadata/message.json 的 payload：head/水位 + 分片索引。
// head.LastSeq = 已发布最大 seq（发布点）；WatermarkSeq 由 LRU 淘汰前移
// （message_id/seq 保持空洞、不重编号，I2）。
type v8MessageHead struct {
	SessionID string `json:"session_id"`
	// LastSeq 是已发布的最大事件行 seq（空会话 = 0）。
	LastSeq uint64 `json:"last_seq"`
	// LastMessageID 是最后一行非空 message_id（可能为空：流式中间行）。
	LastMessageID string `json:"last_message_id,omitempty"`
	// Shards 是有序分片索引（FromSeq 升序）；LRU 删除后保留空洞。
	Shards []v8ShardInfo `json:"shards,omitempty"`
	// TotalRows 是当前物理存在（未淘汰）的已发布行数。
	TotalRows uint64 `json:"total_rows"`
	// WatermarkSeq / WatermarkMessageID 是 LRU 淘汰水位：只允许删除水位
	// 之前的连续前缀；锚 ≤ watermark 视为已淘汰区引用，不算损坏。
	WatermarkSeq       uint64 `json:"watermark_seq,omitempty"`
	WatermarkMessageID string `json:"watermark_message_id,omitempty"`
	// Meta 是目录枚举面（与 legacy manifest.Meta 同语义：UpdatedAt /
	// TokenCount / ShardCount）。TokenCount 按已发布事件行累计。
	Meta frameworkStorage.SessionMeta `json:"meta,omitempty"`
}

// v8ShardInfo 是一个 message 分片文件的索引项。
type v8ShardInfo struct {
	Path    string `json:"path"`
	FromSeq uint64 `json:"from_seq"`
	ToSeq   uint64 `json:"to_seq"`
	Count   int    `json:"count"`
	SHA256  string `json:"sha256,omitempty"`
}

// v8MessageDir 返回 message 数据目录。
func (store *v8Store) v8MessageDir(key Key) string {
	return filepath.Join(store.v8SessionRoot(key), "message")
}

// v8ShardPath 返回 message 分片文件路径（显式命名：from_to 含端点）。
func (store *v8Store) v8ShardPath(key Key, fromSeq, toSeq uint64) string {
	return filepath.Join(store.v8MessageDir(key), fmt.Sprintf("message_%d_%d.jsonl", fromSeq, toSeq))
}

// v8EmptyMessageHead 返回空会话 head。
func v8EmptyMessageHead(key Key) v8MessageHead {
	return v8MessageHead{SessionID: key.SessionID}
}

// v8ReadMessageHead 读取 message head（缺失 = 空会话 head，不报错）。
func (store *v8Store) v8ReadMessageHead(key Key) (v8MessageHead, error) {
	headFile, err := store.readV8ModuleHeadFile(key, v8ModuleMessage)
	if errors.Is(err, fs.ErrNotExist) {
		return v8EmptyMessageHead(key), nil
	}
	if err != nil {
		return v8MessageHead{}, err
	}
	return decodeV8HeadPayload[v8MessageHead](headFile)
}

// v8MessageCommit 把一提交（可含多行事件行）append 到 message 通道并原子
// 发布 message.json。rows 可为空（空 commit 只确保布局/索引存在）。
//
// seq 规则：行 Seq=0 由引擎按 head 续号；显式 Seq 必须严格递增且 > head
// （≤ head 且 commit_id 相同的重复提交为幂等空操作）。同 commit_id 重试不
// 产生重复行、head 不双跳。
func (store *v8Store) v8MessageCommit(key Key, commitID string, rows []Event) (v8MessageHead, error) {
	store.messageMu.Lock()
	defer store.messageMu.Unlock()
	return store.v8MessageCommitLocked(key, commitID, rows)
}

// v8MessageCommitLocked 是 v8MessageCommit 的锁内实现（其它模块/重放逻辑
// 复用时须自行持 messageMu）。
func (store *v8Store) v8MessageCommitLocked(key Key, commitID string, rows []Event) (v8MessageHead, error) {
	freshSession := !store.v8SessionExists(key)
	if _, err := store.ensureV8Guide(key); err != nil {
		return v8MessageHead{}, err
	}
	if commitID == "" {
		commitID = randomID()
	}
	head, err := store.v8ReadMessageHeadLocked(key)
	if err != nil {
		return v8MessageHead{}, err
	}
	// 崩溃恢复：删除 head 之外的分片与 head 尾分片内超过 head 的行
	// （append 完成但 head 未发布 = 未提交，T-M1-03/04）。
	if err := store.v8ReapUnpublishedLocked(key, head); err != nil {
		return v8MessageHead{}, err
	}
	delta, err := store.v8DeltaRowsLocked(head, rows, commitID)
	if err != nil {
		return v8MessageHead{}, err
	}
	if len(delta) == 0 {
		if freshSession {
			// 空 commit（EnsureIndexed/record-only 首写）：仍发布空 head，
			// 使会话可被 List/枚举发现。
			now := time.Now().UTC()
			if _, err := store.publishV8ModuleHead(key, v8ModuleMessage, commitID, head, now); err != nil {
				return v8MessageHead{}, err
			}
			return head, store.registerV8Module(key, v8ModuleMessage, store.v8ModulePath(key, v8ModuleMessage))
		}
		return head, nil
	}
	if err := store.v8AppendRowsLocked(key, &head, delta); err != nil {
		return v8MessageHead{}, err
	}
	now := time.Now().UTC()
	tokens := 0
	for _, row := range delta {
		tokens += row.TokenCount
	}
	head.Meta.TokenCount += tokens
	head.Meta.ShardCount = len(head.Shards)
	head.Meta.UpdatedAt = now
	if head.Meta.SessionID == "" {
		head.Meta.SessionID = key.SessionID
		head.Meta.CreatedAt = now
	}
	if _, err := store.publishV8ModuleHead(key, v8ModuleMessage, commitID, head, now); err != nil {
		return v8MessageHead{}, err
	}
	if err := store.registerV8Module(key, v8ModuleMessage, store.v8ModulePath(key, v8ModuleMessage)); err != nil {
		// guide 注册失败不阻断已发布 head（读路径以模块 head 为准，I9）。
		_ = err
	}
	return head, nil
}

// v8ReadMessageHeadLocked 是 v8ReadMessageHead 的锁内版本。
func (store *v8Store) v8ReadMessageHeadLocked(key Key) (v8MessageHead, error) {
	headFile, err := store.readV8ModuleHeadFile(key, v8ModuleMessage)
	if errors.Is(err, fs.ErrNotExist) {
		return v8EmptyMessageHead(key), nil
	}
	if err != nil {
		return v8MessageHead{}, err
	}
	return decodeV8HeadPayload[v8MessageHead](headFile)
}

// v8ReapUnpublishedLocked 清理未发布残迹：
//  1. 删除 head 未索引的分片文件；
//  2. 把 head 最后一个分片截断到 head.LastSeq（超出的完整行也是未提交
//     行——append 后 head 替换前崩溃的恢复语义：head 不前进、行不可见，
//     下次提交前清掉，避免与重试行重复）。
func (store *v8Store) v8ReapUnpublishedLocked(key Key, head v8MessageHead) error {
	dir := store.v8MessageDir(key)
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
		if entry.IsDir() || !isV8MessageShardName(entry.Name()) {
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
	path := filepath.Join(dir, last.Path)
	if err := truncateV8RowsAfter(path, head.LastSeq); err != nil {
		return err
	}
	return nil
}

func isV8MessageShardName(name string) bool {
	var fromSeq, toSeq uint64
	if _, err := fmt.Sscanf(name, "message_%d_%d.jsonl", &fromSeq, &toSeq); err != nil {
		return false
	}
	return fromSeq > 0 && toSeq >= fromSeq
}

// truncateV8RowsAfter 截断 JSONL 文件，保留 seq <= head 的行（先跳过崩溃
// 残尾半行，再按行内 seq 从文件尾向前删除）。
func truncateV8RowsAfter(path string, headSeq uint64) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	if err := truncateRolloutCrashTail(file); err != nil {
		return err
	}
	lines, err := readV8RowsFile(file)
	if err != nil {
		return err
	}
	if len(lines) == 0 || lines[len(lines)-1].Seq <= headSeq {
		return nil
	}
	keep := 0
	for index, row := range lines {
		if row.Seq > headSeq {
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

// readV8RowsFile 读取已打开 JSONL 文件中的全部事件行（跳过空行与崩溃残尾）。
func readV8RowsFile(file *os.File) ([]Event, error) {
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
	return decodeV8Rows(data), nil
}

// decodeV8Rows 解析 JSONL 事件行（崩溃残尾跳过；坏行显式失败由调用方
// 处理前先经 verify；这里返回可解析行）。
func decodeV8Rows(data []byte) []Event {
	segments := bytes.Split(data, []byte{'\n'})
	rows := make([]Event, 0, len(segments))
	for index, segment := range segments {
		if index == len(segments)-1 && len(bytes.TrimSpace(segment)) > 0 {
			continue // 崩溃残尾（未换行收尾）
		}
		segment = bytes.TrimSpace(segment)
		if len(segment) == 0 {
			continue
		}
		var row Event
		if json.Unmarshal(segment, &row) != nil {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

// v8DeltaRowsLocked 计算应 append 的行：Seq=0 → 引擎续号；显式 Seq 必须
// 严格递增且 > head.LastSeq。返回行均已打上 commit_id。
func (store *v8Store) v8DeltaRowsLocked(head v8MessageHead, rows []Event, commitID string) ([]Event, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	next := head.LastSeq
	delta := make([]Event, 0, len(rows))
	for _, row := range rows {
		if row.Seq == 0 {
			next++
			row.Seq = next
		} else {
			if row.Seq <= head.LastSeq {
				// 重复提交（head 已包含该行）= 幂等空操作，跳过。
				continue
			}
			if row.Seq <= next {
				return nil, fmt.Errorf("v8: message rows must be strictly increasing (seq %d)", row.Seq)
			}
			next = row.Seq
		}
		row.CommitID = commitID
		delta = append(delta, row)
	}
	return delta, nil
}

// v8AppendRowsLocked 把 delta append 进当前分片（满片滚动到新文件），并
// 更新 head（分片索引/计数/水位）。调用方持 messageMu；head 的发布由调用
// 方在返回后执行。
func (store *v8Store) v8AppendRowsLocked(key Key, head *v8MessageHead, delta []Event) error {
	dir := store.v8MessageDir(key)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := ""
	if len(head.Shards) > 0 {
		last := head.Shards[len(head.Shards)-1]
		path = filepath.Join(dir, last.Path)
	}
	for len(delta) > 0 {
		if path == "" {
			// 新分片名以实际写入的首/末 seq 命名（含端点）。
			planned := store.shardRows
			if len(delta) < planned {
				planned = len(delta)
			}
			path = store.v8ShardPath(key, delta[0].Seq, delta[planned-1].Seq)
		}
		existing, err := readV8RowsFileAt(path)
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
		if err := appendV8RowsFile(path, batch); err != nil {
			return err
		}
		all := append(existing, batch...)
		shardPath := filepath.Base(path)
		if len(head.Shards) > 0 && filepath.Clean(path) == filepath.Clean(filepath.Join(dir, head.Shards[len(head.Shards)-1].Path)) {
			head.Shards[len(head.Shards)-1] = v8ShardInfo{
				Path:    shardPath,
				FromSeq: all[0].Seq,
				ToSeq:   all[len(all)-1].Seq,
				Count:   len(all),
				SHA256:  v8FileSHA256(path),
			}
		} else {
			head.Shards = append(head.Shards, v8ShardInfo{
				Path:    shardPath,
				FromSeq: all[0].Seq,
				ToSeq:   all[len(all)-1].Seq,
				Count:   len(all),
				SHA256:  v8FileSHA256(path),
			})
		}
		head.TotalRows += uint64(len(batch))
		head.LastSeq = all[len(all)-1].Seq
		if last := all[len(all)-1]; last.MessageID != "" {
			head.LastMessageID = last.MessageID
		}
		delta = delta[len(batch):]
	}
	return nil
}

func readV8RowsFileAt(path string) ([]Event, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readV8RowsFile(file)
}

// appendV8RowsFile 追加完整 JSONL 行（先截崩溃残尾；再 sync）。
func appendV8RowsFile(path string, rows []Event) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := truncateRolloutCrashTail(file); err != nil {
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
			return fmt.Errorf("v8: encode message row: %w", err)
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

func v8FileSHA256(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// v8ReadRows 按 seq 区间读取已发布行（[from, to] 含端点；越界安全）。
// from == 0 表示从首行开始；to == 0 表示到 head 末尾。
func (store *v8Store) v8ReadRows(key Key, fromSeq, toSeq uint64) ([]Event, error) {
	store.messageMu.Lock()
	defer store.messageMu.Unlock()
	return store.v8ReadRowsLocked(key, fromSeq, toSeq)
}

func (store *v8Store) v8ReadRowsLocked(key Key, fromSeq, toSeq uint64) ([]Event, error) {
	head, err := store.v8ReadMessageHeadLocked(key)
	if err != nil {
		return nil, err
	}
	if toSeq != 0 && fromSeq > toSeq {
		return nil, errors.New("session storage: invalid event range")
	}
	if toSeq == 0 || toSeq > head.LastSeq {
		toSeq = head.LastSeq
	}
	if fromSeq == 0 {
		fromSeq = 1
	}
	if toSeq == 0 || fromSeq > toSeq {
		return []Event{}, nil
	}
	var out []Event
	for _, shard := range head.Shards {
		if shard.ToSeq < fromSeq || shard.FromSeq > toSeq {
			continue
		}
		rows, err := readV8RowsFileAt(filepath.Join(store.v8MessageDir(key), shard.Path))
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if row.Seq >= fromSeq && row.Seq <= toSeq {
				out = append(out, row)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out, nil
}

// v8ReadAllRows 读取全部已发布行（含 LRU 空洞；已淘汰前缀自然缺失）。
func (store *v8Store) v8ReadAllRows(key Key) ([]Event, error) {
	return store.v8ReadRows(key, 0, 0)
}

// v8VerifyMessage 校验 message 通道：head 可读、分片存在、文件行数与 head
// 一致、head 末行 = 最后已发布行；LRU 空洞（锚 ≤ watermark）不算损坏。
func (store *v8Store) v8VerifyMessage(key Key) error {
	store.messageMu.Lock()
	defer store.messageMu.Unlock()
	head, err := store.v8ReadMessageHeadLocked(key)
	if err != nil {
		return err
	}
	total := uint64(0)
	for _, shard := range head.Shards {
		rows, err := readV8RowsFileAt(filepath.Join(store.v8MessageDir(key), shard.Path))
		if err != nil {
			return fmt.Errorf("v8: verify shard %s: %w", shard.Path, err)
		}
		if len(rows) != shard.Count {
			return fmt.Errorf("v8: verify shard %s rows=%d want %d", shard.Path, len(rows), shard.Count)
		}
		if len(rows) > 0 {
			if rows[0].Seq != shard.FromSeq || rows[len(rows)-1].Seq != shard.ToSeq {
				return fmt.Errorf("v8: verify shard %s seq range [%d,%d] != [%d,%d]",
					shard.Path, rows[0].Seq, rows[len(rows)-1].Seq, shard.FromSeq, shard.ToSeq)
			}
			if rows[len(rows)-1].Seq > head.LastSeq {
				return fmt.Errorf("v8: verify shard %s beyond head last_seq=%d", shard.Path, head.LastSeq)
			}
		}
		total += uint64(len(rows))
	}
	if total != head.TotalRows {
		return fmt.Errorf("v8: verify message total=%d want %d", total, head.TotalRows)
	}
	return nil
}

// v8MessageCount 返回当前物理行数（淘汰后不包含前缀空洞）。
func (store *v8Store) v8MessageCount(key Key) (uint64, error) {
	store.messageMu.Lock()
	defer store.messageMu.Unlock()
	head, err := store.v8ReadMessageHeadLocked(key)
	if err != nil {
		return 0, err
	}
	return head.TotalRows, nil
}
