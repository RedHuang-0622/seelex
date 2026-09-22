package sessionstore

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// 草稿尾部（seq_draft）—— message 通道里「已 append、head 未发布」的那一段。
//
// 口径（与 my_design §2.0 规则 4、§3 message 通道一致）：
//   - head.LastSeq 是**发布点**：seq <= head.LastSeq = 已提交、对读者可见；
//   - seq >  head.LastSeq = 未提交草稿：写盘顺序先于发布点推进。提交路径
//     （reapUnpublishedLocked）按「未提交 ⇒ 清理」处理，避免与重试行重复；
//   - 崩溃后这段草稿**可以**被救回，但**绝不能被隐式提升**为已提交
//     （head_self_heal 的 D2 红线：读者自愈不得提升「append 完成但 head
//     未发布」的行）。因此只有本文件的**显式恢复入口**允许推进发布点。
//
// 恢复前置条件（基座校验）：草稿尾部必须恰好是发布点的连续后续——首行
// seq == head.LastSeq+1，且逐行 +1。任一条件不成立 = 基座断裂：只报告、
// 不发布、不隐式清理（把决策留给应用，清理走显式丢弃入口）。
const (
	// PendingTailClean 无草稿尾部（发布点即物理末行）。
	PendingTailClean = "clean"
	// PendingTailRecoverable 基座一致，可安全发布。
	PendingTailRecoverable = "recoverable"
	// PendingTailRecovered 本次调用已把草稿尾部发布（L2 恢复完成）。
	PendingTailRecovered = "recovered"
	// PendingTailGap 基座断裂：不发布（只能显式丢弃或人工介入）。
	PendingTailGap = "gap"
	// PendingTailDiscarded 本次调用已显式清理草稿尾部。
	PendingTailDiscarded = "discarded"
)

// PendingTailReport 是一次草稿尾部探测/恢复的结果快照。
//
// Rows 是草稿行正文（按 seq 升序）；ExtraShards 是 head 未索引的分片文件
// 名（整个文件都是草稿，典型来源：分片滚动后、head 发布前崩溃）。
type PendingTailReport struct {
	SessionID   string   `json:"session_id"`
	Status      string   `json:"status"`
	HeadSeq     uint64   `json:"head_seq"`
	TailFrom    uint64   `json:"tail_from,omitempty"`
	TailTo      uint64   `json:"tail_to,omitempty"`
	Rows        []Event  `json:"rows,omitempty"`
	ExtraShards []string `json:"extra_shards,omitempty"`
	Reason      string   `json:"reason,omitempty"`
}

// RowCount 返回草稿尾部行数（不含已发布行）。
func (report PendingTailReport) RowCount() int { return len(report.Rows) }

// ---------- 探测 ----------

// messageShardFilesLocked 列出 message 目录下的分片文件，按文件名解析出的
// from_seq 升序（调用方持 messageMu）。
func (store *storeEngine) messageShardFilesLocked(key Key) ([]string, error) {
	entries, err := os.ReadDir(store.messageDir(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	type shardFile struct {
		name string
		from uint64
	}
	files := make([]shardFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isMessageShardFile(entry.Name()) {
			continue
		}
		var from, to uint64
		if _, err := fmt.Sscanf(entry.Name(), "message_%d_%d.jsonl", &from, &to); err != nil {
			continue
		}
		files = append(files, shardFile{name: entry.Name(), from: from})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].from < files[j].from })
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, file.name)
	}
	return names, nil
}

// pendingTailLocked 读出「seq > head.LastSeq」的全部物理行（调用方持
// messageMu）。返回的 duplicate=true 表示同一 seq 被多个分片承载（数据
// 层面的不一致，只能报告不能发布）。
func (store *storeEngine) pendingTailLocked(key Key, head messageHead) ([]Event, []string, bool, error) {
	dir := store.messageDir(key)
	indexed := make(map[string]bool, len(head.Shards))
	for _, shard := range head.Shards {
		indexed[filepath.Clean(shard.Path)] = true
	}
	names, err := store.messageShardFilesLocked(key)
	if err != nil {
		return nil, nil, false, err
	}
	var rows []Event
	var extra []string
	seen := make(map[uint64]bool)
	duplicate := false
	for _, name := range names {
		fileRows, err := readMessageRowsFileAt(filepath.Join(dir, name))
		if err != nil {
			return nil, nil, false, err
		}
		if !indexed[filepath.Clean(name)] {
			// head 未索引的分片文件：整个文件都是草稿（分片滚动后崩溃）。
			extra = append(extra, name)
		}
		for _, row := range fileRows {
			if row.Seq <= head.LastSeq {
				continue // 已发布行
			}
			if seen[row.Seq] {
				duplicate = true
				continue
			}
			seen[row.Seq] = true
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Seq < rows[j].Seq })
	return rows, extra, duplicate, nil
}

// classifyPendingTail 判定草稿尾部的可恢复性（基座校验）。
func classifyPendingTail(head messageHead, rows []Event, duplicate bool) (string, string) {
	if len(rows) == 0 {
		return PendingTailClean, ""
	}
	if duplicate {
		return PendingTailGap, "草稿尾部存在重复 seq（多个分片承载同一 seq）：不发布"
	}
	want := head.LastSeq + 1
	for _, row := range rows {
		if row.Seq != want {
			return PendingTailGap, fmt.Sprintf(
				"草稿尾部与发布点不连续：期望 seq %d，实际 %d（发布点已不被本段独占，只能在一致前缀处截断）",
				want, row.Seq)
		}
		want++
	}
	return PendingTailRecoverable, ""
}

// pendingTailReportLocked 是探测的锁内实现。
func (store *storeEngine) pendingTailReportLocked(key Key) (PendingTailReport, error) {
	head, err := store.readMessageHeadLocked(key)
	if err != nil {
		return PendingTailReport{}, err
	}
	rows, extra, duplicate, err := store.pendingTailLocked(key, head)
	if err != nil {
		return PendingTailReport{}, err
	}
	status, reason := classifyPendingTail(head, rows, duplicate)
	report := PendingTailReport{
		SessionID:   key.SessionID,
		Status:      status,
		HeadSeq:     head.LastSeq,
		Rows:        rows,
		ExtraShards: extra,
		Reason:      reason,
	}
	if len(rows) > 0 {
		report.TailFrom = rows[0].Seq
		report.TailTo = rows[len(rows)-1].Seq
	}
	return report, nil
}

// pendingTailReport 探测草稿尾部（只读，不发布、不清理）。
func (store *storeEngine) pendingTailReport(key Key) (PendingTailReport, error) {
	store.mu(key, moduleMessage).Lock()
	defer store.mu(key, moduleMessage).Unlock()
	return store.pendingTailReportLocked(key)
}

// ---------- 逐步 append（写者草稿尾） ----------

// draftBaseSeqLocked 返回"物理末行 seq"作为草稿续号基准（调用方持 messageMu）。
//
// 只读物理末分片：分片按 from_seq 有序、分片内 seq 递增，所以 from_seq 最大的
// 分片的最后一行就是物理末行。索引末分片可能已被写入草稿行（索引里的区间/计数
// 还停在发布时的值），因此只认文件内容、不认索引的 ToSeq。
func (store *storeEngine) draftBaseSeqLocked(key Key, head messageHead) (uint64, error) {
	last, err := store.physicalLastShardLocked(key)
	if err != nil {
		return 0, err
	}
	if last == "" {
		return head.LastSeq, nil
	}
	rows, err := readMessageRowsFileAt(filepath.Join(store.messageDir(key), last))
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return head.LastSeq, nil
	}
	if seq := rows[len(rows)-1].Seq; seq > head.LastSeq {
		return seq, nil
	}
	return head.LastSeq, nil
}

// appendDraftRowsLocked 逐步 append（写者草稿尾）：把行写进物理尾并 sync，但
// **不推进发布点**——head 不写、LastCommitID 不改，读者可见范围不变（长回合的
// 中间行不再需要读者侧等待一次"回合结束大重写"）。
//
// 与提交路径的差别只有两处：续号基准取物理末行（不是发布点）；不 reap（草稿行
// 正是要保留的内容）。发布/丢弃都只走显式入口（publishDraftTail /
// discardPendingTail），不存在隐式提升（head_self_heal 的 D2 红线）。
//
// 返回分配好 seq 并打上 commit_id 的行；调用方按返回顺序持有它们，直到本轮发布
// 或丢弃。
func (store *storeEngine) appendDraftRowsLocked(key Key, commitID string, rows []Event) ([]Event, error) {
	if len(rows) == 0 {
		return []Event{}, nil
	}
	if _, err := store.ensureLayoutGuide(key); err != nil {
		return nil, err
	}
	if commitID == "" {
		// S17/D13：存储层不现造随机凭据，按内容确定性推导。
		commitID = messageRowsCommitID(rows)
	}
	head, err := store.readMessageHeadLocked(key)
	if err != nil {
		return nil, err
	}
	base, err := store.draftBaseSeqLocked(key, head)
	if err != nil {
		return nil, err
	}
	delta, err := assignRowSeqs(rows, base, commitID)
	if err != nil {
		return nil, err
	}
	if len(delta) == 0 {
		return []Event{}, nil
	}
	if _, err := store.writeShardRowsLocked(key, delta); err != nil {
		return nil, err
	}
	return delta, nil
}

// appendDraftRows 是 appendDraftRowsLocked 的加锁入口（逐步 append 的公开面）。
func (store *storeEngine) appendDraftRows(key Key, commitID string, rows []Event) ([]Event, error) {
	store.mu(key, moduleMessage).Lock()
	defer store.mu(key, moduleMessage).Unlock()
	return store.appendDraftRowsLocked(key, commitID, rows)
}

// publishDraftTail 把本轮逐步 append 的草稿尾一次性发布（写者视角的发布入口，
// 与 recoverPendingTail 同一实现：基座校验通过才推进发布点，断裂只报告不发布）。
func (store *storeEngine) publishDraftTail(key Key) (PendingTailReport, error) {
	return store.recoverPendingTail(key)
}

// ---------- 显式恢复 / 显式丢弃 ----------

// recoverPendingTail 把基座一致的草稿尾部**显式发布**为已提交（L2 恢复）。
//   - clean                     → 空操作；
//   - recoverable               → 重建分片索引 + 推进发布点，返回 recovered；
//   - gap                       → 只报告，不发布、不清理。
//
// 发布凭据取草稿行自带的 commit_id（确定性；S17/D13 禁止现造随机凭据），
// 兜底由行内容推导；因此重复调用是幂等的（第二次探测即 clean）。
func (store *storeEngine) recoverPendingTail(key Key) (PendingTailReport, error) {
	store.mu(key, moduleMessage).Lock()
	defer store.mu(key, moduleMessage).Unlock()
	report, err := store.pendingTailReportLocked(key)
	if err != nil {
		return PendingTailReport{}, err
	}
	if report.Status != PendingTailRecoverable {
		return report, nil
	}
	head, err := store.readMessageHeadLocked(key)
	if err != nil {
		return PendingTailReport{}, err
	}
	if err := store.publishPendingTailLocked(key, head, report.Rows); err != nil {
		return PendingTailReport{}, err
	}
	report.Status = PendingTailRecovered
	report.Reason = ""
	return report, nil
}

// publishPendingTailLocked 重建分片索引并把草稿尾部提升为已发布，然后原子
// 发布 head（调用方持 messageMu，且已完成基座校验）。
//
// 与提交路径的差别只有一处：**不做 reap**——草稿行正是本次要保留的内容。
func (store *storeEngine) publishPendingTailLocked(key Key, head messageHead, rows []Event) error {
	dir := store.messageDir(key)
	names, err := store.messageShardFilesLocked(key)
	if err != nil {
		return err
	}
	shards := make([]shardInfo, 0, len(names))
	total := uint64(0)
	for _, name := range names {
		fileRows, err := readMessageRowsFileAt(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if len(fileRows) == 0 {
			continue
		}
		shards = append(shards, shardInfo{
			Path:    name,
			FromSeq: fileRows[0].Seq,
			ToSeq:   fileRows[len(fileRows)-1].Seq,
			Count:   len(fileRows),
			SHA256:  fileSHA256(filepath.Join(dir, name)),
		})
		total += uint64(len(fileRows))
	}
	now := time.Now().UTC()
	tokens := 0
	lastMessageID := ""
	commitID := ""
	for _, row := range rows {
		tokens += row.TokenCount
		if row.MessageID != "" {
			lastMessageID = row.MessageID
		}
		if row.CommitID != "" {
			commitID = row.CommitID
		}
	}
	if commitID == "" {
		commitID = messageRowsCommitID(rows)
	}
	head.Shards = shards
	head.LastSeq = rows[len(rows)-1].Seq
	head.LastMessageID = lastMessageID
	head.TotalRows = total
	head.LastCommitID = commitID
	head.Meta.TokenCount += tokens
	head.Meta.ShardCount = len(shards)
	head.Meta.UpdatedAt = now
	if head.Meta.SessionID == "" {
		head.Meta.SessionID = key.SessionID
	}
	if head.Meta.CreatedAt.IsZero() {
		head.Meta.CreatedAt = now
	}
	if _, err := store.publishModuleHead(key, moduleMessage, commitID, head, now); err != nil {
		return err
	}
	store.rememberMessageAnchor(key, head)
	return nil
}

// discardPendingTail 显式清理草稿尾部（与提交路径 reap 的清理口径一致），
// 使「丢弃」成为可观测、可审计的一次操作，而不是静默副作用。
func (store *storeEngine) discardPendingTail(key Key) (PendingTailReport, error) {
	store.mu(key, moduleMessage).Lock()
	defer store.mu(key, moduleMessage).Unlock()
	report, err := store.pendingTailReportLocked(key)
	if err != nil {
		return PendingTailReport{}, err
	}
	if len(report.Rows) == 0 {
		return report, nil
	}
	head, err := store.readMessageHeadLocked(key)
	if err != nil {
		return PendingTailReport{}, err
	}
	if err := store.reapUnpublishedLocked(key, head); err != nil {
		return PendingTailReport{}, err
	}
	report.Status = PendingTailDiscarded
	report.Reason = fmt.Sprintf("显式丢弃 %d 行草稿（%s）", len(report.Rows), report.Reason)
	return report, nil
}

// ---------- 仓库层（布局门控） ----------

func (repository *jsonRepository) pendingTailWorkspace(key Key) (PendingTailReport, bool, error) {
	if !repository.active(key) {
		return PendingTailReport{}, false, nil
	}
	report, err := repository.layout.pendingTailReport(key)
	if err != nil {
		return PendingTailReport{}, true, err
	}
	return report, true, nil
}

func (repository *jsonRepository) recoverPendingTailWorkspace(key Key) (PendingTailReport, bool, error) {
	if !repository.active(key) {
		return PendingTailReport{}, false, nil
	}
	report, err := repository.layout.recoverPendingTail(key)
	if err != nil {
		return PendingTailReport{}, true, err
	}
	return report, true, nil
}

func (repository *jsonRepository) discardPendingTailWorkspace(key Key) (PendingTailReport, bool, error) {
	if !repository.active(key) {
		return PendingTailReport{}, false, nil
	}
	report, err := repository.layout.discardPendingTail(key)
	if err != nil {
		return PendingTailReport{}, true, err
	}
	return report, true, nil
}
