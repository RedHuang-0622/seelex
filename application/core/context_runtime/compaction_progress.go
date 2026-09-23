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
	// CompactionGateFrame 渲染压缩帧正文（前端可按区间回读的那份）。
	CompactionGateFrame = "frame"
	// CompactionGateStore 帧正文进会话内容存储，得到 frame_ref。
	CompactionGateStore = "store"
	// CompactionGateRecord 写压缩记录并翻转视图修订。
	CompactionGateRecord = "record"
)

// CompactionGates 是门禁的权威顺序（进度条据此画格子）。
var CompactionGates = []string{
	CompactionGateJudge,
	CompactionGateAssemble,
	CompactionGateReplace,
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
	// note 是本轮的非门禁事实（例如「被纪元节流：只折叠、不落记录」），拼进
	// settle 的 Detail。终局必须能自答「进度条走完了，为什么没有记录」——否则
	// 用户只能看到 ran 到 replace 的进度条然后什么都没有，合理地怀疑后端没接线。
	note string
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
	payload := event.CompactionProgress{
		State:     event.CompactionProgressRunning,
		Gate:      id,
		Index:     index,
		Total:     CompactionGateTotal(),
		Version:   p.version,
		Origin:    p.origin,
		Detail:    detail,
		ElapsedMS: p.elapsedLocked(),
	}
	p.mu.Unlock()
	p.publish(payload)
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
