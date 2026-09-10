// message 事件行存储（M1）。
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
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	frameworkStorage "github.com/RedHuang-0622/Seele/seelectx/storage"
)

// messageHead 是 metadata/message.json 的 payload：head/水位 + 分片索引。
// head.LastSeq = 已发布最大 seq（发布点）；WatermarkSeq 由 LRU 淘汰前移
// （message_id/seq 保持空洞、不重编号，I2）。
type messageHead struct {
	SessionID string `json:"session_id"`
	// LastSeq 是已发布的最大事件行 seq（空会话 = 0）。
	LastSeq uint64 `json:"last_seq"`
	// LastMessageID 是最后一行非空 message_id（可能为空：流式中间行）。
	LastMessageID string `json:"last_message_id,omitempty"`
	// Shards 是有序分片索引（FromSeq 升序）；LRU 删除后保留空洞。
	Shards []shardInfo `json:"shards,omitempty"`
	// TotalRows 是当前物理存在（未淘汰）的已发布行数。
	TotalRows uint64 `json:"total_rows"`
	// WatermarkSeq / WatermarkMessageID 是 LRU 淘汰水位：只允许删除水位
	// 之前的连续前缀；锚 ≤ watermark 视为已淘汰区引用，不算损坏。
	WatermarkSeq       uint64 `json:"watermark_seq,omitempty"`
	WatermarkMessageID string `json:"watermark_message_id,omitempty"`
	// LastCommitID 是最近一次已发布提交的凭据（§2.0 规则 4 / S17）。
	LastCommitID string `json:"last_commit_id,omitempty"`
	// Meta 是目录枚举面（与 legacy manifest.Meta 同语义：UpdatedAt /
	// TokenCount / ShardCount）。TokenCount 按已发布事件行累计。
	Meta frameworkStorage.SessionMeta `json:"meta,omitempty"`
}

// shardInfo 是一个 message 分片文件的索引项。
type shardInfo struct {
	Path    string `json:"path"`
	FromSeq uint64 `json:"from_seq"`
	ToSeq   uint64 `json:"to_seq"`
	Count   int    `json:"count"`
	SHA256  string `json:"sha256,omitempty"`
}

// messageDir 返回 message 数据目录。
func (store *storeEngine) messageDir(key Key) string {
	return filepath.Join(store.sessionRoot(key), "message")
}

// shardPath 返回 message 分片文件路径（显式命名：from_to 含端点）。
func (store *storeEngine) shardPath(key Key, fromSeq, toSeq uint64) string {
	return filepath.Join(store.messageDir(key), fmt.Sprintf("message_%d_%d.jsonl", fromSeq, toSeq))
}

// emptyMessageHead 返回空会话 head。
func emptyMessageHead(key Key) messageHead {
	return messageHead{SessionID: key.SessionID}
}

// readMessageHead 读取 message head（缺失 = 空会话 head，不报错）。
func (store *storeEngine) readMessageHead(key Key) (messageHead, error) {
	headFile, err := store.readModuleHeadFile(key, moduleMessage)
	if errors.Is(err, fs.ErrNotExist) {
		return emptyMessageHead(key), nil
	}
	if err != nil {
		return messageHead{}, err
	}
	return decodeHeadPayload[messageHead](headFile)
}

// messageCommit 把一提交（可含多行事件行）append 到 message 通道并原子
// 发布 message.json。rows 可为空（空 commit 只确保布局/索引存在）。
//
// seq 规则：行 Seq=0 由引擎按 head 续号；显式 Seq 必须严格递增且 > head
// （≤ head 且 commit_id 相同的重复提交为幂等空操作）。同 commit_id 重试不
// 产生重复行、head 不双跳。
func (store *storeEngine) messageCommit(key Key, commitID string, rows []Event) (messageHead, error) {
	store.mu(key, moduleMessage).Lock()
	defer store.mu(key, moduleMessage).Unlock()
	return store.messageCommitLocked(key, commitID, rows)
}

// messageCommitLocked 是 messageCommit 的锁内实现（其它模块/重放逻辑
// 复用时须自行持 messageMu）。
func (store *storeEngine) messageCommitLocked(key Key, commitID string, rows []Event) (messageHead, error) {
	freshSession := !store.sessionExists(key)
	if _, err := store.ensureLayoutGuide(key); err != nil {
		return messageHead{}, err
	}
	if commitID == "" && len(rows) > 0 {
		// S17/D13：存储层不得现造随机凭据；无显式凭据时按操作内容确定性
		// 推导（重放同一操作得到同一值）。
		commitID = messageRowsCommitID(rows)
	}
	head, err := store.readMessageHeadLocked(key)
	if err != nil {
		return messageHead{}, err
	}
	// 崩溃恢复：删除 head 之外的分片与 head 尾分片内超过 head 的行
	// （append 完成但 head 未发布 = 未提交，T-M1-03/04）。
	if err := store.reapUnpublishedLocked(key, head); err != nil {
		return messageHead{}, err
	}
	delta, err := store.deltaRowsLocked(head, rows, commitID)
	if err != nil {
		return messageHead{}, err
	}
	if len(delta) == 0 {
		if freshSession {
			// 空 commit（EnsureIndexed/record-only 首写）：仍发布空 head，
			// 使会话可被 List/枚举发现。
			now := time.Now().UTC()
			head.LastCommitID = commitID
			if _, err := store.publishModuleHead(key, moduleMessage, commitID, head, now); err != nil {
				return messageHead{}, err
			}
			store.rememberMessageAnchor(key, head)
			return head, nil
		}
		store.rememberMessageAnchor(key, head)
		return head, nil
	}
	if err := store.appendRowsLocked(key, &head, delta); err != nil {
		return messageHead{}, err
	}
	now := time.Now().UTC()
	head.LastCommitID = commitID
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
	if _, err := store.publishModuleHead(key, moduleMessage, commitID, head, now); err != nil {
		return messageHead{}, err
	}
	// 发布锚坐标：栈通道取锚因此不必打开 metadata/message.json（跨模块
	// 「读者持柄 → rename 发布失败」窗口）。
	store.rememberMessageAnchor(key, head)
	return head, nil
}

// messageRowsCommitID 由事件行身份字段确定性推导一次提交的凭据。
func messageRowsCommitID(rows []Event) string {
	identity := make([]string, 0, len(rows))
	for _, row := range rows {
		identity = append(identity, string(row.Kind)+"|"+row.Role+"|"+row.MessageID+"|"+
			row.ToolCallID+"|"+row.Name+"|"+row.Content+"|"+row.ResultRef)
	}
	return "msg-" + hash(strings.Join(identity, "\n"))
}

// messageAnchorPoint 是 message 通道最近一次发布的坐标（内存锚）。
type messageAnchorPoint struct {
	messageID string
	seq       uint64
}

// rememberMessageAnchor 在 message head 发布后盖章内存锚。调用方持 message
// 模块锁（或刚完成发布），读者只做一次 atomic load。
func (store *storeEngine) rememberMessageAnchor(key Key, head messageHead) {
	store.locks(key).anchor.Store(&messageAnchorPoint{
		messageID: head.LastMessageID, seq: head.LastSeq,
	})
}

// messageAnchorCached 返回内存锚；未装载时返回 nil。
func (store *storeEngine) messageAnchorCached(key Key) *messageAnchorPoint {
	return store.locks(key).anchor.Load()
}

// forgetMessageAnchor 作废内存锚（删除会话 / 换后端）。
func (store *storeEngine) forgetMessageAnchor(key Key) {
	store.locks(key).anchor.Store(nil)
}

// readMessageHeadLocked 是 readMessageHead 的锁内版本。
func (store *storeEngine) readMessageHeadLocked(key Key) (messageHead, error) {
	headFile, err := store.readModuleHeadFile(key, moduleMessage)
	if errors.Is(err, fs.ErrNotExist) {
		return emptyMessageHead(key), nil
	}
	if err != nil {
		return messageHead{}, err
	}
	return decodeHeadPayload[messageHead](headFile)
}

// reapUnpublishedLocked 清理未发布残迹：
//  1. 删除 head 未索引的分片文件；
//  2. 把 head 最后一个分片截断到 head.LastSeq（超出的完整行也是未提交
//     行——append 后 head 替换前崩溃的恢复语义：head 不前进、行不可见，
//     下次提交前清掉，避免与重试行重复）。
func (store *storeEngine) reapUnpublishedLocked(key Key, head messageHead) error {
	dir := store.messageDir(key)
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
		if entry.IsDir() || !isMessageShardFile(entry.Name()) {
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
	if err := truncateMessageRowsAfter(path, head.LastSeq); err != nil {
		return err
	}
	return nil
}

func isMessageShardFile(name string) bool {
	var fromSeq, toSeq uint64
	if _, err := fmt.Sscanf(name, "message_%d_%d.jsonl", &fromSeq, &toSeq); err != nil {
		return false
	}
	return fromSeq > 0 && toSeq >= fromSeq
}

// truncateMessageRowsAfter 截断 JSONL 文件，保留 seq <= head 的行（先跳过崩溃
// 残尾半行，再按行内 seq 从文件尾向前删除）。
func truncateMessageRowsAfter(path string, headSeq uint64) error {
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
	lines, err := readMessageRowsFile(file)
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

// readMessageRowsFile 读取已打开 JSONL 文件中的全部事件行（跳过空行与崩溃残尾）。
func readMessageRowsFile(file *os.File) ([]Event, error) {
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
	return decodeMessageRows(data), nil
}

// decodeMessageRows 解析 JSONL 事件行（崩溃残尾跳过；坏行显式失败由调用方
// 处理前先经 verify；这里返回可解析行）。
func decodeMessageRows(data []byte) []Event {
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

// deltaRowsLocked 计算应 append 的行：Seq=0 → 引擎续号；显式 Seq 必须
// 严格递增且 > head.LastSeq。返回行均已打上 commit_id。
func (store *storeEngine) deltaRowsLocked(head messageHead, rows []Event, commitID string) ([]Event, error) {
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
				return nil, fmt.Errorf("session storage: message rows must be strictly increasing (seq %d)", row.Seq)
			}
			next = row.Seq
		}
		row.CommitID = commitID
		delta = append(delta, row)
	}
	return delta, nil
}

// appendRowsLocked 把 delta append 进当前分片（满片滚动到新文件），并
// 更新 head（分片索引/计数/水位）。调用方持 messageMu；head 的发布由调用
// 方在返回后执行。
func (store *storeEngine) appendRowsLocked(key Key, head *messageHead, delta []Event) error {
	dir := store.messageDir(key)
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
			planned := store.settings.shardRows()
			if len(delta) < planned {
				planned = len(delta)
			}
			path = store.shardPath(key, delta[0].Seq, delta[planned-1].Seq)
		}
		existing, err := readMessageRowsFileAt(path)
		if err != nil {
			return err
		}
		if len(existing) >= store.settings.shardRows() {
			path = ""
			continue
		}
		batch := delta
		room := store.settings.shardRows() - len(existing)
		if len(batch) > room {
			batch = batch[:room]
		}
		if err := appendMessageRowsFile(path, batch); err != nil {
			return err
		}
		all := append(existing, batch...)
		shardPath := filepath.Base(path)
		if len(head.Shards) > 0 && filepath.Clean(path) == filepath.Clean(filepath.Join(dir, head.Shards[len(head.Shards)-1].Path)) {
			head.Shards[len(head.Shards)-1] = shardInfo{
				Path:    shardPath,
				FromSeq: all[0].Seq,
				ToSeq:   all[len(all)-1].Seq,
				Count:   len(all),
				SHA256:  fileSHA256(path),
			}
		} else {
			head.Shards = append(head.Shards, shardInfo{
				Path:    shardPath,
				FromSeq: all[0].Seq,
				ToSeq:   all[len(all)-1].Seq,
				Count:   len(all),
				SHA256:  fileSHA256(path),
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

func readMessageRowsFileAt(path string) ([]Event, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readMessageRowsFile(file)
}

// appendMessageRowsFile 追加完整 JSONL 行（先截崩溃残尾；再 sync）。
func appendMessageRowsFile(path string, rows []Event) error {
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
			return fmt.Errorf("session storage: encode message row: %w", err)
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

func fileSHA256(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// readRows 按 seq 区间读取已发布行（[from, to] 含端点；越界安全）。
// from == 0 表示从首行开始；to == 0 表示到 head 末尾。
func (store *storeEngine) readRows(key Key, fromSeq, toSeq uint64) ([]Event, error) {
	store.mu(key, moduleMessage).Lock()
	defer store.mu(key, moduleMessage).Unlock()
	return store.readRowsLocked(key, fromSeq, toSeq)
}

func (store *storeEngine) readRowsLocked(key Key, fromSeq, toSeq uint64) ([]Event, error) {
	head, err := store.readMessageHeadLocked(key)
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
		rows, err := readMessageRowsFileAt(filepath.Join(store.messageDir(key), shard.Path))
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

// readAllRows 读取全部已发布行（含 LRU 空洞；已淘汰前缀自然缺失）。
func (store *storeEngine) readAllRows(key Key) ([]Event, error) {
	return store.readRows(key, 0, 0)
}

// readTailRowsForSelection 只读“尾部选择可能需要的”分片，避免每轮
// 读/恢复都全量解码 message 文件：
//   - tokenBudget/maxUnits 为正常窗口（maxUnits < 1<<20）时，从最后一个
//     分片向前累积，直到行数覆盖 maxUnits 边界或 token 预算（含前一
//     分片作单元边界余量）即停止；
//   - 全量语义（MaxInt/MaxInt）保持旧行为读全量。
//
// 返回的行按 seq 升序，供调用方再做 selectEventTail/完整单元裁剪。
func (store *storeEngine) readTailRowsForSelection(key Key, tokenBudget, maxUnits int) ([]Event, error) {
	if tokenBudget <= 0 || maxUnits <= 0 {
		return []Event{}, nil
	}
	if maxUnits >= 1<<20 {
		return store.readRows(key, 1, 0)
	}
	messageLock := store.mu(key, moduleMessage)
	messageLock.Lock()
	defer messageLock.Unlock()
	head, err := store.readMessageHeadLocked(key)
	if err != nil {
		return nil, err
	}
	if head.LastSeq == 0 {
		return []Event{}, nil
	}
	// 128 行/单元上限的保守估算：覆盖普通轮次 + 边界余量，同时避免把
	// 整个历史读进来；超长单单元（极端工具链）会退化为读更多分片。
	rowCap := (maxUnits + 1) * 128
	var rows []Event
	collectedTokens := 0
	for index := len(head.Shards) - 1; index >= 0; index-- {
		shard := head.Shards[index]
		shardRows, readErr := readMessageRowsFileAt(filepath.Join(store.messageDir(key), shard.Path))
		if readErr != nil {
			return nil, readErr
		}
		for _, row := range shardRows {
			collectedTokens += row.TokenCount
		}
		rows = append(shardRows, rows...)
		if len(rows) >= rowCap {
			break
		}
		if tokenBudget < math.MaxInt && collectedTokens >= tokenBudget && index < len(head.Shards)-1 {
			break
		}
	}
	return rows, nil
}

// verifyMessage 校验 message 通道：head 可读、分片存在、文件行数与 head
// 一致、head 末行 = 最后已发布行；LRU 空洞（锚 ≤ watermark）不算损坏。
func (store *storeEngine) verifyMessage(key Key) error {
	store.mu(key, moduleMessage).Lock()
	defer store.mu(key, moduleMessage).Unlock()
	head, err := store.readMessageHeadLocked(key)
	if err != nil {
		return err
	}
	total := uint64(0)
	for _, shard := range head.Shards {
		rows, err := readMessageRowsFileAt(filepath.Join(store.messageDir(key), shard.Path))
		if err != nil {
			return fmt.Errorf("session storage: verify shard %s: %w", shard.Path, err)
		}
		if len(rows) != shard.Count {
			return fmt.Errorf("session storage: verify shard %s rows=%d want %d", shard.Path, len(rows), shard.Count)
		}
		if len(rows) > 0 {
			if rows[0].Seq != shard.FromSeq || rows[len(rows)-1].Seq != shard.ToSeq {
				return fmt.Errorf("session storage: verify shard %s seq range [%d,%d] != [%d,%d]",
					shard.Path, rows[0].Seq, rows[len(rows)-1].Seq, shard.FromSeq, shard.ToSeq)
			}
			if rows[len(rows)-1].Seq > head.LastSeq {
				return fmt.Errorf("session storage: verify shard %s beyond head last_seq=%d", shard.Path, head.LastSeq)
			}
		}
		total += uint64(len(rows))
	}
	if total != head.TotalRows {
		return fmt.Errorf("session storage: verify message total=%d want %d", total, head.TotalRows)
	}
	return nil
}

// messageCount 返回当前物理行数（淘汰后不包含前缀空洞）。
func (store *storeEngine) messageCount(key Key) (uint64, error) {
	store.mu(key, moduleMessage).Lock()
	defer store.mu(key, moduleMessage).Unlock()
	head, err := store.readMessageHeadLocked(key)
	if err != nil {
		return 0, err
	}
	return head.TotalRows, nil
}
