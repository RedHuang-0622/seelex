package execution

// continuation_channel_test.go — **两条通道 + 两层状态机**的用例。
// 规格：docs/arch/goal-auto-continue.md §2.8（§2.8.0 状态机、§2.8.2 pull、§2.8.3 push、
// §2.8.7 验收）。
//
// 与 continuation_test.go 的分工：那一份证"判据 C1–C5 的推导"（单会话骨架）；
// 这一份证**形状**，三条都是它独有的：
//   - ① pull 通道：暂停期间本轮照常跑完，轮边界不再续；恢复 continue 后**下一次轮边界**才续；
//   - ② push 通道：对 teammate / subagent 发 paused 时在飞子会话被**立即**中断（不等轮边界），
//     同时主会话 goal 切到 doing/paused；
//   - ③ 状态是两层、粒度是会话：none/done 下层二无意义，换 goal 不残留，A/B 并发互不串扰。
//
// 因此骨架按 sessionID → 会话现场组织，并且带一个"在飞子会话"的模型（push 的中断对象）。
// 派生口径（Continued/Truncated/Attempts）与 continuation_test.go 的骨架逐字同源：
// 只从 append-only 记录算，不攒旁路账。

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

// channelChild 是一个**在飞的子会话**（teammate 角色回合 / subagent）。
// push 通道要做的就是让它当场被 cancel——不是等轮边界。
type channelChild struct {
	id         string
	ctx        context.Context
	cancel     context.CancelFunc
	canceledAt time.Time // 非零 = 在那个时刻被中断
}

// channelSession 是一个会话的现场（会话粒度的状态就住在这里）。
type channelSession struct {
	goalID        string
	goalStatus    dto.GoalRunStatus
	turnStatus    dto.TurnStatus
	requestID     string
	reason        string
	stoppedByUser bool
	paused        bool
	pausedGoalID  string
	records       []model.ContinuationAttempt
	running       bool
	submitted     []string
	children      []*channelChild
	requestSeq    int
}

type channelHost struct {
	mu       sync.Mutex
	clock    time.Time
	limits   limit.ContinuationLimits
	sessions map[string]*channelSession
	audits   []ContinuationAudit
}

func newChannelHost(sessionIDs ...string) *channelHost {
	host := &channelHost{
		clock:    time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC),
		limits:   limit.ContinuationLimits{Enabled: true, MaxAttemptsPerGoal: 3},
		sessions: map[string]*channelSession{},
	}
	for index, sessionID := range sessionIDs {
		host.sessions[sessionID] = &channelSession{
			goalID:     fmt.Sprintf("goal-%d", index+1),
			goalStatus: dto.GoalRunDoing,
			turnStatus: dto.TurnInterrupted,
			requestID:  fmt.Sprintf("req-%d", index+1),
			reason:     ContinuationReasonInterrupted,
		}
	}
	return host
}

func (h *channelHost) application() *ContinuationApplication {
	return NewContinuation(ContinuationDeps{
		Facts: h.factsFor,
		SessionIdle: func(sessionID string) bool {
			h.mu.Lock()
			defer h.mu.Unlock()
			return !h.sessionLocked(sessionID).running
		},
		CurrentRequestID: func(sessionID string) string {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.sessionLocked(sessionID).requestID
		},
		SubmitConversation: h.submit,
		RecordAttempt: func(sessionID string, attempt model.ContinuationAttempt) {
			h.mu.Lock()
			defer h.mu.Unlock()
			session := h.sessionLocked(sessionID)
			session.records = append(session.records, attempt)
		},
		SetPaused: func(sessionID, goalID string, paused bool, _ time.Time) {
			h.mu.Lock()
			defer h.mu.Unlock()
			session := h.sessionLocked(sessionID)
			session.paused = paused
			if paused {
				session.pausedGoalID = goalID // 层二绑定哪张 goal（I2）
				return
			}
			session.pausedGoalID = ""
		},
		// push 通道的唯一副作用点：立刻中断在飞子会话（等价于红色停止键）。
		InterruptInFlight: func(sessionID string) []string {
			h.mu.Lock()
			defer h.mu.Unlock()
			session := h.sessionLocked(sessionID)
			interrupted := make([]string, 0, len(session.children))
			for _, child := range session.children {
				if child.ctx.Err() != nil {
					continue
				}
				child.cancel()
				child.canceledAt = h.clock
				interrupted = append(interrupted, child.id)
			}
			return interrupted
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

func (h *channelHost) sessionLocked(sessionID string) *channelSession {
	session := h.sessions[sessionID]
	if session == nil {
		session = &channelSession{goalStatus: dto.GoalRunNone, turnStatus: dto.TurnInterrupted}
		h.sessions[sessionID] = session
	}
	return session
}

// factsFor 的派生口径与真实存档适配器一致（见 model.ContinuationAttempt 注释）。
func (h *channelHost) factsFor(sessionID string) ContinuationFacts {
	h.mu.Lock()
	defer h.mu.Unlock()
	session := h.sessionLocked(sessionID)
	facts := ContinuationFacts{
		GoalStatus:    session.goalStatus,
		GoalID:        session.goalID,
		GoalTitle:     "验收两条通道",
		TurnStatus:    session.turnStatus,
		RequestID:     session.requestID,
		Reason:        session.reason,
		StoppedByUser: session.stoppedByUser,
		Paused:        session.paused,
		PausedGoalID:  session.pausedGoalID,
		Continued:     map[string]bool{},
		Truncated:     map[string]bool{},
	}
	attempts := 0
	var lastAttempt time.Time
	for _, record := range session.records {
		if record.GoalID != session.goalID {
			continue
		}
		key := continuationKey(record.GoalID, record.RequestID)
		switch {
		case record.Truncated:
			facts.Truncated[key] = true
		case record.Manual && record.Attempt <= 0:
			attempts, lastAttempt = 0, time.Time{}
		case record.Attempt <= 0:
		default:
			facts.Continued[key] = true
			attempts++
			lastAttempt = record.At
		}
	}
	facts.Attempts = attempts
	facts.LastAttemptAt = lastAttempt
	return facts
}

func (h *channelHost) submit(_ context.Context, sessionID, prompt string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	session := h.sessionLocked(sessionID)
	session.submitted = append(session.submitted, prompt)
	session.requestSeq++
	session.requestID = fmt.Sprintf("req-auto-%d", session.requestSeq)
	session.turnStatus = dto.TurnProgressing
	session.running = true
	return nil
}

func (h *channelHost) setRunning(sessionID string, running bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	session := h.sessionLocked(sessionID)
	session.running = running
	if running {
		session.turnStatus = dto.TurnProgressing
	}
}

func (h *channelHost) finishRound(sessionID string, status dto.TurnStatus, requestID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	session := h.sessionLocked(sessionID)
	session.turnStatus = status
	session.requestID = requestID
	session.running = false
}

func (h *channelHost) interrupt(sessionID, requestID string) {
	h.finishRound(sessionID, dto.TurnInterrupted, requestID)
}

func (h *channelHost) setGoal(sessionID, goalID string, status dto.GoalRunStatus) {
	h.mu.Lock()
	defer h.mu.Unlock()
	session := h.sessionLocked(sessionID)
	session.goalID = goalID
	session.goalStatus = status
}

// startChild 造一个"在飞子会话"：它带自己的 ctx，只有被 cancel 才会结束。
func (h *channelHost) startChild(sessionID, childID string) *channelChild {
	h.mu.Lock()
	session := h.sessionLocked(sessionID)
	h.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	child := &channelChild{id: childID, ctx: ctx, cancel: cancel}
	h.mu.Lock()
	session.children = append(session.children, child)
	h.mu.Unlock()
	return child
}

func (h *channelHost) submitCount(sessionID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.sessionLocked(sessionID).submitted)
}

func (h *channelHost) roundRunning(sessionID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessionLocked(sessionID).running
}

func (h *channelHost) auditsOf(kind string) []ContinuationAudit {
	h.mu.Lock()
	defer h.mu.Unlock()
	matched := make([]ContinuationAudit, 0, len(h.audits))
	for _, entry := range h.audits {
		if entry.Kind == kind {
			matched = append(matched, entry)
		}
	}
	return matched
}

func (h *channelHost) lastAttempt(sessionID string) (model.ContinuationAttempt, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	records := h.sessionLocked(sessionID).records
	if len(records) == 0 {
		return model.ContinuationAttempt{}, false
	}
	return records[len(records)-1], true
}

// ① pull 通道：暂停期间本轮照常跑完（不中断），轮边界不再续；
// 恢复 continue 之后**下一次轮边界**才续。
func TestPullChannelPausedTurnRunsToCompletionThenNoNextRound(t *testing.T) {
	host := newChannelHost("s-pull")
	application := host.application()

	// 正在跑的这一轮：它有自己的 ctx，暂停没有权利取消它。
	round, cancelRound := context.WithCancel(context.Background())
	defer cancelRound()
	host.setRunning("s-pull", true)

	status := application.Pause("s-pull")
	if status.GoalStatus != dto.GoalRunDoing || status.Substate != dto.GoalSubstatePaused || !status.Paused {
		t.Fatalf("⏸ 之后两层状态 = %+v，want doing/paused（层一在跑、层二被按停）", status)
	}
	if round.Err() != nil {
		t.Fatal("pull 通道取消了本轮 ctx：暂停不得打断正在跑的这一轮")
	}

	// 第一半：本轮跑完（自己的活儿干完才结束）。
	host.finishRound("s-pull", dto.TurnCompleted, "req-1")
	if round.Err() != nil {
		t.Fatal("暂停在轮内生效了：它只在轮边界生效")
	}

	// 第二半：轮结束后被硬中断，暂停位在 → 轮边界一次都不发、也不挂欠账。
	host.interrupt("s-pull", "req-2")
	if pending := application.TriggerGoalContinuations("s-pull", map[string]struct{}{}); pending != 0 {
		t.Fatalf("暂停期间 pending = %d，want 0（持续状态不挂欠账）", pending)
	}
	if got := host.submitCount("s-pull"); got != 0 {
		t.Fatalf("暂停期间仍然自动续跑了 %d 次", got)
	}

	// 恢复 continue：**本身不发行**。
	if resumed := application.Continue("s-pull"); resumed.Substate != dto.GoalSubstateContinue || resumed.Paused {
		t.Fatalf("Continue 之后层二 = %+v，want continue", resumed)
	}
	if got := host.submitCount("s-pull"); got != 0 {
		t.Fatalf("恢复 continue 立刻发了 %d 次：它只清位，续跑发生在下一次轮边界", got)
	}

	// 下一次轮边界：这才续（pull 通道的生效点）。
	if pending := application.TriggerGoalContinuations("s-pull", map[string]struct{}{}); pending != 0 {
		t.Fatalf("恢复后续跑仍欠账 pending = %d", pending)
	}
	if got := host.submitCount("s-pull"); got != 1 {
		t.Fatalf("下一次轮边界的续跑数 = %d，want 1", got)
	}
	attempt, ok := host.lastAttempt("s-pull")
	if !ok || attempt.Manual || attempt.RequestID != "req-2" || attempt.Attempt != 1 {
		t.Fatalf("续跑落账 = %+v（want 一条非人工、绑定被中断的那一轮的记载）", attempt)
	}
	pauses := host.auditsOf("goal.pause")
	if len(pauses) != 1 || !strings.Contains(pauses[0].Detail, "pull") {
		t.Fatalf("goal.pause 打点 = %+v，want 一条写明 pull 通道的", pauses)
	}
}

// ② push 通道：对 teammate / subagent 发 paused 时在飞子会话被**立即**中断
// （不等轮边界），同时主会话 goal 状态切到 doing/paused。
func TestPushChannelInterruptsInFlightChildrenImmediately(t *testing.T) {
	host := newChannelHost("s-main")
	application := host.application()
	host.setRunning("s-main", true) // 主会话自己这一轮在跑
	reviewer := host.startChild("s-main", "teammate:reviewer")
	scout := host.startChild("s-main", "subagent:scout")

	status := application.PauseAndInterrupt("s-main")

	// 层二切到 paused，层一仍是 doing（主会话还在跑它自己那一轮）。
	if status.GoalStatus != dto.GoalRunDoing || status.Substate != dto.GoalSubstatePaused {
		t.Fatalf("push 之后两层状态 = %+v，want doing/paused", status)
	}
	// 在飞子会话**当场**被中断：不等轮边界、不等定时器（时钟一次都没动过）。
	if reviewer.ctx.Err() == nil || scout.ctx.Err() == nil {
		t.Fatalf("push 没有立刻中断在飞子会话：reviewer=%v scout=%v",
			reviewer.ctx.Err(), scout.ctx.Err())
	}
	if !reviewer.canceledAt.Equal(host.clock) || !scout.canceledAt.Equal(host.clock) {
		t.Fatal("子会话不是当场被中断的（时刻与调用时刻不一致）")
	}
	if got := strings.Join(status.Interrupted, ","); got != "teammate:reviewer,subagent:scout" {
		t.Fatalf("Interrupted = %q，want 两个子会话 ID", got)
	}
	// push 不停主会话自己这一轮（那是 CancelChat 的事）。
	if !host.roundRunning("s-main") {
		t.Fatal("push 把主会话自己这一轮也停了：push 只停子会话")
	}

	// 同一个状态源：随后的轮边界不再自动续（两条通道共享层二）。
	host.finishRound("s-main", dto.TurnInterrupted, "req-2")
	if pending := application.TriggerGoalContinuations("s-main", map[string]struct{}{}); pending != 0 {
		t.Fatalf("push 之后轮边界 pending = %d，want 0", pending)
	}
	if got := host.submitCount("s-main"); got != 0 {
		t.Fatalf("push 之后仍然自动续跑了 %d 次", got)
	}
	pauses := host.auditsOf("goal.pause")
	if len(pauses) != 1 || !strings.Contains(pauses[0].Detail, "push") ||
		!strings.Contains(pauses[0].Detail, "2") {
		t.Fatalf("goal.pause 打点 = %+v，want 一条写明 push 与停掉 2 个子会话的", pauses)
	}
}

// ③ 两层状态机 + 会话粒度：none/done 下层二无意义、换 goal 不残留、A/B 互不串扰。
func TestGoalRunStateIsSessionScopedAndPauseDoesNotResidue(t *testing.T) {
	host := newChannelHost("s-a", "s-b")
	application := host.application()
	// 两个会话都在各自的 goal 上、且都"上一轮被硬中断"（本来就都会自动续）。
	host.interrupt("s-b", "req-b-2")

	if status := application.Pause("s-a"); status.Substate != dto.GoalSubstatePaused {
		t.Fatalf("A 暂停没生效：%+v", status)
	}
	if status := application.Status("s-b"); status.Paused || status.Substate != dto.GoalSubstateContinue {
		t.Fatalf("A 的暂停串到了 B：%+v", status)
	}

	// 并发：A 反复按停、B 反复读状态，互不影响（I3；host 自带互斥，跑 -race 也一样）。
	var wait sync.WaitGroup
	for index := 0; index < 8; index++ {
		wait.Add(2)
		go func() {
			defer wait.Done()
			application.Pause("s-a")
		}()
		go func() {
			defer wait.Done()
			if status := application.Status("s-b"); status.Substate == dto.GoalSubstatePaused {
				t.Error("并发期 B 读到了 paused：会话粒度被串了")
			}
		}()
	}
	wait.Wait()

	// 不残留（I2）：A 换了一张 goal，读回必须是 continue——即便存档里的 paused 位还在。
	host.setGoal("s-a", "goal-2", dto.GoalRunDoing)
	state := host.factsFor("s-a").State()
	if state.GoalID != "goal-2" || state.Status != dto.GoalRunDoing ||
		state.Substate != dto.GoalSubstateContinue || state.AutoContinue() == false {
		t.Fatalf("换 goal 之后两层状态 = %+v，want doing/continue（paused 不得残留）", state)
	}
	// 回到那张 goal 上，暂停仍在（绑定的是 goal，不是"最近一次动作"）。
	host.setGoal("s-a", "goal-1", dto.GoalRunDoing)
	if state := host.factsFor("s-a").State(); state.Substate != dto.GoalSubstatePaused {
		t.Fatalf("回到原 goal 后层二 = %+v，want paused（绑定未被别的 goal 抹掉）", state)
	}

	// 层一 none / done 下层二无意义（I1）：paused 位不许拦住"没有目标"的会话。
	closed := ContinuationFacts{GoalStatus: dto.GoalRunDone, GoalID: "", Paused: true, PausedGoalID: "goal-1"}
	if state := closed.State(); state.AutoContinue() || state.Substate != dto.GoalSubstateContinue {
		t.Fatalf("done 会话的两层状态 = %+v，want done/continue", state)
	}
	none := ContinuationFacts{GoalStatus: dto.GoalRunNone, Paused: true, PausedGoalID: "goal-1"}
	if state := none.State(); state.AutoContinue() || state.Paused() {
		t.Fatalf("none 会话的两层状态 = %+v，want none/continue", state)
	}
}
