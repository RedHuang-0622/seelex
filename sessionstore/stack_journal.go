// 栈通道的后端契约与共享提交逻辑（my_design §2.4/§3.1/§3.2/§4）。
//
// 分层：
//   - 通道语义（批次弹栈、水位、head 发布点、message 锚、EVENT 迁移、fork
//     按锚重建、verify）只在本文件与 stack_channel.go 实现一次；
//   - 后端只提供「载入 / 发布 / 锚 / 水位 / EVENT」五个动作：JSON 文件
//     （stack_journal_json.go）、SQL（stack_journal_sql.go）、Redis
//     （stack_journal_redis.go）。三种后端读写同一套 StackItemRecord 语义，
//     不再「只有 JSON 后端有栈」。
//
// 并发（§2.0 规则 2）：
//   - 写：同一会话的栈提交整段串行 —— metadata/stack.json 是跨 kind 共享的
//     发布点，head_seq 必须单调；提交以闭包形式在该临界区内执行，可变投影
//     只被这一个写者触碰，因此不存在数据竞争（actor 消息 = 闭包）；
//   - 读：JSON 后端读者读 actor 发布的不可变内存快照，既不等写锁也不打开文件
//     句柄（Windows 上「任何句柄都会让 rename 发布失败且零重试」）；
//   - EVENT 移出栈临界区：写序仍是 数据 → head → EVENT（I5），只是 EVENT 的
//     分片 IO 不再拉长栈锁的持有时间。
//
// 上下文：journal 接口不带 context。JSON 后端无 IO 上下文；SQL/Redis 后端在
// 自身实现里用 context.WithTimeout(context.Background(), stackJournalTimeout)
// 给每次调用兜底超时（Router 公开 API 引入 ctx 后再把取消传播接上）。
package sessionstore

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// stackJournalTimeout 是 SQL/Redis 后端单次通道调用的兜底超时。
const stackJournalTimeout = 5 * time.Second

// stackJournal 是栈通道后端契约。实现必须满足：
//  1. load 只返回 revision ≤ head_seq 的行（head 是提交发布点）；
//  2. publish 在同一模块内原子落「数据 + 新 head」；先数据后 head；
//  3. lock 串行化同一会话的整段提交（load → mutate → publish）；
//  4. recordEvents 在 publish 之后调用；失败不回撤销已发布的 head。
type stackJournal interface {
	// backend 返回后端标识（诊断与统计用）。
	backend() string
	// lock 串行化同一会话的栈提交，返回解锁函数。
	lock(key Key) func()
	// load 返回 head 水位与该 kind 已发布的 active 行（写路径载入）。
	load(key Key, kind StackKind) (stackLoaded, error)
	// readHistory 返回该 kind 的归档行（读路径与 fork 重建用；写路径不调用）。
	readHistory(key Key, kind StackKind) ([]StackItemRecord, error)
	// publish 落数据并发布新 head。
	publish(key Key, entry stackPublish) error
	// anchor 返回当前已发布 message 坐标（无行时 ""/0）。
	anchor(key Key) (string, uint64)
	// watermark 返回 message LRU 水位 seq（不支持行淘汰的后端返回 0）。
	watermark(key Key) (uint64, error)
	// recordEvents 把状态迁移写进 EVENT 通道（plan.*/task.*/goal.*）。
	recordEvents(key Key, kind StackKind, mutation StackMutation) error
	// dropCache 丢弃该会话的内存读投影（删除会话 / fork 覆盖后调用）。
	dropCache(key Key)
	// stats 返回该后端的延迟归因（锁等待与各数据域 IO）。
	stats() stackJournalStats
}

// stackLoaded 是写路径载入的投影。HistoryCount 取自 head 水位：写路径不读
// history 数据文件（归档文件是 append-only，只在读者与 fork 需要时解析）。
type stackLoaded struct {
	HeadSeq      uint64
	Kinds        map[StackKind]stackWatermark
	Active       []StackItemRecord
	HistoryCount uint64
}

// stackPublish 是一次已确定的栈变更：新投影 + 待追加的归档行 + 该 kind 新水位。
type stackPublish struct {
	Kind      StackKind
	Revision  uint64
	Active    []StackItemRecord
	Archived  []StackItemRecord
	Watermark stackWatermark
}

// stackCommit 是栈通道的唯一提交入口：载入 → 闭包变更 → 数据 → head → EVENT。
//
// mutate 闭包即 actor 消息：只有它返回非空迁移时才落盘；闭包内不碰文件，
// 因此后端 IO 失败不会留下半改状态（head 未发布 = 不可见，I5）。
func stackCommit(journal stackJournal, key Key, kind StackKind, mutate func(*stackState) (StackMutation, error)) (StackMutation, error) {
	if !validStackKind(kind) {
		return StackMutation{}, fmt.Errorf("session storage: unknown stack kind %q", kind)
	}
	published, mutation, err := stackCommitLocked(journal, key, kind, mutate)
	if err != nil || !published {
		return mutation, err
	}
	// 固定写序的最后一步：EVENT 在栈锁外写（栈 head 已发布即提交成功）。
	return mutation, journal.recordEvents(key, kind, mutation)
}

// stackCommitLocked 执行提交的临界区（load → mutate → publish）。返回
// published=false 表示闭包未产生迁移（未落盘、未发布 head）。
func stackCommitLocked(journal stackJournal, key Key, kind StackKind, mutate func(*stackState) (StackMutation, error)) (bool, StackMutation, error) {
	unlock := journal.lock(key)
	defer unlock()

	loaded, err := journal.load(key, kind)
	if err != nil {
		return false, StackMutation{}, err
	}
	anchorID, anchorSeq := journal.anchor(key)
	water := loaded.Kinds[kind]
	state := &stackState{
		revision:  loaded.HeadSeq + 1,
		nextSeq:   maxU64(water.HeadSeq, maxSeqOf(loaded.Active)),
		active:    loaded.Active,
		anchorID:  anchorID,
		anchorSeq: anchorSeq,
	}
	mutation, err := mutate(state)
	if err != nil {
		return false, StackMutation{}, err
	}
	if len(mutation.Pushed) == 0 && len(mutation.Updated) == 0 && len(mutation.Archived) == 0 {
		return false, mutation, nil
	}
	entry := stackPublish{
		Kind:     kind,
		Revision: state.revision,
		Active:   state.active,
		Archived: state.archived,
		Watermark: stackWatermark{
			HeadSeq:      maxU64(state.nextSeq, water.HeadSeq),
			ActiveCount:  len(state.active),
			HistoryCount: loaded.HistoryCount + uint64(len(state.archived)),
			OpenBatches:  state.openBatches(),
		},
	}
	if err := journal.publish(key, entry); err != nil {
		return false, StackMutation{}, err
	}
	mutation.Revision = state.revision
	return true, mutation, nil
}

// stackReadActive 返回该 kind 当前 active 投影（读者入口，后端无关）。
func stackReadActive(journal stackJournal, key Key, kind StackKind) ([]StackItemRecord, error) {
	if !validStackKind(kind) {
		return nil, fmt.Errorf("session storage: unknown stack kind %q", kind)
	}
	loaded, err := journal.load(key, kind)
	if err != nil {
		return nil, err
	}
	return loaded.Active, nil
}

// stackReadHistory 返回该 kind 的归档行（后端无关）。
func stackReadHistory(journal stackJournal, key Key, kind StackKind) ([]StackItemRecord, error) {
	if !validStackKind(kind) {
		return nil, fmt.Errorf("session storage: unknown stack kind %q", kind)
	}
	return journal.readHistory(key, kind)
}

// stackVerify 校验 head 水位与数据计数一致（M5 巡检的栈通道部分）。
func stackVerify(journal stackJournal, key Key) error {
	unlock := journal.lock(key)
	defer unlock()
	loaded, err := journal.load(key, StackKindPlan)
	if err != nil {
		return err
	}
	for _, kind := range []StackKind{StackKindPlan, StackKindTask, StackKindGoal} {
		water := loaded.Kinds[kind]
		active, err := stackReadActive(journal, key, kind)
		if err != nil {
			return err
		}
		if len(active) != water.ActiveCount {
			return fmt.Errorf("session storage: verify %s active=%d want %d", kind, len(active), water.ActiveCount)
		}
		history, err := journal.readHistory(key, kind)
		if err != nil {
			return err
		}
		if uint64(len(history)) != water.HistoryCount {
			return fmt.Errorf("session storage: verify %s history=%d want %d", kind, len(history), water.HistoryCount)
		}
	}
	return nil
}

// stackSnapshotAt 返回「message 坐标 ≤ fromSeq」时的栈快照（fork 重建，
// §8.1 + T-FK-02）：active 只留进入坐标 ≤ from 的条目，history 只留弹栈坐标
// ≤ from 的条目。空锚视为早于首行（保留）。
func stackSnapshotAt(journal stackJournal, key Key, kind StackKind, fromSeq uint64) (active []StackItemRecord, archived []StackItemRecord, err error) {
	if !validStackKind(kind) {
		return nil, nil, fmt.Errorf("session storage: unknown stack kind %q", kind)
	}
	loaded, err := journal.load(key, kind)
	if err != nil {
		return nil, nil, err
	}
	for _, row := range loaded.Active {
		if row.ItemMessageSeq == 0 || row.ItemMessageSeq <= fromSeq {
			active = append(active, row)
		}
	}
	history, err := journal.readHistory(key, kind)
	if err != nil {
		return nil, nil, err
	}
	for _, row := range history {
		if row.BatchMessageToSeq == 0 || row.BatchMessageToSeq <= fromSeq {
			archived = append(archived, row)
		}
	}
	return active, archived, nil
}

// stackRestoreSnapshot 把快照写进目标会话（fork 深拷贝）：归档条目续写归档
// 通道，活跃条目重建 active 投影；锚与 seq 原样保留、只重盖 revision。
func stackRestoreSnapshot(journal stackJournal, key Key, kind StackKind, active, archived []StackItemRecord) error {
	if len(active) == 0 && len(archived) == 0 {
		return nil
	}
	_, err := stackCommit(journal, key, kind, func(state *stackState) (StackMutation, error) {
		var mutation StackMutation
		for _, item := range archived {
			row := item
			row.Revision = state.revision
			row.Kind = kind
			row.StackID = string(kind) + "|history"
			if row.Seq > state.nextSeq {
				state.nextSeq = row.Seq
			}
			state.archived = append(state.archived, row)
			mutation.Archived = append(mutation.Archived, row)
		}
		for _, item := range active {
			row := item
			row.Revision = state.revision
			row.Kind = kind
			row.StackID = string(kind) + "|active"
			if row.Seq > state.nextSeq {
				state.nextSeq = row.Seq
			}
			if containsStackItem(state.active, row.ItemID) {
				continue
			}
			state.active = append(state.active, row)
			mutation.Pushed = append(mutation.Pushed, row)
		}
		return mutation, nil
	})
	return err
}

// stackFork 把父会话三栈按 message 锚重建进子会话（Router.ForkStacks 的
// 后端无关实现）。fromSeq 必须 ≥ 父会话 LRU watermark（I7）。
func stackFork(journal stackJournal, parentKey, childKey Key, fromSeq uint64) error {
	watermark, err := journal.watermark(parentKey)
	if err != nil {
		return err
	}
	if fromSeq < watermark {
		return ErrForkBeforeWatermark
	}
	journal.dropCache(childKey)
	for _, kind := range []StackKind{StackKindPlan, StackKindTask, StackKindGoal} {
		active, archived, err := stackSnapshotAt(journal, parentKey, kind, fromSeq)
		if err != nil {
			return err
		}
		if err := stackRestoreSnapshot(journal, childKey, kind, active, archived); err != nil {
			return err
		}
	}
	return nil
}

// keyedMutex 是「按会话键分片」的独占锁注册表（SQL/Redis 后端栈提交的单写者
// 保证；注册表短临界区只取锁指针，不参与 IO）。
type keyedMutex struct {
	registry sync.RWMutex
	locks    map[string]*sync.Mutex
}

func (registry *keyedMutex) get(id string) *sync.Mutex {
	registry.registry.RLock()
	lock := registry.locks[id]
	registry.registry.RUnlock()
	if lock != nil {
		return lock
	}
	registry.registry.Lock()
	defer registry.registry.Unlock()
	if registry.locks == nil {
		registry.locks = make(map[string]*sync.Mutex)
	}
	if lock = registry.locks[id]; lock != nil {
		return lock
	}
	lock = &sync.Mutex{}
	registry.locks[id] = lock
	return lock
}

// stackJournalStats 是栈通道的延迟归因快照：分别计量「栈锁等待」与各数据域
// IO，直接回答「延迟落在 active 还是 history」。
type stackJournalStats struct {
	Backend       string
	Commits       uint64
	LockWait      time.Duration
	ActiveIO      time.Duration
	HistoryAppend time.Duration
	HistoryRead   time.Duration
	HeadIO        time.Duration
	GuideIO       time.Duration
	EventIO       time.Duration
	ColdLoads     uint64
}

// stackStats 是各后端共用的累加器（构造时创建，字段自身为原子类型）。
type stackStats struct {
	commits       atomic.Uint64
	lockWait      atomic.Int64
	activeIO      atomic.Int64
	historyAppend atomic.Int64
	historyRead   atomic.Int64
	headIO        atomic.Int64
	guideIO       atomic.Int64
	eventIO       atomic.Int64
	coldLoads     atomic.Uint64
}

func (stats *stackStats) snapshot(backend string) stackJournalStats {
	return stackJournalStats{
		Backend:       backend,
		Commits:       stats.commits.Load(),
		LockWait:      time.Duration(stats.lockWait.Load()),
		ActiveIO:      time.Duration(stats.activeIO.Load()),
		HistoryAppend: time.Duration(stats.historyAppend.Load()),
		HistoryRead:   time.Duration(stats.historyRead.Load()),
		HeadIO:        time.Duration(stats.headIO.Load()),
		GuideIO:       time.Duration(stats.guideIO.Load()),
		EventIO:       time.Duration(stats.eventIO.Load()),
		ColdLoads:     stats.coldLoads.Load(),
	}
}

// timeSection 执行 fn 并把耗时（含其内部全部 IO）计入 counter。
func timeSection(counter *atomic.Int64, fn func() error) error {
	begin := time.Now()
	err := fn()
	counter.Add(time.Since(begin).Nanoseconds())
	return err
}

// backgroundJournalContext 给非 JSON 后端的单次调用兜底超时。
func backgroundJournalContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), stackJournalTimeout)
}
