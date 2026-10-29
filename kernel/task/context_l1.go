package task_context

import (
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/context/prefixprobe"
)

// ContextL1Snapshot 是一次已装配 provider 前缀的进程内派生快照。
//
// L1 不属于事实源（I5）：Messages 来自已定稿 transcript 的 wire 投影，
// ThroughSeq/EventCount 是这份投影最后消费到的 truth 水位。键相同且水位连续时，
// 下一次装配只转换新增事件；任何缺口、压缩纪元变化或冷加载后的空快照都显式
// 重建。快照不持久化，也不能被当成恢复 truth 的替代品。
type ContextL1Snapshot struct {
	Key          prefixprobe.Key
	System       string
	Messages     []contract.EngineMessage
	RetainedFrom int

	ProjectVersion string
	MemoryEpoch    uint64
	CompactSeq     uint64
	ToolsEpoch     uint64

	// ThroughSeq/EventCount 是快照最后消费到的 transcript 事件序号与数量。
	// 二者共同防止"序号有空洞但看起来只多了一条"的错误追加。
	ThroughSeq uint64
	EventCount int

	// TokenGeneration 是估算器校准代数。校准变化后旧 token 基数不再可信，
	// 必须重建；TokenGeneration=0 表示不检查代数（兼容测试桩）。
	TokenGeneration uint64
	// ReuseBaseMessages 是最近一次 resume 前的消息数。引擎可能在 L1 之后还持有
	// 与 transcript 记录形态不同的本轮 wire 消息（例如大 tool 参数），调用方
	// 只需按这个边界比较增量，无需重扫完整前缀。
	ReuseBaseMessages int
	// Tokens 是 system + Messages + tools 的请求估算；TruthTokens 是 Messages
	// 对应的真值历史估算。两者都包含 CountRequest 的固定 3 token 底数。
	Tokens      int
	TruthTokens int
}

// Clone 深拷贝消息切片；L1 对外只给派生数据副本，调用方不能回写快照。
func (snapshot ContextL1Snapshot) Clone() ContextL1Snapshot {
	out := snapshot
	out.Messages = append([]contract.EngineMessage(nil), snapshot.Messages...)
	for index := range out.Messages {
		out.Messages[index].ToolCalls = append([]contract.EngineToolCall(nil), snapshot.Messages[index].ToolCalls...)
	}
	return out
}

// Chars 返回 L1 的字符量，用于日志与探针同口径归因。
func (snapshot ContextL1Snapshot) Chars() int {
	total := prefixprobe.CountChars(snapshot.System)
	for _, message := range snapshot.Messages {
		total += prefixprobe.CountChars(message.Role) + prefixprobe.CountChars(message.Content)
	}
	return total
}

// ContextL1Stats 是 L1 派生缓存的累计观测。
type ContextL1Stats struct {
	Hits            int
	Misses          int
	Rebuilt         int
	ReusedEvents    int
	ReusedChars     int64
	LastReusedChars int64
	RebuiltMessages int
	Last            string
	LastChanged     []string
	LastKey         prefixprobe.Key
}

// Hit 报告最近一次装配是否复用了同一前缀身份。
func (stats ContextL1Stats) Hit() bool { return stats.Last == "hit" }

// Terse 生成一行打点，供日志、报表与测试共用。
func (stats ContextL1Stats) Terse() string {
	last := stats.Last
	if last == "" {
		last = "none"
	}
	return fmt.Sprintf("l1=%s hits=%d misses=%d rebuilt=%d reused_events=%d reused_chars=%d last_reused_chars=%d rebuilt_messages=%d changed=[%s] key{%s}",
		last, stats.Hits, stats.Misses, stats.Rebuilt, stats.ReusedEvents,
		stats.ReusedChars, stats.LastReusedChars, stats.RebuiltMessages,
		strings.Join(stats.LastChanged, ","), stats.LastKey.Terse())
}

// RecordContextL1 登记一次从 truth 重建设配结果。只有上一条快照不存在时才计入
// Rebuilt；同一键但水位不连续仍是一次 miss，由调用方显式记录。
func (coordinator *Coordinator) RecordContextL1(sessionID string, snapshot ContextL1Snapshot) ContextL1Stats {
	coordinator.stateMu.Lock()
	defer coordinator.stateMu.Unlock()
	runtime := coordinator.sessionStateLocked(sessionID)
	stats := runtime.l1Stats
	stats.LastKey = snapshot.Key
	stats.LastChanged = nil
	stats.LastReusedChars = 0
	if runtime.l1 == nil {
		stats.Misses++
		stats.Rebuilt++
		stats.RebuiltMessages += len(snapshot.Messages)
		stats.Last = "miss"
	} else {
		stats.Misses++
		stats.RebuiltMessages += len(snapshot.Messages)
		stats.Last = "miss"
		stats.LastChanged = prefixprobe.DiffKeys(runtime.l1.Key, snapshot.Key)
		if len(stats.LastChanged) == 0 {
			stats.LastChanged = []string{"truth_watermark"}
		}
	}
	clone := snapshot.Clone()
	runtime.l1 = &clone
	runtime.l1Stats = stats
	return stats
}

// ResumeContextL1 尝试在连续水位上只转换新增 transcript 事件。
//
// 返回 false 表示调用方必须从 truth[retained_from:] 重建。false 覆盖：
// 键不同、token 校准代数变化、快照水位不在当前事件流中、事件数量回退。函数
// 不修改 runtime；只有完整通过校验后才一次性提交新快照与命中统计。
func (coordinator *Coordinator) ResumeContextL1(
	sessionID string,
	key prefixprobe.Key,
	events []model.TranscriptEvent,
	retainedFrom int,
	tokenGeneration uint64,
) (ContextL1Snapshot, bool) {
	coordinator.stateMu.Lock()
	defer coordinator.stateMu.Unlock()
	runtime := coordinator.sessionStateLocked(sessionID)
	previous := runtime.l1
	if previous == nil || previous.Key != key || previous.RetainedFrom != retainedFrom {
		return ContextL1Snapshot{}, false
	}
	if tokenGeneration != 0 && previous.TokenGeneration != 0 && tokenGeneration != previous.TokenGeneration {
		return ContextL1Snapshot{}, false
	}
	if previous.EventCount > len(events) || previous.ThroughSeq > 0 && len(events) == 0 {
		return ContextL1Snapshot{}, false
	}
	boundary := previous.EventCount - 1
	if previous.ThroughSeq > 0 {
		boundary = -1
		for index, event := range events {
			if event.Seq == previous.ThroughSeq {
				boundary = index
				break
			}
		}
		if boundary < 0 || boundary+1 != previous.EventCount {
			return ContextL1Snapshot{}, false
		}
	}
	delta := events[boundary+1:]
	next := previous.Clone()
	next.ReuseBaseMessages = len(previous.Messages)
	deltaMessages := TranscriptEventMessages(delta)
	next.Messages = append(next.Messages, deltaMessages...)
	if len(deltaMessages) > 0 {
		deltaTokens := coordinator._CountRequestTokens("", deltaMessages, "", nil) - 3
		if deltaTokens > 0 {
			next.Tokens += deltaTokens
			next.TruthTokens += deltaTokens
		}
		next.ThroughSeq = delta[len(delta)-1].Seq
	}
	next.EventCount = len(events)
	next.TokenGeneration = tokenGeneration
	clone := next.Clone()
	runtime.l1 = &clone
	reusedChars := int64(previous.Chars())
	runtime.l1Stats.Hits++
	runtime.l1Stats.ReusedEvents += len(delta)
	runtime.l1Stats.ReusedChars += reusedChars
	runtime.l1Stats.LastReusedChars = reusedChars
	runtime.l1Stats.Last = "hit"
	runtime.l1Stats.LastChanged = nil
	runtime.l1Stats.LastKey = key
	return next, true
}

// ContextCacheViewFor 把 L1 命中统计投影成前端 DTO。这里没有 provider
// cached_tokens：数值只回答“本地 L1 复用了多少已装配前缀”。
func (coordinator *Coordinator) ContextCacheViewFor(sessionID string) *dto.ContextCacheView {
	coordinator.stateMu.Lock()
	defer coordinator.stateMu.Unlock()
	runtime := coordinator.sessionStateLocked(sessionID)
	stats := runtime.l1Stats
	if stats.Last == "" {
		return nil
	}
	key := stats.LastKey
	return &dto.ContextCacheView{
		Available:       true,
		Status:          stats.Last,
		Hits:            stats.Hits,
		Misses:          stats.Misses,
		Rebuilt:         stats.Rebuilt,
		ReusedEvents:    stats.ReusedEvents,
		ReusedChars:     stats.ReusedChars,
		LastReusedChars: stats.LastReusedChars,
		LastChanged:     append([]string(nil), stats.LastChanged...),
		PrefixKey: dto.ContextPrefixKeyView{
			SystemDigest: key.SystemDigest, ProjectVersion: key.ProjectVersion,
			MemoryEpoch: key.MemoryEpoch, CompactSeq: key.CompactSeq,
			ToolsEpoch: key.ToolsEpoch, ToolsDigest: key.ToolsDigest,
			RetainedFrom: key.RetainedFrom,
		},
	}
}

// ContextL1For 返回当前快照副本（未装配过 → false）。
func (coordinator *Coordinator) ContextL1For(sessionID string) (ContextL1Snapshot, bool) {
	coordinator.stateMu.Lock()
	defer coordinator.stateMu.Unlock()
	snapshot := coordinator.sessionStateLocked(sessionID).l1
	if snapshot == nil {
		return ContextL1Snapshot{}, false
	}
	return snapshot.Clone(), true
}

// ContextL1StatsFor 返回 L1 命中统计（未装配过 → 零值）。
func (coordinator *Coordinator) ContextL1StatsFor(sessionID string) ContextL1Stats {
	coordinator.stateMu.Lock()
	defer coordinator.stateMu.Unlock()
	return coordinator.sessionStateLocked(sessionID).l1Stats
}

// ForgetContextL1For 丢弃进程内派生快照与统计。冷加载/卸载后下一次装配从
// truth 重建；该操作不触碰 transcript、压缩栈或任何持久事实。
func (coordinator *Coordinator) ForgetContextL1For(sessionID string) {
	coordinator.stateMu.Lock()
	defer coordinator.stateMu.Unlock()
	runtime := coordinator.sessionStateLocked(sessionID)
	runtime.l1 = nil
	runtime.l1Stats = ContextL1Stats{}
}
