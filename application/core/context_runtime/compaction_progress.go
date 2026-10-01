package context_runtime

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/application/event"
)

// 压缩门禁 id：一轮折叠从「判据估算」到「写压缩记录」实际经过的关口。顺序 =
// prepareExecutionContextFor 里的执行顺序，不是愿望清单：每关都在代码里真实
// 收口（见 coordinator.go 的 progress.gate 调用点），因此进度条走的每一步都有
// 对应的事实发生。id 是协议字面量，前端文案在 compaction-format.js（同一处
// 只放一种语言），两侧由配对测试互钉。
const (
	// CompactionGateJudge 达峰判定：token 计数 + 折叠判据 + 纪元节流。
	CompactionGateJudge = "judge"
	// CompactionGateAssemble 装配：保留窗口截断、历史拟合、必要时自主折叠。
	CompactionGateAssemble = "assemble"
	// CompactionGateReplace 替换 provider 历史并归一化引擎缓存。
	CompactionGateReplace = "replace"
	// CompactionGateStackPush 把这次折出的区间推进会话压缩栈（见
	// CompactionIndexPort）：拿到 segment_id 才算给模型留了 read_compressed_turn
	// 这一跳。索引面未装配或推帧失败也收口——降级必须留痕，不能静默跳过。
	// id 字面量是 "index"（协议字面量），常量名避开 CompactionGateIndex 那个
	// 序号查询函数。
	CompactionGateStackPush = "index"
	// CompactionGateFrame 渲染压缩帧正文（前端可按区间回读的那份）。
	CompactionGateFrame = "frame"
	// CompactionGateStore 帧正文进会话内容存储，得到 frame_ref。
	CompactionGateStore = "store"
	// CompactionGateRecord 写压缩记录并翻转视图修订。
	CompactionGateRecord = "record"
)

// CompactionGateTiming 是一关的实测耗时（门禁 id + 本关毫秒数）。
//
// 它同时供两处消费，且**只有这一个来源**：进度事件（ElapsedMS）与压缩回执里的
// 逐关清单。回执与进度条分开各测一次，同一关就会有两种说法（"进度条说 33ms、
// 回执说 4ms"），正是这个仓库反复防的那类漂移。
//
// 门禁的中文名不在这里：id 是跨语言协议字面量，文案表只在前端
// compaction-format.js 的 compactionGateLabels（同一处只放一种语言，键序由
// TestFrontendGateLabelsMatchBackendOrder 互钉）。回执只报 id 与毫秒。
type CompactionGateTiming struct {
	Gate      string
	ElapsedMS int
}

// CompactionGates 是门禁的权威顺序（进度条据此画格子）。
var CompactionGates = []string{
	CompactionGateJudge,
	CompactionGateAssemble,
	CompactionGateReplace,
	CompactionGateStackPush,
	CompactionGateFrame,
	CompactionGateStore,
	CompactionGateRecord,
}

// CompactionGateTotal 返回一轮压缩的门禁总数（进度条分母）。
func CompactionGateTotal() int { return len(CompactionGates) }

// CompactionGateIndex 返回门禁在权威顺序里的序号（1 起）；未知 id 返回 0。
func CompactionGateIndex(gate string) int {
	for index, candidate := range CompactionGates {
		if candidate == gate {
			return index + 1
		}
	}
	return 0
}

// compactionProgress 是一轮压缩的进度发射器：一条折叠只对应一个实例，
// 由 prepareExecutionContextFor 在判定要折叠后创建，函数返回时收口。
//
// 三件事由类型保证，而不是靠调用点自觉：
//   - 终局恰好一条（settled 幂等门）——少一条，前端进度条永远停在半途；
//   - Detail 只带该关的数字事实，标题文案留在前端，避免一句话两个来源；
//   - 发布走会话路由 + revision=0（载荷不进快照，同 team.changed 口径）。
//
// 计时也由类型保证：每帧带**刚刚过去那一段**的毫秒数（ElapsedMS），起手帧为 0。
// 压缩整轮往往只有几十毫秒（一次 token 估算 + 一次装配），"哪一关慢"只有逐关
// 计时才看得见；不记这个，界面上就只剩一条瞬时满格的进度条，读者合理地认为
// "什么都没发生"。
//
// 可以在 ViewMu 内调用：hub 的投递端是非阻塞 select（缓冲满即记入重放窗口并
// 排空投递 resync.required），不会回调进 core，因此不构成锁嵌套。
type compactionProgress struct {
	hub       event.SessionAwareHub
	sessionID string
	requestID string

	mu      sync.Mutex
	version uint64
	origin  string
	reached int
	settled bool
	// last 是上一帧的发出时刻（逐帧计时的基准）。
	last time.Time
	// timings 是已收口的每一关及其耗时。它不只是"给进度条看的"：显式路径的
	// 调用方（/compact 回执）要能把"这一轮走了哪几关、各花了多久"如实带回用户，
	// 否则回执只能说"已压缩"，用户看不出慢在哪一关。
	timings []CompactionGateTiming
	// note 是本轮的非门禁事实（例如「被纪元节流：只折叠、不落记录」），拼进
	// settle 的 Detail。终局必须能自答「进度条走完了，为什么没有记录」——否则
	// 用户只能看到 ran 到 replace 的进度条然后什么都没有，合理地怀疑后端没接线。
	note string
	// outcome 是本轮"没有落记录"时的**结果分类**（空 → settle 按 folded_without_record
	// 兜底）。纪元节流与「没有模型读后感所以不折」是两种完全不同的终局：前端文案
	// 不该把后者读成"折叠了，只是没记"。
	outcome string
}

// startCompactionProgress 开启一轮门禁进度。没有会话路由键或宿主不支持按会话
// 发布时返回 nil——进度面是可选观测，绝不为它伪造归属。
func (c *Coordinator) startCompactionProgress(sessionID, requestID string, version uint64, origin string) *compactionProgress {
	if c == nil || sessionID == "" {
		return nil
	}
	hub, ok := c.Events.(event.SessionAwareHub)
	if !ok {
		return nil
	}
	now := time.Now()
	return &compactionProgress{
		hub: hub, sessionID: sessionID, requestID: requestID,
		version: version, origin: origin, last: now,
	}
}

// begin 发**起手帧**（见 event.CompactionPhaseBegin）：显式压缩在动第一个重活
// 之前就把"这一轮已经开始"送到界面，Index=0 表示还没有任何一关收口——不预支
// 进度，只如实说明"现在在判据估算"（这正是即将发生的那一步）。
func (p *compactionProgress) begin() {
	if p == nil {
		return
	}
	p.mu.Lock()
	payload := event.CompactionProgress{
		State:   event.CompactionProgressRunning,
		Phase:   event.CompactionPhaseBegin,
		Gate:    CompactionGateJudge,
		Index:   0,
		Total:   CompactionGateTotal(),
		Version: p.version,
		Origin:  p.origin,
	}
	p.mu.Unlock()
	p.publish(payload)
}

// gate 通告「第 index 关收口」。未知 id 也发（序号 0 会被形状测试抓到），
// 但先把 id 归零，避免把一个拼错的门禁静默画成进度条上的一格。
func (p *compactionProgress) gate(id, detail string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	index := CompactionGateIndex(id)
	if index == 0 {
		id = ""
	} else if index > p.reached {
		p.reached = index
	}
	elapsed := p.elapsedLocked()
	if index > 0 {
		// 先把耗时记进本轮清单，再发帧：回执与进度条读同一份数字。
		p.timings = append(p.timings, CompactionGateTiming{Gate: id, ElapsedMS: elapsed})
	}
	payload := event.CompactionProgress{
		State:     event.CompactionProgressRunning,
		Gate:      id,
		Index:     index,
		Total:     CompactionGateTotal(),
		Version:   p.version,
		Origin:    p.origin,
		Detail:    detail,
		ElapsedMS: elapsed,
	}
	p.mu.Unlock()
	p.publish(payload)
}

// GateTimings 返回本轮已收口门禁的实测耗时（副本）。调用点在本轮收口之后
// （prepareExecutionContextFor 的出栈 deferred settle 之后），因此读到的是一份
// 完整清单；返回副本是为了不把内部切片借给调用方。
func (p *compactionProgress) GateTimings() []CompactionGateTiming {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.timings) == 0 {
		return nil
	}
	out := make([]CompactionGateTiming, len(p.timings))
	copy(out, p.timings)
	return out
}

// elapsedLocked 返回距上一帧的毫秒数并推进计时基准。调用方持锁。
func (p *compactionProgress) elapsedLocked() int {
	now := time.Now()
	elapsed := int(now.Sub(p.last).Milliseconds())
	p.last = now
	return elapsed
}

// setVersion 在自主压缩另开新纪元时校正本轮版本：判定关拿到的版本号可能还是
// 旧的，进度条不能对到别的压缩记录上。
func (p *compactionProgress) setVersion(version uint64) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.version = version
	p.mu.Unlock()
}

// skip 记下「本轮折叠了但不落记录」的原因（settle 时拼进 Detail）。没有它，读者
// 只能看到一个走到 replace 的进度条然后什么都没有。
func (p *compactionProgress) skip(reason string) {
	if p == nil {
		return
	}
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return
	}
	p.mu.Lock()
	p.note = trimmed
	p.mu.Unlock()
}

// skipOutcome 记下本轮"没有落记录"的结果分类（settle 时优先于默认的
// folded_without_record）。callers 用它把「没有模型读后感所以不折」与「被纪元
// 节流」分开：前者什么都没动，后者折了上下文只是没记。
func (p *compactionProgress) skipOutcome(outcome CompactOutcome) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.outcome = string(outcome)
	p.mu.Unlock()
}

// settle 收口本轮：err 非空即失败终局（Outcome 带真实原因），否则按是否落了
// 压缩记录给出 compacted / folded_without_record。幂等——重复调用只发一条。
func (p *compactionProgress) settle(err error, recorded bool, outcome string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.settled {
		p.mu.Unlock()
		return
	}
	p.settled = true
	total := CompactionGateTotal()
	detail := fmt.Sprintf("reached=%d/%d", p.reached, total)
	if p.note != "" {
		detail += " " + p.note
	}
	state := event.CompactionProgressDone
	if err != nil {
		state = event.CompactionProgressFailed
		outcome = err.Error()
	} else if outcome == "" {
		if recorded {
			outcome = string(CompactDone)
		} else if p.outcome != "" {
			outcome = p.outcome
		} else {
			outcome = string(CompactFoldedUnrecorded)
		}
	}
	payload := event.CompactionProgress{
		State:     state,
		Index:     total,
		Total:     total,
		Version:   p.version,
		Origin:    p.origin,
		Detail:    detail,
		Outcome:   outcome,
		ElapsedMS: p.elapsedLocked(),
	}
	p.mu.Unlock()
	p.publish(payload)
}

func (p *compactionProgress) publish(payload event.CompactionProgress) {
	p.hub.PublishSession(event.EventCompactionProgress, 0, p.requestID, p.sessionID, payload)
}
