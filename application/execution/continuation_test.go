package execution

// continuation_test.go — 自动续跑与 ⏸/⟳ 控制语义的用例。
//
// 每条用例对应 docs/arch/goal-auto-continue.md 的一条硬判据（编号写在小标题里）。
// 用例跑在 continuationHost 这个"会话存档 + goal 栈 + 回合"的最小骨架上：
// 存档是 append-only 的 model.ContinuationAttempt 列表（与
// SessionRecord.Execution.Continuations 同口径），Attempts / Continued / Truncated
// 全部从它派生——不是测试自己攒的旁路账，所以"重启后仍不续/仍不双发"是真的
// 从记录读回来的结论。

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
	model "github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/limit"
)

const continuationTestSession = "session-a"

// continuationHost 是最小真实骨架：存档（paused / stoppedByUser / continuations）
// + goal 栈（goalID 空 = 已收口）+ 当前回合的终态与 requestID。
type continuationHost struct {
	mu     sync.Mutex
	clock  time.Time
	limits limit.ContinuationLimits

	goalID     string
	goalTitle  string
	turnStatus dto.TurnStatus
	requestID  string
	reason     string

	stoppedByUser bool
	paused        bool
	records       []model.ContinuationAttempt

	running       bool
	submitted     []string
	audits        []ContinuationAudit
	requestSeq    int
	freezeRequest bool // submit 不改 requestID：模拟"提交成功但回合没起"
}

func newContinuationHost() *continuationHost {
	return &continuationHost{
		clock:      time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC),
		limits:     limit.ContinuationLimits{Enabled: true, MaxAttemptsPerGoal: 3},
		goalID:     "goal-1",
		goalTitle:  "验收自动续跑",
		turnStatus: dto.TurnInterrupted,
		requestID:  "req-1",
		reason:     ContinuationReasonInterrupted,
	}
}

// application 每次调用都造一个新实例：同一个 host 造两个实例 = 模拟进程重启
// （进程内幂等账清空，只有存档读回）。
func (h *continuationHost) application() *ContinuationApplication {
	return NewContinuation(ContinuationDeps{
		Facts: h.factsFor,
		SessionIdle: func(string) bool {
			h.mu.Lock()
			defer h.mu.Unlock()
			return !h.running
		},
		CurrentRequestID: func(string) string {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.requestID
		},
		SubmitConversation: h.submit,
		RecordAttempt: func(_ string, attempt model.ContinuationAttempt) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.records = append(h.records, attempt)
		},
		SetPaused: func(_ string, paused bool, _ time.Time) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.paused = paused
		},
		Audit: func(_ string, entry ContinuationAudit) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.audits = append(h.audits, entry)
		},
		Limits: func() limit.ContinuationLimits { return h.limits },
		Now: func() time.Time {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.clock
		},
	})
}

func (h *continuationHost) factsFor(string) ContinuationFacts {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.factsLocked()
}

// factsLocked 的派生口径与真实存档适配器一致（见 model.ContinuationAttempt 注释）。
func (h *continuationHost) factsLocked() ContinuationFacts {
	facts := ContinuationFacts{
		GoalID:        h.goalID,
		GoalTitle:     h.goalTitle,
		TurnStatus:    h.turnStatus,
		RequestID:     h.requestID,
		Reason:        h.reason,
		StoppedByUser: h.stoppedByUser,
		Paused:        h.paused,
		Continued:     map[string]bool{},
		Truncated:     map[string]bool{},
	}
	attempts := 0
	var lastAttempt time.Time
	for _, record := range h.records {
		if record.GoalID != h.goalID {
			continue
		}
		key := continuationKey(record.GoalID, record.RequestID)
		switch {
		case record.Truncated:
			// 截断没发出续跑：只记止步痕，不挡人工恢复。
			facts.Truncated[key] = true
		case record.Manual && record.Attempt <= 0:
			// 人工恢复的**清零标记**（Attempt=0）：计数从这里重新数。
			attempts = 0
			lastAttempt = time.Time{}
		case record.Attempt <= 0:
			// 没有任何意义的记录（不计数、不挡续跑）。
		default:
			// 真发过一次续跑（Manual=true 表示这一次是 ⟳ 触发的）。
			facts.Continued[key] = true
			attempts++
			lastAttempt = record.At
		}
	}
	facts.Attempts = attempts
	facts.LastAttemptAt = lastAttempt
	return facts
}

func (h *continuationHost) submit(_ context.Context, _ string, prompt string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.submitted = append(h.submitted, prompt)
	if h.freezeRequest {
		return nil
	}
	h.requestSeq++
	h.requestID = fmt.Sprintf("req-auto-%d", h.requestSeq)
	h.turnStatus = dto.TurnProgressing
	h.running = true
	return nil
}

// interrupt 模拟"一轮被硬中断"：终态 TurnInterrupted + 新的 requestID + 会话空闲。
func (h *continuationHost) interrupt(requestID, reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.turnStatus = dto.TurnInterrupted
	h.requestID = requestID
	h.reason = reason
	h.running = false
}

func (h *continuationHost) setRunning(running bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.running = running
	if running {
		h.turnStatus = dto.TurnProgressing
	}
}

func (h *continuationHost) advance(delta time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clock = h.clock.Add(delta)
}

func (h *continuationHost) submitCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.submitted)
}

func (h *continuationHost) prompt(index int) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.submitted[index]
}

func (h *continuationHost) recordCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.records)
}

func (h *continuationHost) auditsOf(kind string) []ContinuationAudit {
	h.mu.Lock()
	defer h.mu.Unlock()
	var matched []ContinuationAudit
	for _, entry := range h.audits {
		if entry.Kind == kind {
			matched = append(matched, entry)
		}
	}
	return matched
}

func (h *continuationHost) countTruncated() int {
	count := 0
	for _, entry := range h.auditsOf("goal.continue") {
		if entry.Truncated {
			count++
		}
	}
	return count
}

// ①-a + ⑤：goal 未收口 + 中断 → 续跑一次；同一中断点（含重启后）不再续。
func TestContinuationFiresOnceForInterruptedOpenGoal(t *testing.T) {
	host := newContinuationHost()
	application := host.application()
	triggered := map[string]struct{}{}
	if pending := application.TriggerGoalContinuations(continuationTestSession, triggered); pending != 0 {
		t.Fatalf("pending = %d, want 0", pending)
	}
	if got := host.submitCount(); got != 1 {
		t.Fatalf("续跑提交数 = %d, want 1", got)
	}
	prompt := host.prompt(0)
	for _, want := range []string{"goal-1", "验收自动续跑", "req-1", "goal_done"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("续跑输入缺少 %q：%q", want, prompt)
		}
	}
	continuations := host.auditsOf("goal.continue")
	if len(continuations) != 1 {
		t.Fatalf("goal.continue 打点数 = %d, want 1", len(continuations))
	}
	if continuations[0].Truncated || continuations[0].Manual {
		t.Fatalf("常规续跑被打成了截断/人工恢复：%+v", continuations[0])
	}
	if host.recordCount() != 1 {
		t.Fatalf("续跑记录数 = %d, want 1", host.recordCount())
	}

	// ⑤ 同一中断点被再次观测到（进程内账还在）：不许双发。
	host.interrupt("req-1", ContinuationReasonInterrupted)
	application.TriggerGoalContinuations(continuationTestSession, triggered)
	// ⑤ 重启（新实例 + 空进程内账，只读存档）：仍然不许双发。
	var fresh map[string]struct{}
	host.application().TriggerGoalContinuations(continuationTestSession, fresh)
	if got := host.submitCount(); got != 1 {
		t.Fatalf("同一中断点被续了 %d 次，want 1", got)
	}
}

// ①-b：goal 已 goal_done（栈上没有 active 帧）→ 一次都不续。
func TestContinuationSkipsClosedGoal(t *testing.T) {
	host := newContinuationHost()
	host.mu.Lock()
	host.goalID = ""
	host.goalTitle = ""
	host.mu.Unlock()
	application := host.application()
	if pending := application.TriggerGoalContinuations(continuationTestSession, map[string]struct{}{}); pending != 0 {
		t.Fatalf("pending = %d, want 0", pending)
	}
	if got := host.submitCount(); got != 0 {
		t.Fatalf("goal 已收口却续跑了 %d 次", got)
	}
	if host.recordCount() != 0 || len(host.auditsOf("goal.continue")) != 0 {
		t.Fatal("goal 已收口却留了续跑账")
	}
	// 横条也不该出现。
	if status := application.Status(continuationTestSession); status.Stalled {
		t.Fatalf("goal 已收口，横条不该 stalled：%+v", status)
	}
}

// ①-c：用户显式停止（人工意图优先）→ 不续，且重启后仍不续。
func TestContinuationSkipsUserStoppedTurn(t *testing.T) {
	host := newContinuationHost()
	host.mu.Lock()
	host.stoppedByUser = true
	host.mu.Unlock()
	host.application().TriggerGoalContinuations(continuationTestSession, map[string]struct{}{})
	// 重启：新实例 + 空进程内账，停止位从存档读回。
	host.application().TriggerGoalContinuations(continuationTestSession, map[string]struct{}{})
	if got := host.submitCount(); got != 0 {
		t.Fatalf("用户停止后仍续跑了 %d 次", got)
	}
	if host.recordCount() != 0 {
		t.Fatal("用户停止后仍写了续跑记录")
	}
}

// ① 防呆：TurnBlocked / TurnNeedsUserDecision 是"等人"，不是"被中断"。
func TestContinuationSkipsWaitingForUserOrExternal(t *testing.T) {
	for _, status := range []dto.TurnStatus{dto.TurnBlocked, dto.TurnNeedsUserDecision, dto.TurnCompleted, dto.TurnFailed} {
		host := newContinuationHost()
		host.mu.Lock()
		host.turnStatus = status
		host.mu.Unlock()
		host.application().TriggerGoalContinuations(continuationTestSession, map[string]struct{}{})
		if got := host.submitCount(); got != 0 {
			t.Fatalf("终态 %v 被当成「被中断」续跑了 %d 次", status, got)
		}
	}
}

// C4：忙会话不打断、不塞队列，但欠账要报出去（pending=1 让定时器再来）。
func TestContinuationStaysPendingWhileSessionBusy(t *testing.T) {
	host := newContinuationHost()
	// 忙但上一轮仍是"被中断"（排队输入/待审批在等）：这才有"欠一次续跑"。
	host.setRunning(true)
	host.mu.Lock()
	host.turnStatus = dto.TurnInterrupted
	host.mu.Unlock()
	application := host.application()
	if pending := application.TriggerGoalContinuations(continuationTestSession, map[string]struct{}{}); pending != 1 {
		t.Fatalf("pending = %d, want 1", pending)
	}
	if got := host.submitCount(); got != 0 {
		t.Fatalf("忙会话被插了 %d 次续跑", got)
	}
	if host.recordCount() != 0 {
		t.Fatal("忙会话却记了续跑账")
	}
	host.interrupt("req-2", ContinuationReasonProvider)
	application.TriggerGoalContinuations(continuationTestSession, map[string]struct{}{})
	if got := host.submitCount(); got != 1 {
		t.Fatalf("空闲后续跑数 = %d, want 1", got)
	}
}

// ② 暂停语义：置 paused 后当前这一轮照常跑完（不被打断），
//
//	但轮结束后不再自动发继续。两半都要证。
func TestPauseKeepsRunningTurnAndStopsNextContinuation(t *testing.T) {
	host := newContinuationHost()
	application := host.application()
	host.setRunning(true) // 当前这一轮正在跑

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	release := make(chan struct{})
	turnEnded := make(chan string, 1)
	go func() {
		// 这一轮只在自己的活儿干完时结束；没有任何人取消它。
		select {
		case <-release:
			turnEnded <- "completed"
		case <-ctx.Done():
			turnEnded <- "canceled"
		}
	}()

	if status := application.Pause(continuationTestSession); !status.Paused {
		t.Fatalf("⏸ 没落到存档：%+v", status)
	}
	select {
	case result := <-turnEnded:
		t.Fatalf("暂停打断了当前这一轮（%s）：暂停 ≠ 中断", result)
	case <-time.After(20 * time.Millisecond):
	}
	if ctx.Err() != nil {
		t.Fatal("暂停路径取消了回合 ctx：暂停必须只是停止后继续跑")
	}
	host.mu.Lock()
	stopped := host.stoppedByUser
	host.mu.Unlock()
	if stopped {
		t.Fatal("暂停被写成了用户停止：两条判据语义不同（C3 vs 暂停位）")
	}
	if got := len(host.auditsOf("goal.pause")); got != 1 {
		t.Fatalf("goal.pause 打点数 = %d, want 1", got)
	}

	// 第一半：这一轮照常跑完。
	close(release)
	if result := <-turnEnded; result != "completed" {
		t.Fatalf("当前这一轮没有跑完：%s", result)
	}

	// 第二半：轮结束后被中断，暂停位在 → 不再自动续。
	host.interrupt("req-2", ContinuationReasonInterrupted)
	if pending := application.TriggerGoalContinuations(continuationTestSession, map[string]struct{}{}); pending != 0 {
		t.Fatalf("暂停期间 pending = %d, want 0（不该挂欠账）", pending)
	}
	if got := host.submitCount(); got != 0 {
		t.Fatalf("暂停之后仍然自动续跑了 %d 次", got)
	}
	if host.recordCount() != 0 {
		t.Fatal("暂停期间写了续跑记录")
	}
}

// ③ 继续语义：清 paused 并立即续一次，且只发一次（不与定时策略叠加双发）。
func TestResumeFiresExactlyOnceAndDoesNotStack(t *testing.T) {
	host := newContinuationHost()
	application := host.application()
	host.mu.Lock()
	host.paused = true
	host.mu.Unlock()
	host.interrupt("req-2", ContinuationReasonProvider)
	if got := host.submitCount(); got != 0 {
		t.Fatalf("暂停期间续跑了 %d 次", got)
	}

	status := application.Resume(continuationTestSession)
	if status.Paused {
		t.Fatalf("⟳ 没清暂停位：%+v", status)
	}
	if got := host.submitCount(); got != 1 {
		t.Fatalf("⟳ 之后提交数 = %d, want 1（必须立即续一次）", got)
	}
	resumes := host.auditsOf("goal.resume")
	if len(resumes) != 1 || !resumes[0].Manual {
		t.Fatalf("人工恢复打点 = %+v, want 一条 goal.resume(manual)", resumes)
	}

	// 只发一次：同一中断点被重新观测到 + 紧接着的定时扫描 + 重启后的新实例，
	// 三路都不许再发。
	host.interrupt("req-2", ContinuationReasonProvider)
	application.TriggerGoalContinuations(continuationTestSession, map[string]struct{}{})
	host.application().TriggerGoalContinuations(continuationTestSession, map[string]struct{}{})
	if got := host.submitCount(); got != 1 {
		t.Fatalf("人工恢复与定时策略叠加成了双发：提交数 = %d, want 1", got)
	}
}

// ④ 上限：连续中断时续跑次数被截断，且有打点标明被截断（止步痕只记一次）。
func TestContinuationTruncatesAtAttemptLimit(t *testing.T) {
	host := newContinuationHost()
	host.mu.Lock()
	host.limits = limit.ContinuationLimits{Enabled: true, MaxAttemptsPerGoal: 2}
	host.mu.Unlock()
	application := host.application()
	for round := 1; round <= 2; round++ {
		host.interrupt(fmt.Sprintf("req-%d", round), ContinuationReasonProvider)
		application.TriggerGoalContinuations(continuationTestSession, map[string]struct{}{})
	}
	if got := host.submitCount(); got != 2 {
		t.Fatalf("上限内的续跑数 = %d, want 2", got)
	}
	// 第 3 次中断：被上限截断。
	host.interrupt("req-3", ContinuationReasonProvider)
	application.TriggerGoalContinuations(continuationTestSession, map[string]struct{}{})
	if got := host.submitCount(); got != 2 {
		t.Fatalf("触顶后仍在续跑：提交数 = %d, want 2", got)
	}
	truncated := host.countTruncated()
	if truncated != 1 {
		t.Fatalf("截断打点数 = %d, want 1", truncated)
	}
	for _, entry := range host.auditsOf("goal.continue") {
		if entry.Truncated && !strings.Contains(entry.Detail, "上限") {
			t.Fatalf("截断打点没说清原因：%q", entry.Detail)
		}
	}
	// 同一次中断反复观测（重复事件 / 重启）：止步痕不刷屏，也不再续。
	application.TriggerGoalContinuations(continuationTestSession, map[string]struct{}{})
	host.application().TriggerGoalContinuations(continuationTestSession, map[string]struct{}{})
	if got := host.countTruncated(); got != 1 {
		t.Fatalf("截断打点被重复记了 %d 次", got)
	}
	if got := host.submitCount(); got != 2 {
		t.Fatalf("触顶后重启又续跑了：提交数 = %d", got)
	}
}

// ④ 退避：第 2 次续跑前要等够 BackoffSec，期间只报 pending，不发。
func TestContinuationBackoffDelaysNextAttempt(t *testing.T) {
	host := newContinuationHost()
	host.mu.Lock()
	host.limits = limit.ContinuationLimits{
		Enabled: true, MaxAttemptsPerGoal: 5, BackoffSec: 30, MaxBackoffSec: 120,
	}
	// 已经续过一次（req-1，就在此刻），现在又断在 req-2。
	host.records = append(host.records, model.ContinuationAttempt{
		At: host.clock, GoalID: host.goalID, RequestID: "req-1", Attempt: 1,
	})
	host.mu.Unlock()
	host.interrupt("req-2", ContinuationReasonProvider)
	application := host.application()
	if pending := application.TriggerGoalContinuations(continuationTestSession, map[string]struct{}{}); pending != 1 {
		t.Fatalf("退避期内 pending = %d, want 1", pending)
	}
	if got := host.submitCount(); got != 0 {
		t.Fatalf("退避期内还是发了 %d 次续跑", got)
	}
	host.advance(31 * time.Second)
	if pending := application.TriggerGoalContinuations(continuationTestSession, map[string]struct{}{}); pending != 0 {
		t.Fatalf("退避到期后 pending = %d, want 0", pending)
	}
	if got := host.submitCount(); got != 1 {
		t.Fatalf("退避到期后提交数 = %d, want 1", got)
	}
}

// ④ + ③：resume 可重置计数（必须留一条人工恢复打点），清零后同一 goal 又能
// 自动续到新上限；触顶后的"继续"也能把同一中断点再试一次。
func TestResumeResetsAttemptCounterAfterTruncation(t *testing.T) {
	host := newContinuationHost()
	host.mu.Lock()
	host.limits = limit.ContinuationLimits{Enabled: true, MaxAttemptsPerGoal: 2}
	host.mu.Unlock()
	application := host.application()
	host.interrupt("req-1", ContinuationReasonProvider)
	application.TriggerGoalContinuations(continuationTestSession, map[string]struct{}{})
	host.interrupt("req-2", ContinuationReasonProvider)
	application.TriggerGoalContinuations(continuationTestSession, map[string]struct{}{})
	host.interrupt("req-3", ContinuationReasonProvider)
	application.TriggerGoalContinuations(continuationTestSession, map[string]struct{}{})
	if got := host.submitCount(); got != 2 {
		t.Fatalf("触顶前续跑数 = %d, want 2", got)
	}
	if !application.Status(continuationTestSession).Truncated {
		t.Fatal("横条该显示已触顶（Truncated）")
	}

	// ⟳：清暂停位（本来没暂停）+ 计数清零 + 立刻把同一中断点再试一次。
	application.Resume(continuationTestSession)
	if got := host.submitCount(); got != 3 {
		t.Fatalf("人工恢复没有立即续跑：提交数 = %d, want 3", got)
	}
	if got := len(host.auditsOf("goal.resume")); got != 1 {
		t.Fatalf("人工恢复打点数 = %d, want 1", got)
	}

	// 计数被清零后，自动策略又有额度了：⟳ 那一次已占新账的第 1 次，
	// 所以 req-4 是第 2 次（也是新账的最后一次），req-5 起再次触顶。
	host.interrupt("req-4", ContinuationReasonProvider)
	application.TriggerGoalContinuations(continuationTestSession, map[string]struct{}{})
	if got := host.submitCount(); got != 4 {
		t.Fatalf("清零后自动续跑数 = %d, want 4（req-3 人工 + req-4 自动）", got)
	}
	host.interrupt("req-5", ContinuationReasonProvider)
	application.TriggerGoalContinuations(continuationTestSession, map[string]struct{}{})
	host.interrupt("req-6", ContinuationReasonProvider)
	application.TriggerGoalContinuations(continuationTestSession, map[string]struct{}{})
	if got := host.submitCount(); got != 4 {
		t.Fatalf("新一轮触顶后仍在续跑：提交数 = %d, want 4", got)
	}
	// 每个**新的**中断点各留一条截断痕（req-3 与 req-5 各一条；同一中断点
	// 反复观测不会再刷屏，见 TestContinuationTruncatesAtAttemptLimit）。
	if got := host.countTruncated(); got != 3 {
		t.Fatalf("截断打点数 = %d, want 3（req-3/req-5/req-6 各一条止步痕）", got)
	}
}

// 开关零值 = 关：一次都不扫，也不写任何账；⏸/⟳ 也不落位。
func TestContinuationOffMeansOff(t *testing.T) {
	host := newContinuationHost()
	host.mu.Lock()
	host.limits = limit.ContinuationLimits{}
	host.mu.Unlock()
	application := host.application()
	if pending := application.TriggerGoalContinuations(continuationTestSession, map[string]struct{}{}); pending != 0 {
		t.Fatalf("关闭时 pending = %d, want 0", pending)
	}
	if got := host.submitCount(); got != 0 {
		t.Fatalf("关闭时续跑了 %d 次", got)
	}
	if host.recordCount() != 0 || len(host.auditsOf("goal.continue")) != 0 {
		t.Fatal("关闭时仍写了续跑账")
	}
	if status := application.Pause(continuationTestSession); status.Paused {
		t.Fatal("关闭时 ⏸ 落了位")
	}
	if status := application.Resume(continuationTestSession); status.Paused || len(host.auditsOf("goal.resume")) != 0 {
		t.Fatal("关闭时 ⟳ 写了打点")
	}
}

// "提交成功"不等于"真的起了一个回合"：这种情况不落账、不计数，留给下一次。
func TestContinuationDoesNotCountTurnThatNeverStarted(t *testing.T) {
	host := newContinuationHost()
	host.mu.Lock()
	host.freezeRequest = true
	host.mu.Unlock()
	application := host.application()
	if pending := application.TriggerGoalContinuations(continuationTestSession, map[string]struct{}{}); pending != 1 {
		t.Fatalf("回合没起来时 pending = %d, want 1（该挂欠账）", pending)
	}
	if host.recordCount() != 0 {
		t.Fatalf("回合没起来却记了 %d 条续跑账", host.recordCount())
	}
	if len(host.auditsOf("goal.continue")) != 0 {
		t.Fatal("回合没起来却打了 goal.continue")
	}
	host.mu.Lock()
	host.freezeRequest = false
	host.mu.Unlock()
	application.TriggerGoalContinuations(continuationTestSession, map[string]struct{}{})
	if host.recordCount() != 1 {
		t.Fatalf("真起了回合后记录数 = %d, want 1", host.recordCount())
	}
}

// stalled 横条的读面：未收口 + 被中断 → Stalled；暂停/触顶/收口都不该显示 stalled。
func TestContinuationStatusReflectsStalledAndPause(t *testing.T) {
	host := newContinuationHost()
	application := host.application()
	status := application.Status(continuationTestSession)
	if !status.Stalled || status.Paused || status.GoalID != "goal-1" || status.RequestID != "req-1" {
		t.Fatalf("横条读面不对：%+v", status)
	}
	if status.MaxAttempts != 3 {
		t.Fatalf("横条上限 = %d, want 3", status.MaxAttempts)
	}
	if paused := application.Pause(continuationTestSession); paused.Stalled || !paused.Paused {
		t.Fatalf("暂停后横条读面不对：%+v", paused)
	}
}
