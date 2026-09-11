// 栈通道的 JSON 文件后端（my_design §2.4 放置位置 + §2.0 head/发布点规则）。
//
// 放置：session/{plan,task,goal}/active.jsonl（当前投影，原子替换）、
// {kind}/history.jsonl（append-only 归档）；head 按 kind 分文件
// metadata/stack_{plan,task,goal}.json（S16），只存该 kind 水位。
//
// 读路径为什么可以不持锁、不碰文件：
//   - 提交临界区（按 kind 的 stack{Plan,Task,Goal}Mu，S16）结束时把 active
//     投影连同该 kind head 水位发布成一份不可变内存快照（atomic.Pointer），
//     读者只读该快照 → 读者与写者之间既没有锁等待，也不会因为「读者持有
//     句柄」让 Windows 上的 rename 发布失败；
//   - 归档文件是 append-only 且发布后不再改写，读者直接读文件不需要锁；head
//     未发布的行按 revision 过滤掉（与写路径同一判据）。
//
// 写路径的 history 成本是 O(1)：不解析归档文件，只按 head 记录的
// history_bytes 把「已落盘但未发布」的归档尾行截掉（回收），再追加本次归档行
// 并记录新的字节水位。
package sessionstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// jsonStackJournal 是栈通道的 JSON 后端。
type jsonStackJournal struct {
	store *storeEngine
}

// stackView 是一份已发布的栈读投影（不可变；只追加不换引用）。
type stackView struct {
	headSeq      uint64
	kinds        map[StackKind]stackWatermark
	lastCommitID string
	active       []StackItemRecord
	historyCount uint64
}

// stackJournal 是 jsonRepository 的 Repository 契约方法：栈通道对每个后端
// 都必须存在，不允许「某个后端没有栈」的运行期分支。
func (repository *jsonRepository) stackJournal() stackJournal {
	return repository.layout.stackJournal()
}

func (store *storeEngine) stackJournal() stackJournal {
	return &jsonStackJournal{store: store}
}

func (journal *jsonStackJournal) backend() string { return string(BackendJSON) }

// stackDir / 文件路径（§2.4 放置位置）。
func (store *storeEngine) stackDir(key Key, kind StackKind) string {
	return filepath.Join(store.sessionRoot(key), string(kind))
}

func (store *storeEngine) stackActivePath(key Key, kind StackKind) string {
	return filepath.Join(store.stackDir(key, kind), "active.jsonl")
}

func (store *storeEngine) stackHistoryPath(key Key, kind StackKind) string {
	return filepath.Join(store.stackDir(key, kind), "history.jsonl")
}

// lock 按「会话 × kind」串行化栈提交（S16/D6：每 kind 一份 head 一把锁，
// 跨 kind 不再共享串行点）。等待时长单独计量 → mutexprofile 之外还能直接
// 回答「栈锁等了多久」。
func (journal *jsonStackJournal) lock(key Key, kind StackKind) func() {
	store := journal.store
	lock := store.mu(key, moduleForStackKind(kind))
	begin := time.Now()
	lock.Lock()
	store.stackStats().lockWait.Add(time.Since(begin).Nanoseconds())
	return lock.Unlock
}

// load 返回 head 水位与 active 投影：命中内存快照则零 IO，未命中才冷读磁盘
// （冷读只发生在该会话在本进程的首次访问，且只在提交临界区内）。
func (journal *jsonStackJournal) load(key Key, kind StackKind) (stackLoaded, error) {
	store := journal.store
	stats := store.stackStats()
	if view := store.stackViewLoad(key, kind); view != nil {
		return stackLoaded{
			HeadSeq:      view.headSeq,
			Kinds:        cloneStackWatermarks(view.kinds),
			Active:       cloneStackRows(view.active),
			HistoryCount: view.historyCount,
			LastCommitID: view.lastCommitID,
		}, nil
	}
	// 冷读：该会话在本进程的首次访问才走磁盘（此后读者与写者都用快照）。
	begin := time.Now()
	head, err := journal.readStackHead(key, kind)
	if err != nil {
		return stackLoaded{}, err
	}
	active, err := readStackRowsFile(store.stackActivePath(key, kind), head.HeadSeq)
	stats.activeIO.Add(time.Since(begin).Nanoseconds())
	if err != nil {
		return stackLoaded{}, err
	}
	active = dedupeStackRowsByItemID(active)
	water := head.Kinds[kind]
	store.stackViewPublish(key, kind, &stackView{
		headSeq: head.HeadSeq, kinds: cloneStackWatermarks(head.Kinds),
		lastCommitID: water.LastCommitID, active: active, historyCount: water.HistoryCount,
	})
	return stackLoaded{
		HeadSeq: head.HeadSeq, Kinds: cloneStackWatermarks(head.Kinds),
		Active: cloneStackRows(active), HistoryCount: water.HistoryCount,
		LastCommitID: water.LastCommitID,
	}, nil
}

// readHistory 直接读归档文件（append-only，发布后的行不再改写 → 无锁读）。
func (journal *jsonStackJournal) readHistory(key Key, kind StackKind) ([]StackItemRecord, error) {
	store := journal.store
	stats := store.stackStats()
	begin := time.Now()
	head, err := journal.readStackHead(key, kind)
	if err != nil {
		return nil, err
	}
	rows, err := readStackRowsFile(store.stackHistoryPath(key, kind), head.HeadSeq)
	stats.historyRead.Add(time.Since(begin).Nanoseconds())
	if err != nil {
		return nil, err
	}
	return dedupeStackRowsByItemID(rows), nil
}

// publish 落数据并发布 head：归档回收 → 归档追加 → active 原子替换 → head
// 发布（提交点）→ 内存读快照。任一步失败都不动已发布 head（未发布即可见性
// 为零，I5）。
func (journal *jsonStackJournal) publish(key Key, entry stackPublish) error {
	store := journal.store
	stats := store.stackStats()
	if err := timeSection(&stats.guideIO, func() error {
		_, err := store.ensureLayoutGuide(key)
		return err
	}); err != nil {
		return err
	}
	head, err := journal.readStackHead(key, entry.Kind)
	if err != nil {
		return err
	}
	previous := head.Kinds[entry.Kind]
	historyPath := store.stackHistoryPath(key, entry.Kind)
	// 1) 先回收 head 未发布的归档尾行（O(1) 截断，不解析文件）。
	if err := journal.reapHistoryTail(historyPath, previous); err != nil {
		return err
	}
	// 2) 归档行 append-only 续写。
	if err := timeSection(&stats.historyAppend, func() error {
		return appendStackRows(historyPath, entry.Archived)
	}); err != nil {
		return err
	}
	historyBytes, err := fileSizeOrZero(historyPath)
	if err != nil {
		return err
	}
	// 3) active 投影整文件原子替换。
	activePath := store.stackActivePath(key, entry.Kind)
	if err := os.MkdirAll(filepath.Dir(activePath), 0o700); err != nil {
		return err
	}
	if err := timeSection(&stats.activeIO, func() error {
		return writeAtomic(activePath, encodeStackRows(entry.Active), 0o600)
	}); err != nil {
		return err
	}
	// 4) 发布 head（提交点）。
	watermark := entry.Watermark
	watermark.HistoryBytes = historyBytes
	head.SessionID = key.SessionID
	head.HeadSeq = entry.Revision
	if head.Kinds == nil {
		head.Kinds = make(map[StackKind]stackWatermark)
	}
	head.Kinds[entry.Kind] = watermark
	if err := timeSection(&stats.headIO, func() error {
		if _, err := store.publishModuleHead(key, moduleForStackKind(entry.Kind), entry.Watermark.LastCommitID, head, time.Now().UTC()); err != nil {
			return err
		}
		store.stackStats().commits.Add(1)
		return nil
	}); err != nil {
		return err
	}
	// 5) 最后发布读者投影（与 head 同一份内容）。
	journal.publishView(key, head, entry.Kind, entry.Active, watermark.HistoryCount)
	return nil
}

// publishView 只重建被本次提交改动的 kind 的读投影（S16：各 kind head
// 独立，其它 kind 的投影不受影响）。
func (journal *jsonStackJournal) publishView(key Key, head stackModuleHead, kind StackKind, active []StackItemRecord, historyCount uint64) {
	store := journal.store
	kinds := cloneStackWatermarks(head.Kinds)
	store.stackViewPublish(key, kind, &stackView{
		headSeq: head.HeadSeq, kinds: kinds, active: cloneStackRows(active),
		lastCommitID: head.Kinds[kind].LastCommitID, historyCount: historyCount,
	})
}

// reapHistoryTail 把归档文件截回 head 记录的字节水位（head 未发布的追加是
// 未提交数据）。HistoryBytes 为 0 但已有归档计数时按「未知字节水位」处理：
// 以当前文件长度为基线补齐，不截断（避免把已发布归档当成残尾删掉）。
func (journal *jsonStackJournal) reapHistoryTail(path string, water stackWatermark) error {
	size, err := fileSizeOrZero(path)
	if err != nil || size == 0 {
		return err
	}
	if water.HistoryBytes == 0 {
		if water.HistoryCount == 0 {
			return os.Truncate(path, 0)
		}
		return nil
	}
	if size <= water.HistoryBytes {
		// 文件比 head 记录更短 = 外部损坏，交给 verify/巡检报出，这里不猜。
		return nil
	}
	return os.Truncate(path, int64(water.HistoryBytes))
}

// readStackHead 读取该 kind 的 stack 模块 head（缺失 = 空水位，不报错）。
func (journal *jsonStackJournal) readStackHead(key Key, kind StackKind) (stackModuleHead, error) {
	store := journal.store
	begin := time.Now()
	headFile, err := store.readModuleHeadFile(key, moduleForStackKind(kind))
	store.stackStats().headIO.Add(time.Since(begin).Nanoseconds())
	if errors.Is(err, fs.ErrNotExist) {
		return stackModuleHead{SessionID: key.SessionID}, nil
	}
	if err != nil {
		return stackModuleHead{}, err
	}
	return decodeHeadPayload[stackModuleHead](headFile)
}

// anchor 返回当前已发布 message 坐标。优先用 message 提交时发布的内存锚：
// 栈提交因此不再打开 metadata/message.json —— 在 Windows 上那会把「读者持柄
// → rename 发布失败」的跨模块窗口从栈链路上去掉。
func (journal *jsonStackJournal) anchor(key Key) (string, uint64) {
	store := journal.store
	if anchor := store.messageAnchorCached(key); anchor != nil {
		return anchor.messageID, anchor.seq
	}
	begin := time.Now()
	head, err := store.readMessageHead(key)
	store.stackStats().headIO.Add(time.Since(begin).Nanoseconds())
	if err != nil {
		return "", 0
	}
	store.rememberMessageAnchor(key, head)
	return head.LastMessageID, head.LastSeq
}

// watermark 返回 message LRU 水位（fork 起点判定，I7）。
func (journal *jsonStackJournal) watermark(key Key) (uint64, error) {
	retention, err := journal.store.readRetentionHead(key)
	if err != nil {
		return 0, err
	}
	return retention.WatermarkSeq, nil
}

// recordEvents 把状态迁移写进 EVENT 通道（plan.*/task.*/goal.*）。EVENT 不
// 参与装配（I4），失败不回撤销已发布的栈 head。
func (journal *jsonStackJournal) recordEvents(key Key, kind StackKind, mutation StackMutation) error {
	events := stackTransitionEvents(kind, mutation)
	if len(events) == 0 {
		return nil
	}
	store := journal.store
	return timeSection(&store.stackStats().eventIO, func() error {
		_, err := store.structuralEventCommit(key, stackMutationCommitID(kind, mutation), events)
		return err
	})
}

// dropCache 丢弃该会话的三份读投影（删除会话 / fork 覆盖子会话后）。
func (journal *jsonStackJournal) dropCache(key Key) {
	journal.store.dropStackViews(key)
}

// stats 返回 JSON 后端的延迟归因。
func (journal *jsonStackJournal) stats() stackJournalStats {
	return journal.store.stackStats().snapshot(string(BackendJSON))
}

// ---------- JSONL 行编解码（§2.4 数据文件形状） ----------

// readStackRowsFile 读取栈 JSONL（跳过空行与崩溃残尾），只保留
// revision ≤ headSeq 的行（head 是发布点）。
func readStackRowsFile(path string, headSeq uint64) ([]StackItemRecord, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return []StackItemRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	rows := decodeStackRows(data)
	out := make([]StackItemRecord, 0, len(rows))
	for _, row := range rows {
		if row.Revision == 0 || row.Revision > headSeq {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func decodeStackRows(data []byte) []StackItemRecord {
	segments := bytes.Split(data, []byte{'\n'})
	rows := make([]StackItemRecord, 0, len(segments))
	for index, segment := range segments {
		if index == len(segments)-1 && len(bytes.TrimSpace(segment)) > 0 {
			continue // 崩溃残尾（未换行收尾）
		}
		segment = bytes.TrimSpace(segment)
		if len(segment) == 0 {
			continue
		}
		var row StackItemRecord
		if json.Unmarshal(segment, &row) != nil {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

func encodeStackRows(rows []StackItemRecord) []byte {
	buffer := make([]byte, 0, len(rows)*128)
	for _, row := range rows {
		data, err := json.Marshal(row)
		if err != nil {
			continue
		}
		buffer = append(buffer, data...)
		buffer = append(buffer, '\n')
	}
	return buffer
}

// appendStackRows 以 append-only 方式追加归档行（崩溃残尾先截掉）。
func appendStackRows(path string, rows []StackItemRecord) error {
	if len(rows) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
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
	if _, err := file.Write(encodeStackRows(rows)); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func fileSizeOrZero(path string) (uint64, error) {
	stat, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return uint64(stat.Size()), nil
}

// cloneStackRows 复制条目行：读者与写者拿到的都是独立副本，投影不被就地改。
func cloneStackRows(rows []StackItemRecord) []StackItemRecord {
	if len(rows) == 0 {
		return nil
	}
	return append([]StackItemRecord(nil), rows...)
}

func cloneStackWatermarks(kinds map[StackKind]stackWatermark) map[StackKind]stackWatermark {
	out := make(map[StackKind]stackWatermark, len(kinds))
	for kind, water := range kinds {
		out[kind] = water
	}
	return out
}

// stackStats 返回会话存储布局的栈通道诊断累加器（newStoreEngine 创建）。
func (store *storeEngine) stackStats() *stackStats { return store.stack }

// stackViewLoad 返回该会话该 kind 的已发布读投影；未装载时返回 nil。
func (store *storeEngine) stackViewLoad(key Key, kind StackKind) *stackView {
	locks := store.locks(key)
	return locks.stackViews[stackKindIndex(kind)].Load()
}

// stackViewPublish 发布新的读投影（只在提交临界区内调用 → 单写者）。
func (store *storeEngine) stackViewPublish(key Key, kind StackKind, view *stackView) {
	locks := store.locks(key)
	locks.stackViews[stackKindIndex(kind)].Store(view)
}

// dropStackViews 丢弃该会话全部栈读投影（目录被删除/替换后必须作废）。
func (store *storeEngine) dropStackViews(key Key) {
	locks := store.locks(key)
	for index := range locks.stackViews {
		locks.stackViews[index].Store(nil)
	}
}

// dropSessionCaches 作废该会话的全部内存读投影（删除会话、换后端、fork 覆盖
// 子目录后调用）；读投影是「本进程写者发布的」，目录一旦不在就必须一起作废。
func (store *storeEngine) dropSessionCaches(key Key) {
	store.dropStackViews(key)
	store.forgetMessageAnchor(key)
}
