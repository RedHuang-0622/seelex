// 压缩记录通道（moduleCompaction）：把「这次压缩到底发生了什么」从退役的
// record 通道搬到本会话自己的 append-only 事实源上。
//
// 为什么需要它（docs/research/2026-10-02-compaction-record-and-failure-trace-survey.md §4.2）：
// 前端右栏「上下文压缩」（压缩栈表 + 失败痕 + seq 徽标）、轨迹「压缩」轨、对话区
// 「以上 … 已被压缩」分界，以及冷恢复的保留窗口起点（ContextRetainedFrom），
// 都只读 `record.Execution.Task.ContextCompactions`。而现行存储布局（v8/S20）里
// record 通道已退役：`SaveRecordRaw` 只写穿 status/title，`LoadRecordRaw` 交回的是
// 按 message head 派生的最小 record——**没有 execution 子树**。于是进程重启后
// 「压缩帧在（compact 通道）、压缩记录不在」：右栏整条为空、保留窗口起点归零，
// 下一次装配把已被压出的前缀重新计入预算。
//
// 形状与 compact 通道同构，但**不共用文件**：一行一次压缩（成功记录或失败痕），
// Seq 从 1 单调递增；head 只记水位（行数 + 末行指纹），不冗余正文。payload 是应用侧
// 压缩记录的序列化原文——存储层不解析它，因此应用侧 schema 增字段不需要动存储层，
// 也不会出现「存储层镜像类型与应用模型两处漂移」。
//
// 写序与其它通道一致：先 append 数据行（截崩溃残尾 → sync），再原子发布 head；
// head 未替换 = 未提交，下一次提交按水位重算并覆盖残尾。
package sessionstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// CompactionRecordRow 是压缩记录通道的一行。Payload 是应用侧压缩记录的序列化
// 原文（存储层不解析；调用方负责 schema 含义与向前兼容）。
type CompactionRecordRow struct {
	Seq     uint64          `json:"seq"`
	Payload json.RawMessage `json:"payload"`
}

// compactionHeadRecord 是 metadata/compaction.json 的 payload：只有水位。
//
// Count 既是「已发布多少行」，也是幂等判据的坐标——应用侧维护的压缩记录列表是
// append-only 的（只追加、不重排、不删除），因此「列表的第 N 条」与「文件里的第 N
// 行」是同一个东西，重复落盘只需比较长度。LastChecksum 是第 Count 行 payload 的
// 指纹：内存列表若被别的事实源重建过（前缀对不上），追加就会把两份历史缝在一起，
// 此时改为整份重写（自愈），而不是静默产生一条重复记录。
type compactionHeadRecord struct {
	SessionID    string `json:"session_id"`
	Count        int    `json:"count"`
	LastChecksum string `json:"last_checksum,omitempty"`
}

// compactionRecordsPath 返回压缩记录数据文件（与 compact.jsonl 同层，但**不是
// 同一个文件**：compact.jsonl 是帧摘要水位，本文件是压缩记录事件流）。
func (store *storeEngine) compactionRecordsPath(key Key) string {
	return filepath.Join(store.sessionRoot(key), "compaction-records.jsonl")
}

// commitCompactionRecords 按水位把新增压缩记录追加进本会话通道，并原子发布 head。
//
// 幂等与自愈：
//   - head.Count == len(records) → 本次没有新记录，整次空操作（每回合收尾都会调它，
//     常态即此路径，一次 stat 都不用做）；
//   - head.Count < len(records) 且第 Count 行指纹相符 → 追加尾部，报告实际追加行数；
//   - 前缀指纹不符（内存列表被别的事实源重建/截断过）→ 整份重写：宁可按调用方给的
//     列表对齐，也不把两份互不衔接的历史缝成一条；
//   - head.Count > len(records) → append-only 通道不做删除，返回现状（调用方按
//     "盘上有更多"处理，绝不截断别人写下的历史）。
func (store *storeEngine) commitCompactionRecords(key Key, records []json.RawMessage) (compactionHeadRecord, int, error) {
	store.mu(key, moduleCompaction).Lock()
	defer store.mu(key, moduleCompaction).Unlock()
	if _, err := store.ensureLayoutGuide(key); err != nil {
		return compactionHeadRecord{}, 0, err
	}
	head, err := store.readCompactionHeadLocked(key)
	if err != nil {
		return compactionHeadRecord{}, 0, err
	}
	head.SessionID = key.SessionID
	switch {
	case head.Count >= len(records):
		// 盘上不少于调用方：没有可安全追加的行（含"完全一致"的常态空操作）。
		return head, 0, nil
	case head.Count > 0 && head.LastChecksum != "" &&
		head.LastChecksum != compactionRecordChecksum(records[head.Count-1]):
		if err := store.replaceCompactionRecordsLocked(key, records); err != nil {
			return compactionHeadRecord{}, 0, err
		}
		head.Count = len(records)
		head.LastChecksum = compactionRecordChecksum(records[len(records)-1])
		if _, err := store.publishModuleHead(key, moduleCompaction, compactionRecordsCommitID(head), head, time.Now().UTC()); err != nil {
			return compactionHeadRecord{}, 0, err
		}
		return head, len(records), nil
	}
	rows := make([]CompactionRecordRow, 0, len(records)-head.Count)
	for index, payload := range records[head.Count:] {
		rows = append(rows, CompactionRecordRow{Seq: uint64(head.Count + index + 1), Payload: payload})
	}
	if err := appendCompactionRecordRows(store.compactionRecordsPath(key), rows); err != nil {
		return compactionHeadRecord{}, 0, err
	}
	head.Count = len(records)
	head.LastChecksum = compactionRecordChecksum(records[len(records)-1])
	if _, err := store.publishModuleHead(key, moduleCompaction, compactionRecordsCommitID(head), head, time.Now().UTC()); err != nil {
		return compactionHeadRecord{}, 0, err
	}
	return head, len(rows), nil
}

// readCompactionHeadLocked 返回最新聚合 head（缺失 = 零值，不报错）。
func (store *storeEngine) readCompactionHeadLocked(key Key) (compactionHeadRecord, error) {
	headFile, err := store.readModuleHeadFileLocked(key, moduleCompaction)
	if errors.Is(err, fs.ErrNotExist) {
		return compactionHeadRecord{SessionID: key.SessionID}, nil
	}
	if err != nil {
		return compactionHeadRecord{}, err
	}
	return decodeHeadPayload[compactionHeadRecord](headFile)
}

// readCompactionRecords 读取该会话全部压缩记录行（[1, head.Count] 含端点；缺失 =
// 空，不报错）。崩溃残尾与超出水位的行按水位截断——与其它 append-only 通道同一语义。
func (store *storeEngine) readCompactionRecords(key Key) ([]CompactionRecordRow, error) {
	store.mu(key, moduleCompaction).Lock()
	defer store.mu(key, moduleCompaction).Unlock()
	head, err := store.readCompactionHeadLocked(key)
	if err != nil {
		return nil, err
	}
	rows, err := readCompactionRecordRows(store.compactionRecordsPath(key))
	if err != nil {
		return nil, err
	}
	if head.Count > 0 && len(rows) > head.Count {
		rows = rows[:head.Count]
	}
	return rows, nil
}

// replaceCompactionRecordsLocked 用调用方给的列表整份重写数据文件（前缀指纹不符
// 时的自愈路径；调用方持模块锁）。写序与追加一致：数据文件先落，head 由调用方发布。
func (store *storeEngine) replaceCompactionRecordsLocked(key Key, records []json.RawMessage) error {
	path := store.compactionRecordsPath(key)
	if len(records) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	buffer := make([]byte, 0, 4096)
	for index, payload := range records {
		data, err := json.Marshal(CompactionRecordRow{Seq: uint64(index + 1), Payload: payload})
		if err != nil {
			return err
		}
		buffer = append(buffer, data...)
		buffer = append(buffer, '\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeAtomic(path, buffer, 0o600)
}

// compactionRecordChecksum 是单行 payload 的指纹（水位校验用，不承担内容防腐）。
func compactionRecordChecksum(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}
	return "cr-" + hash(string(payload))
}

// compactionRecordsCommitID 由 head 本身推导提交凭据（S17/D13：不用存储层随机号）。
func compactionRecordsCommitID(head compactionHeadRecord) string {
	return "compaction-records-" + hash(fmt.Sprintf("%d|%s", head.Count, head.LastChecksum))
}

// appendCompactionRecordRows 追加完整 JSONL 行（先截崩溃残尾；再 sync）。
func appendCompactionRecordRows(path string, rows []CompactionRecordRow) error {
	if len(rows) == 0 {
		return nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := truncateCrashTail(file); err != nil {
		file.Close()
		return err
	}
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	if _, err := file.Seek(stat.Size(), 0); err != nil {
		file.Close()
		return err
	}
	for _, row := range rows {
		data, err := json.Marshal(row)
		if err != nil {
			file.Close()
			return err
		}
		if _, err := file.Write(append(data, '\n')); err != nil {
			file.Close()
			return err
		}
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// readCompactionRecordRows 读取 compaction-records.jsonl 的完整行（崩溃残尾跳过，
// 与 compact.jsonl 的读法一致）。
func readCompactionRecordRows(path string) ([]CompactionRecordRow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []CompactionRecordRow{}, nil
		}
		return nil, err
	}
	segments := bytes.Split(data, []byte{'\n'})
	rows := make([]CompactionRecordRow, 0, len(segments))
	for index, segment := range segments {
		if index == len(segments)-1 && len(bytes.TrimSpace(segment)) > 0 {
			continue // 崩溃残尾（无换行收尾的行）
		}
		segment = bytes.TrimSpace(segment)
		if len(segment) == 0 {
			continue
		}
		var row CompactionRecordRow
		if json.Unmarshal(segment, &row) != nil {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// rebuildCompactionHeadFromData 从 compaction-records.jsonl 重建 compaction head
// （S15：head 损坏时按数据文件自愈，压缩记录因此不会因一次 head 损坏整条消失）。
func (store *storeEngine) rebuildCompactionHeadFromData(key Key) (compactionHeadRecord, error) {
	head := compactionHeadRecord{SessionID: key.SessionID}
	rows, err := readCompactionRecordRows(store.compactionRecordsPath(key))
	if err != nil {
		return compactionHeadRecord{}, err
	}
	if len(rows) == 0 {
		return head, nil
	}
	head.Count = len(rows)
	head.LastChecksum = compactionRecordChecksum(rows[len(rows)-1].Payload)
	return head, nil
}
