// Package record 维护**工具调用记录**的内存读数：一个会话一份"工具名 → 成功/失败/
// 失败原因"的累计。
//
// 生态位：application/core 在工具完成段判出这次是成功还是失败（判据在 core 的
// `toolStatusFailed` / `toolRecordReason`），把 (工具名, 成败, 失败原因) 交给本包累加；
// **落盘归会话存储**（`store` 的 `tool-record.jsonl`），由 core 在**真相源提交点**
// 批量更新（默认草稿物化进真相源那一刻，见 core 的 flushToolRecord）——本包**不做任何 IO**。
//
// 为什么粒度是"一会话一份"而不是进程级一份（2026-10-15 用户口径）：
//   - 存储落在会话自己的目录里（跟项目作用域、会话生命周期走，回收会话即回收读数）；
//   - 落盘频率因此可以按"会话提交"批量算，而不是每次工具调用写一次盘
//     （进程级单文件 + 每次调用 append 会让磁盘一直忙）。
//
// 为什么是整份快照而不是事件流：落盘点本来就是批次，整份快照"取最后一行"即当前读数，
// 读侧不需要全量折叠，文件长度 = 批次数。
package record

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// Accumulator 是一个会话的工具记录累计（内存态；落盘走会话存储）。
//
// 零值不可用；用 NewAccumulator 构造。并发：单把锁（工具完成段是回合内高频点，
// 但也只在**每个工具调用结束时**进入一次）。
type Accumulator struct {
	mu     sync.Mutex
	stats  map[string]*dto.ToolRecordStat
	order  []string
	total  int
	lastAt map[string]time.Time
	// dirty 报告"上次落盘之后有没有新东西"。落到真相源时只写脏的那份，
	// 免得"每开一次会话都重写一遍同样的读数"。
	dirty bool
}

// NewAccumulator 构造一个空累计。
func NewAccumulator() *Accumulator {
	return &Accumulator{stats: map[string]*dto.ToolRecordStat{}, lastAt: map[string]time.Time{}}
}

// Record 累加一次调用（成功也记：没有分母的失败计数会把优化带偏）。
func (accumulator *Accumulator) Record(tool string, ok bool, reason string) {
	if accumulator == nil {
		return
	}
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return
	}
	at := time.Now().UTC()
	accumulator.mu.Lock()
	defer accumulator.mu.Unlock()
	stat, seen := accumulator.stats[tool]
	if !seen {
		stat = &dto.ToolRecordStat{Tool: tool}
		accumulator.stats[tool] = stat
		accumulator.order = append(accumulator.order, tool)
	}
	if ok {
		stat.Success++
	} else {
		stat.Failure++
		stat.Reasons = bumpReason(stat.Reasons, normalizeReason(reason))
		if at.After(accumulator.lastAt[tool]) {
			accumulator.lastAt[tool] = at
		}
	}
	stat.Total = stat.Success + stat.Failure
	stat.LastAt = accumulator.lastAt[tool]
	accumulator.total++
	accumulator.dirty = true
}

// Stats 返回全部工具的读数（按总调用次数降序；并列按首次出现序 ⇒ 读数确定）。
func (accumulator *Accumulator) Stats() []dto.ToolRecordStat {
	if accumulator == nil {
		return nil
	}
	accumulator.mu.Lock()
	defer accumulator.mu.Unlock()
	stats := make([]dto.ToolRecordStat, 0, len(accumulator.order))
	for _, tool := range accumulator.order {
		stat, ok := accumulator.stats[tool]
		if !ok {
			continue
		}
		stats = append(stats, cloneStat(*stat))
	}
	sort.SliceStable(stats, func(i, j int) bool { return stats[i].Total > stats[j].Total })
	return stats
}

// Stat 返回某工具当前的读数（不存在 = 零值 + false）。
func (accumulator *Accumulator) Stat(tool string) (dto.ToolRecordStat, bool) {
	if accumulator == nil {
		return dto.ToolRecordStat{}, false
	}
	accumulator.mu.Lock()
	defer accumulator.mu.Unlock()
	stat, ok := accumulator.stats[strings.TrimSpace(tool)]
	if !ok {
		return dto.ToolRecordStat{}, false
	}
	return cloneStat(*stat), true
}

// Totals 返回（总调用, 成功, 失败）。
func (accumulator *Accumulator) Totals() (int, int, int) {
	if accumulator == nil {
		return 0, 0, 0
	}
	accumulator.mu.Lock()
	defer accumulator.mu.Unlock()
	success, failure := 0, 0
	for _, stat := range accumulator.stats {
		success += stat.Success
		failure += stat.Failure
	}
	return accumulator.total, success, failure
}

// Snapshot 取一份整份读数快照（落盘用；同时把 dirty 清掉）。
//
// 返回 false = 没有新东西（调用方因此可以跳过这次写盘）。
func (accumulator *Accumulator) Snapshot() (dto.ToolRecordSnapshot, bool) {
	if accumulator == nil {
		return dto.ToolRecordSnapshot{}, false
	}
	accumulator.mu.Lock()
	stats := make([]dto.ToolRecordStat, 0, len(accumulator.order))
	for _, tool := range accumulator.order {
		stat, ok := accumulator.stats[tool]
		if !ok {
			continue
		}
		stats = append(stats, cloneStat(*stat))
	}
	dirty := accumulator.dirty
	accumulator.dirty = false
	accumulator.mu.Unlock()
	sort.SliceStable(stats, func(i, j int) bool { return stats[i].Total > stats[j].Total })
	return dto.ToolRecordSnapshot{At: time.Now().UTC(), Tools: stats}, dirty
}

// Load 用一份落盘读数**填充**空累计（冷启动/会话切回时读回）。
//
// 只填充不清零：进程内已经累加过的部分不能因为"读回旧快照"而丢——旧快照是子集，
// 按工具名逐格取**较大者**合并（同一进程内累计只会单调增长）。
func (accumulator *Accumulator) Load(snapshot dto.ToolRecordSnapshot) {
	if accumulator == nil {
		return
	}
	accumulator.mu.Lock()
	defer accumulator.mu.Unlock()
	for _, incoming := range snapshot.Tools {
		tool := strings.TrimSpace(incoming.Tool)
		if tool == "" {
			continue
		}
		stat, seen := accumulator.stats[tool]
		if !seen {
			stat = &dto.ToolRecordStat{Tool: tool}
			accumulator.stats[tool] = stat
			accumulator.order = append(accumulator.order, tool)
		}
		if incoming.Success > stat.Success {
			accumulator.total += incoming.Success - stat.Success
			stat.Success = incoming.Success
		}
		if incoming.Failure > stat.Failure {
			accumulator.total += incoming.Failure - stat.Failure
			stat.Failure = incoming.Failure
		}
		stat.Total = stat.Success + stat.Failure
		stat.Reasons = mergeReasons(stat.Reasons, incoming.Reasons)
		if incoming.LastAt.After(stat.LastAt) {
			stat.LastAt = incoming.LastAt
		}
		if incoming.LastAt.After(accumulator.lastAt[tool]) {
			accumulator.lastAt[tool] = incoming.LastAt
		}
	}
}

// Merge 把**一批增量读数**加进累计（阶梯提交的合并点：叶子暂存交上来的那一段）。
//
// 与 Load 的分工（两条语义，别混用）：
//   - `Load` 是"冷启动读回落盘快照"：同一份事实的子集 → 按工具名**取较大者**；
//   - `Merge` 是"叶子交上来的新一笔增量"：**相加**（seq 递增的两次提交都要计入），
//     失败原因也按条数相加。
//
// 判据仍在上游（成败与原因都算好才进来），本函数不判任何东西。
func (accumulator *Accumulator) Merge(stats []dto.ToolRecordStat) {
	if accumulator == nil || len(stats) == 0 {
		return
	}
	accumulator.mu.Lock()
	defer accumulator.mu.Unlock()
	for _, incoming := range stats {
		tool := strings.TrimSpace(incoming.Tool)
		if tool == "" {
			continue
		}
		stat, seen := accumulator.stats[tool]
		if !seen {
			stat = &dto.ToolRecordStat{Tool: tool}
			accumulator.stats[tool] = stat
			accumulator.order = append(accumulator.order, tool)
		}
		stat.Success += incoming.Success
		stat.Failure += incoming.Failure
		for _, reason := range incoming.Reasons {
			if reason.Count == 0 {
				continue
			}
			stat.Reasons = addReason(stat.Reasons, normalizeReason(reason.Reason), reason.Count)
		}
		stat.Total = stat.Success + stat.Failure
		if incoming.LastAt.After(stat.LastAt) {
			stat.LastAt = incoming.LastAt
		}
		if incoming.LastAt.After(accumulator.lastAt[tool]) {
			accumulator.lastAt[tool] = incoming.LastAt
		}
		accumulator.total += incoming.Success + incoming.Failure
		accumulator.dirty = true
	}
}

// normalizeReason 把外部给的 reason 归一到词表：认得才留，认不得一律 other（保守默认：
// 宁可归到"其它"，也不要让看板出现一个拼错的格子）。
func normalizeReason(reason string) string {
	trimmed := strings.ToLower(strings.TrimSpace(reason))
	for _, known := range dto.ToolReasonVocabulary() {
		if trimmed == known {
			return known
		}
	}
	return dto.ToolReasonOther
}

// bumpReason 给原因表加一笔。
func bumpReason(reasons []dto.ToolRecordReason, reason string) []dto.ToolRecordReason {
	return addReason(reasons, reason, 1)
}

// addReason 给原因表加 count 笔（合并增量时一批里同一原因可能有多笔）。
func addReason(reasons []dto.ToolRecordReason, reason string, count int) []dto.ToolRecordReason {
	if count <= 0 {
		return reasons
	}
	for index := range reasons {
		if reasons[index].Reason == reason {
			reasons[index].Count += count
			return reasons
		}
	}
	return append(reasons, dto.ToolRecordReason{Reason: reason, Count: count})
}

// mergeReasons 按原因取较大者合并（与 Load 的"只增不减"同源）。
func mergeReasons(current, incoming []dto.ToolRecordReason) []dto.ToolRecordReason {
	for _, candidate := range incoming {
		reason := strings.TrimSpace(candidate.Reason)
		if reason == "" {
			continue
		}
		replaced := false
		for index := range current {
			if current[index].Reason != reason {
				continue
			}
			if candidate.Count > current[index].Count {
				current[index].Count = candidate.Count
			}
			replaced = true
			break
		}
		if !replaced {
			current = append(current, dto.ToolRecordReason{Reason: reason, Count: candidate.Count})
		}
	}
	return current
}

// cloneStat 复制一格读数（含原因切片）：读面交出去的必须是快照，
// 否则调用方改了切片就改了累计的内部事实。
func cloneStat(stat dto.ToolRecordStat) dto.ToolRecordStat {
	if len(stat.Reasons) > 0 {
		stat.Reasons = append([]dto.ToolRecordReason(nil), stat.Reasons...)
	}
	return stat
}
