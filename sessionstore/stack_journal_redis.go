// 栈通道的 Redis 后端。
//
// 键形状（沿用 sessionKey 的 {project} hash tag → 同一会话的键落在同一 slot，
// MULTI/EXEC 可用）：
//
//	<session>:stack:head            string  JSON(stackModuleHead)（只存水位）
//	<session>:stack:<kind>:active   list    JSON 条目行（当前投影）
//	<session>:stack:<kind>:history  list    JSON 条目行（append-only 归档）
//	<session>:structural-events     list    JSON 结构性 EVENT 行
//
// 发布点 = EXEC：条目行与新 head 在同一事务内生效，读者不会看到「数据已落、
// head 未发布」的中间态；revision 过滤仍保留（与 JSON 后端同一判据）。
package sessionstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisStackJournal 是栈通道的 Redis 后端。
type redisStackJournal struct {
	repository *redisRepository
}

func (repository *redisRepository) stackJournal() stackJournal {
	return &redisStackJournal{repository: repository}
}

func (journal *redisStackJournal) backend() string { return string(BackendRedis) }

func (journal *redisStackJournal) lock(key Key, _ StackKind) func() {
	stats := journal.repository.stack
	lock := journal.repository.stackLocks.get(key.ProjectID + "|" + key.SessionID)
	begin := time.Now()
	lock.Lock()
	stats.lockWait.Add(time.Since(begin).Nanoseconds())
	return lock.Unlock
}

// stats 返回 Redis 后端的延迟归因。
func (journal *redisStackJournal) stats() stackJournalStats {
	return journal.repository.stack.snapshot(journal.backend())
}

func (repository *redisRepository) stackHeadKey(key Key) string {
	return repository.sessionKey(key) + ":stack:head"
}

func (repository *redisRepository) stackRowsKey(key Key, kind StackKind, state string) string {
	return fmt.Sprintf("%s:stack:%s:%s", repository.sessionKey(key), string(kind), state)
}

func (repository *redisRepository) structuralEventKey(key Key) string {
	return repository.sessionKey(key) + ":structural-events"
}

func (journal *redisStackJournal) load(key Key, kind StackKind) (stackLoaded, error) {
	ctx, cancel := backgroundJournalContext()
	defer cancel()
	head, err := journal.readHead(ctx, key)
	if err != nil {
		return stackLoaded{}, err
	}
	rows, err := journal.readRows(ctx, key, kind, stackRowActive, head.HeadSeq)
	if err != nil {
		return stackLoaded{}, err
	}
	return stackLoaded{
		HeadSeq:      head.HeadSeq,
		Kinds:        cloneStackWatermarks(head.Kinds),
		Active:       rows,
		HistoryCount: head.Kinds[kind].HistoryCount,
		LastCommitID: head.Kinds[kind].LastCommitID,
	}, nil
}

func (journal *redisStackJournal) readHistory(key Key, kind StackKind) ([]StackItemRecord, error) {
	ctx, cancel := backgroundJournalContext()
	defer cancel()
	head, err := journal.readHead(ctx, key)
	if err != nil {
		return nil, err
	}
	return journal.readRows(ctx, key, kind, stackRowHistory, head.HeadSeq)
}

func (journal *redisStackJournal) readHead(ctx context.Context, key Key) (stackModuleHead, error) {
	begin := time.Now()
	data, err := journal.repository.client.Get(ctx, journal.repository.stackHeadKey(key)).Bytes()
	journal.repository.stack.headIO.Add(time.Since(begin).Nanoseconds())
	if errors.Is(err, redis.Nil) {
		return stackModuleHead{SessionID: key.SessionID}, nil
	}
	if err != nil {
		return stackModuleHead{}, err
	}
	var head stackModuleHead
	if err := json.Unmarshal(data, &head); err != nil {
		return stackModuleHead{}, fmt.Errorf("session storage: decode stack head: %w", err)
	}
	return head, nil
}

func (journal *redisStackJournal) readRows(ctx context.Context, key Key, kind StackKind, state string, headSeq uint64) ([]StackItemRecord, error) {
	begin := time.Now()
	values, err := journal.repository.client.LRange(ctx, journal.repository.stackRowsKey(key, kind, state), 0, -1).Result()
	if state == stackRowActive {
		journal.repository.stack.activeIO.Add(time.Since(begin).Nanoseconds())
	} else {
		journal.repository.stack.historyRead.Add(time.Since(begin).Nanoseconds())
	}
	if err != nil {
		return nil, err
	}
	out := make([]StackItemRecord, 0, len(values))
	for _, value := range values {
		var row StackItemRecord
		if err := json.Unmarshal([]byte(value), &row); err != nil {
			return nil, fmt.Errorf("session storage: decode stack row: %w", err)
		}
		if headSeq > 0 && (row.Revision == 0 || row.Revision > headSeq) {
			continue
		}
		row.Kind = kind
		row.StackID = string(kind) + "|" + state
		out = append(out, row)
	}
	return dedupeStackRowsByItemID(out), nil
}

// publish 在 MULTI/EXEC 内重建 active 列表、续写 history 列表并写入新 head。
func (journal *redisStackJournal) publish(key Key, entry stackPublish) error {
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
	headJSON, err := json.Marshal(head)
	if err != nil {
		return err
	}
	activeJSON, err := stackRowPayloads(entry.Active, string(entry.Kind)+"|"+stackRowActive)
	if err != nil {
		return err
	}
	historyJSON, err := stackRowPayloads(entry.Archived, string(entry.Kind)+"|"+stackRowHistory)
	if err != nil {
		return err
	}
	begin := time.Now()
	_, err = journal.repository.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		activeKey := journal.repository.stackRowsKey(key, entry.Kind, stackRowActive)
		pipe.Del(ctx, activeKey)
		if len(activeJSON) > 0 {
			pipe.RPush(ctx, activeKey, toRedisArgs(activeJSON)...)
		}
		if len(historyJSON) > 0 {
			pipe.RPush(ctx, journal.repository.stackRowsKey(key, entry.Kind, stackRowHistory), toRedisArgs(historyJSON)...)
		}
		pipe.Set(ctx, journal.repository.stackHeadKey(key), headJSON, 0)
		return nil
	})
	journal.repository.stack.activeIO.Add(time.Since(begin).Nanoseconds())
	if err == nil {
		journal.repository.stack.commits.Add(1)
	}
	return err
}

func stackRowPayloads(rows []StackItemRecord, stackID string) ([]string, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		row.StackID = stackID
		data, err := json.Marshal(row)
		if err != nil {
			return nil, fmt.Errorf("session storage: encode stack row: %w", err)
		}
		out = append(out, string(data))
	}
	return out, nil
}

func toRedisArgs(values []string) []any {
	args := make([]any, len(values))
	for index, value := range values {
		args[index] = value
	}
	return args
}

// anchor 返回当前已发布 message 坐标的 seq（= 消息行数）。Redis 后端的消息
// 通道仍是整块 shard 快照（无 v8 事件行键），因此锚只有 seq 维度。
func (journal *redisStackJournal) anchor(key Key) (string, uint64) {
	ctx, cancel := backgroundJournalContext()
	defer cancel()
	manifest, err := journal.repository.readManifest(ctx, key)
	if err != nil {
		return "", 0
	}
	total := sumShardCounts(manifest.HistoryShardCounts)
	if total < 0 {
		return "", 0
	}
	return "", uint64(total)
}

// watermark：Redis 后端尚未实现 v8 message 行与 LRU 淘汰 → 无水位下界。
func (journal *redisStackJournal) watermark(key Key) (uint64, error) { return 0, nil }

// dropCache：Redis 后端没有内存读投影（读即查询）。
func (journal *redisStackJournal) dropCache(key Key) {}

// recordEvents 把栈状态迁移追加进结构性 EVENT 列表。
func (journal *redisStackJournal) recordEvents(key Key, kind StackKind, mutation StackMutation) error {
	events := stackTransitionEvents(kind, mutation)
	if len(events) == 0 {
		return nil
	}
	ctx, cancel := backgroundJournalContext()
	defer cancel()
	begin := time.Now()
	count, err := journal.repository.client.LLen(ctx, journal.repository.structuralEventKey(key)).Result()
	defer func() { journal.repository.stack.eventIO.Add(time.Since(begin).Nanoseconds()) }()
	if err != nil {
		return err
	}
	payloads := make([]string, 0, len(events))
	commitID := stackMutationCommitID(kind, mutation)
	for _, event := range events {
		event.EventID = uint64(count) + uint64(len(payloads)) + 1
		event.CommitID = commitID
		if event.CreatedAt.IsZero() {
			event.CreatedAt = time.Now().UTC()
		}
		data, err := json.Marshal(event)
		if err != nil {
			return err
		}
		payloads = append(payloads, string(data))
	}
	return journal.repository.client.RPush(ctx, journal.repository.structuralEventKey(key), toRedisArgs(payloads)...).Err()
}

// deleteStackKeys 在删除会话时清掉栈通道与结构性 EVENT 键（由 Delete 调用）。
func (repository *redisRepository) deleteStackKeys(pipe redis.Pipeliner, ctx context.Context, key Key) {
	keys := []string{repository.stackHeadKey(key), repository.structuralEventKey(key)}
	for _, kind := range []StackKind{StackKindPlan, StackKindTask, StackKindGoal} {
		keys = append(keys,
			repository.stackRowsKey(key, kind, stackRowActive),
			repository.stackRowsKey(key, kind, stackRowHistory))
	}
	pipe.Del(ctx, keys...)
}
