// 栈通道的 SQL 后端（SQLite / PostgreSQL 共用一套 DDL 与语义）。
//
// 与 JSON 后端的差异只在「发布点怎么实现」：
//   - JSON：先 append 数据 → 原子替换 metadata/stack.json，读者按 revision ≤
//     head 过滤；
//   - SQL：同一事务里写条目行 + 写 head 行，COMMIT 即发布点。事务天然满足
//     「数据先于 head 可见」，因此不需要 revision 过滤那层可见性判定（行仍
//     带 revision，用于 ER 保真与 verify 交叉校验）。
//
// 表形状按 my_design §3.1 ER 的 STACK_ITEM 逐字段建列（payload 为该域自有
// JSON；stack_id = kind|state）；head 只存水位（§2.0 规则 1）。
package sessionstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	stackItemsTable = "seelex_session_stack_item"
	stackHeadTable  = "seelex_session_stack_head"
	// structuralEventTable 是 v8 结构性 EVENT 的关系表（§2.4 状态迁移摘要）。
	structuralEventTable = "seelex_session_structural_event"
)

// sqlStackJournal 是栈通道的 SQL 后端。
// 锁与计量累加器挂在 repository 上：每次调用都会新建一个轻量 journal 值。
type sqlStackJournal struct {
	repository *sqlRepository
}

func (repository *sqlRepository) stackJournal() stackJournal {
	return &sqlStackJournal{repository: repository}
}

func (journal *sqlStackJournal) backend() string {
	if journal.repository.placeholder == "$" {
		return string(BackendPostgreSQL)
	}
	return string(BackendSQLite)
}

// lock 串行化同一会话的栈提交（head 行是跨 kind 共享的发布点）。等待时长单独
// 计量 → 与 JSON 后端同一口径回答「栈锁等了多久」。
func (journal *sqlStackJournal) lock(key Key) func() {
	stats := journal.repository.stack
	lock := journal.repository.stackLocks.get(key.ProjectID + "|" + key.SessionID)
	begin := time.Now()
	lock.Lock()
	stats.lockWait.Add(time.Since(begin).Nanoseconds())
	return lock.Unlock
}

// stats 返回 SQL 后端的延迟归因。
func (journal *sqlStackJournal) stats() stackJournalStats {
	return journal.repository.stack.snapshot(journal.backend())
}

// ensureStackSchema 建栈通道与结构性 EVENT 的表（幂等；由 Ping 调用）。
func (repository *sqlRepository) ensureStackSchema() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS ` + stackItemsTable + ` (
project_id TEXT NOT NULL, session_id TEXT NOT NULL, kind TEXT NOT NULL,
stack_id TEXT NOT NULL, seq BIGINT NOT NULL, revision BIGINT NOT NULL,
item_id TEXT NOT NULL, batch_id TEXT NOT NULL, status TEXT NOT NULL,
payload TEXT NOT NULL, item_message_id TEXT NOT NULL, item_message_seq BIGINT NOT NULL,
batch_message_from TEXT NOT NULL, batch_message_to TEXT NOT NULL,
batch_message_to_seq BIGINT NOT NULL, entered_at BIGINT NOT NULL, closed_at BIGINT NOT NULL,
PRIMARY KEY (project_id, session_id, kind, item_id))`,
		`CREATE INDEX IF NOT EXISTS seelex_stack_item_state
ON ` + stackItemsTable + ` (project_id, session_id, kind, status, seq)`,
		`CREATE TABLE IF NOT EXISTS ` + stackHeadTable + ` (
project_id TEXT NOT NULL, session_id TEXT NOT NULL, head_seq BIGINT NOT NULL,
kinds_json TEXT NOT NULL, updated_at BIGINT NOT NULL,
PRIMARY KEY (project_id, session_id))`,
		`CREATE TABLE IF NOT EXISTS ` + structuralEventTable + ` (
project_id TEXT NOT NULL, session_id TEXT NOT NULL, event_id BIGINT NOT NULL,
kind TEXT NOT NULL, anchor_message_id TEXT NOT NULL, anchor_seq BIGINT NOT NULL,
frame_id TEXT NOT NULL, commit_id TEXT NOT NULL, payload TEXT NOT NULL,
created_at BIGINT NOT NULL, PRIMARY KEY (project_id, session_id, event_id))`,
	}
	for _, statement := range statements {
		if _, err := repository.db.Exec(statement); err != nil {
			return fmt.Errorf("session storage: ensure stack schema: %w", err)
		}
	}
	return nil
}

// stackRowState 是条目行所在通道（ER STACK_ITEM 的 active/history 归属）。
const (
	stackRowActive  = "active"
	stackRowHistory = "history"
)

func (journal *sqlStackJournal) load(key Key, kind StackKind) (stackLoaded, error) {
	ctx, cancel := backgroundJournalContext()
	defer cancel()
	head, err := journal.readHead(ctx, key)
	if err != nil {
		return stackLoaded{}, err
	}
	var rows []StackItemRecord
	if err := timeSection(&journal.repository.stack.activeIO, func() error {
		var err error
		rows, err = journal.selectRows(ctx, key, kind, stackRowActive)
		return err
	}); err != nil {
		return stackLoaded{}, err
	}
	return stackLoaded{
		HeadSeq:      head.HeadSeq,
		Kinds:        cloneStackWatermarks(head.Kinds),
		Active:       rows,
		HistoryCount: head.Kinds[kind].HistoryCount,
	}, nil
}

func (journal *sqlStackJournal) readHistory(key Key, kind StackKind) ([]StackItemRecord, error) {
	ctx, cancel := backgroundJournalContext()
	defer cancel()
	var rows []StackItemRecord
	if err := timeSection(&journal.repository.stack.historyRead, func() error {
		var err error
		rows, err = journal.selectRows(ctx, key, kind, stackRowHistory)
		return err
	}); err != nil {
		return nil, err
	}
	return rows, nil
}

func (journal *sqlStackJournal) selectRows(ctx context.Context, key Key, kind StackKind, state string) ([]StackItemRecord, error) {
	query := `SELECT seq,revision,item_id,batch_id,status,payload,item_message_id,item_message_seq,` +
		`batch_message_from,batch_message_to,batch_message_to_seq,entered_at,closed_at` +
		` FROM ` + stackItemsTable + ` WHERE project_id=` + journal.repository.arg(1) +
		` AND session_id=` + journal.repository.arg(2) + ` AND kind=` + journal.repository.arg(3) +
		` AND stack_id=` + journal.repository.arg(4) + ` ORDER BY seq ASC`
	rows, err := journal.repository.db.QueryContext(ctx, query, key.ProjectID, key.SessionID, string(kind), string(kind)+"|"+state)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]StackItemRecord, 0)
	for rows.Next() {
		var (
			row            StackItemRecord
			payload        string
			enteredAt      int64
			closedAt       int64
			itemMessageSeq int64
			batchToSeq     int64
		)
		if err := rows.Scan(&row.Seq, &row.Revision, &row.ItemID, &row.BatchID, &row.Status,
			&payload, &row.ItemMessageID, &itemMessageSeq, &row.BatchMessageFrom, &row.BatchMessageTo,
			&batchToSeq, &enteredAt, &closedAt); err != nil {
			return nil, err
		}
		row.Kind = kind
		row.StackID = string(kind) + "|" + state
		row.Payload = json.RawMessage(payload)
		row.ItemMessageSeq = uint64(itemMessageSeq)
		row.BatchMessageToSeq = uint64(batchToSeq)
		row.EnteredAt = time.Unix(0, enteredAt).UTC()
		if closedAt != 0 {
			row.ClosedAt = time.Unix(0, closedAt).UTC()
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// publish 在一个事务内落条目行 + 发布 head（COMMIT = 发布点）。
func (journal *sqlStackJournal) publish(key Key, entry stackPublish) error {
	ctx, cancel := backgroundJournalContext()
	defer cancel()
	head, err := journal.readHead(ctx, key)
	if err != nil {
		return err
	}
	head.SessionID = key.SessionID
	head.HeadSeq = entry.Revision
	if head.Kinds == nil {
		head.Kinds = make(map[StackKind]stackWatermark)
	}
	head.Kinds[entry.Kind] = entry.Watermark
	kinds, err := json.Marshal(head.Kinds)
	if err != nil {
		return fmt.Errorf("session storage: encode stack kinds: %w", err)
	}
	transaction, err := journal.repository.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	repository := journal.repository
	stats := journal.repository.stack
	deleteActive := `DELETE FROM ` + stackItemsTable + ` WHERE project_id=` + repository.arg(1) +
		` AND session_id=` + repository.arg(2) + ` AND kind=` + repository.arg(3) + ` AND stack_id=` + repository.arg(4)
	if _, err := transaction.ExecContext(ctx, deleteActive, key.ProjectID, key.SessionID, string(entry.Kind), string(entry.Kind)+"|"+stackRowActive); err != nil {
		return err
	}
	upsert := `INSERT INTO ` + stackItemsTable + ` (project_id,session_id,kind,stack_id,seq,revision,item_id,batch_id,status,payload,item_message_id,item_message_seq,batch_message_from,batch_message_to,batch_message_to_seq,entered_at,closed_at) VALUES (` +
		repository.placeholders(17) + `) ON CONFLICT (project_id,session_id,kind,item_id) DO UPDATE SET ` +
		`stack_id=excluded.stack_id, seq=excluded.seq, revision=excluded.revision, batch_id=excluded.batch_id, ` +
		`status=excluded.status, payload=excluded.payload, item_message_id=excluded.item_message_id, ` +
		`item_message_seq=excluded.item_message_seq, batch_message_from=excluded.batch_message_from, ` +
		`batch_message_to=excluded.batch_message_to, batch_message_to_seq=excluded.batch_message_to_seq, ` +
		`entered_at=excluded.entered_at, closed_at=excluded.closed_at`
	for _, rows := range [][]StackItemRecord{entry.Archived, entry.Active} {
		for _, row := range rows {
			if _, err := transaction.ExecContext(ctx, upsert, stackRowArgs(key, row)...); err != nil {
				return err
			}
		}
	}
	headQuery := `INSERT INTO ` + stackHeadTable + ` (project_id,session_id,head_seq,kinds_json,updated_at) VALUES (` +
		repository.placeholders(5) + `) ON CONFLICT (project_id,session_id) DO UPDATE SET ` +
		`head_seq=excluded.head_seq, kinds_json=excluded.kinds_json, updated_at=excluded.updated_at`
	if _, err := transaction.ExecContext(ctx, headQuery, key.ProjectID, key.SessionID, int64(head.HeadSeq), string(kinds), time.Now().UTC().UnixNano()); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return err
	}
	stats.commits.Add(1)
	return nil
}

// stackRowArgs 按 stackItemsTable 列序展开一行条目（payload 原文存取）。
func stackRowArgs(key Key, row StackItemRecord) []any {
	state := stackRowActive
	if row.StackID == string(row.Kind)+"|"+stackRowHistory {
		state = stackRowHistory
	}
	if row.StackID == "" {
		row.StackID = string(row.Kind) + "|" + state
	}
	entered := row.EnteredAt
	if entered.IsZero() {
		entered = time.Now().UTC()
	}
	return []any{
		key.ProjectID, key.SessionID, string(row.Kind), row.StackID, int64(row.Seq), int64(row.Revision),
		row.ItemID, row.BatchID, row.Status, string(row.Payload), row.ItemMessageID, int64(row.ItemMessageSeq),
		row.BatchMessageFrom, row.BatchMessageTo, int64(row.BatchMessageToSeq),
		entered.UnixNano(), row.ClosedAt.UTC().UnixNano(),
	}
}

func (journal *sqlStackJournal) readHead(ctx context.Context, key Key) (stackModuleHead, error) {
	begin := time.Now()
	defer func() { journal.repository.stack.headIO.Add(time.Since(begin).Nanoseconds()) }()
	query := `SELECT head_seq,kinds_json FROM ` + stackHeadTable + ` WHERE project_id=` +
		journal.repository.arg(1) + ` AND session_id=` + journal.repository.arg(2)
	var (
		headSeq int64
		kinds   string
	)
	err := journal.repository.db.QueryRowContext(ctx, query, key.ProjectID, key.SessionID).Scan(&headSeq, &kinds)
	if errors.Is(err, sql.ErrNoRows) {
		return stackModuleHead{SessionID: key.SessionID}, nil
	}
	if err != nil {
		return stackModuleHead{}, err
	}
	head := stackModuleHead{SessionID: key.SessionID, HeadSeq: uint64(headSeq)}
	if kinds != "" {
		if err := json.Unmarshal([]byte(kinds), &head.Kinds); err != nil {
			return stackModuleHead{}, fmt.Errorf("session storage: decode stack kinds: %w", err)
		}
	}
	return head, nil
}

// anchor 返回当前已发布 message 坐标的 seq（= 消息行数）。
//
// SQL 后端的消息通道仍是整块 shard 快照（没有 v8 事件行键），因此锚只有 seq
// 维度、message_id 为空。fork 过滤按 seq（ItemMessageSeq/BatchMessageToSeq）
// 判定，与 id 无关；「v8 事件行落到 SQL 后端」是另一条待办（打点表 §4）。
func (journal *sqlStackJournal) anchor(key Key) (string, uint64) {
	ctx, cancel := backgroundJournalContext()
	defer cancel()
	total, err := journal.repository.messageCount(ctx, key)
	if err != nil || total < 0 {
		return "", 0
	}
	return "", uint64(total)
}

// watermark：SQL 后端尚未实现 v8 message 行与 LRU 淘汰 → 无水位下界。
func (journal *sqlStackJournal) watermark(key Key) (uint64, error) { return 0, nil }

// dropCache：SQL 后端没有内存读投影（读即查询）。
func (journal *sqlStackJournal) dropCache(key Key) {}

// recordEvents 把栈状态迁移追加进结构性 EVENT 表（I5 的第三步）。
func (journal *sqlStackJournal) recordEvents(key Key, kind StackKind, mutation StackMutation) error {
	events := stackTransitionEvents(kind, mutation)
	if len(events) == 0 {
		return nil
	}
	ctx, cancel := backgroundJournalContext()
	defer cancel()
	return timeSection(&journal.repository.stack.eventIO, func() error {
		return appendStructuralEventsSQL(ctx, journal.repository, key, "stack-"+randomID(), events)
	})
}

// appendStructuralEventsSQL 续号写入结构性 EVENT 行（调用方持会话栈锁）。
func appendStructuralEventsSQL(ctx context.Context, repository *sqlRepository, key Key, commitID string, events []structuralEvent) error {
	query := `SELECT COALESCE(MAX(event_id), 0) FROM ` + structuralEventTable + ` WHERE project_id=` +
		repository.arg(1) + ` AND session_id=` + repository.arg(2)
	var next int64
	if err := repository.db.QueryRowContext(ctx, query, key.ProjectID, key.SessionID).Scan(&next); err != nil {
		return err
	}
	insert := `INSERT INTO ` + structuralEventTable + ` (project_id,session_id,event_id,kind,anchor_message_id,anchor_seq,frame_id,commit_id,payload,created_at) VALUES (` +
		repository.placeholders(10) + `) ON CONFLICT (project_id,session_id,event_id) DO NOTHING`
	transaction, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	for _, event := range events {
		next++
		createdAt := event.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		if _, err := transaction.ExecContext(ctx, insert, key.ProjectID, key.SessionID, next, string(event.Kind),
			event.AnchorMessageID, int64(event.AnchorSeq), event.FrameID, commitID, string(event.Payload),
			createdAt.UTC().UnixNano()); err != nil {
			return err
		}
	}
	return transaction.Commit()
}

func maxInt(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
