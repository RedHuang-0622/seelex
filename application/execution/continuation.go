package execution

// continuation.go — goal 未收口 + 被硬中断时的**自动续跑**，以及它的两个人工
// 控制位：⏸ 暂停 / ⟳ 继续。
//
// 设计：docs/arch/goal-auto-continue.md（§2 触发/输入/防呆/可观测，§2.8 两层状态机
// 与两条通道）。
//
// **两条通道，不是一个 bool**（§2.8.0 / §2.8.2 / §2.8.3）：
//
//   - pull（`TriggerGoalContinuations`，主路径）：`paused` 的身份是责任链上的一问，
//     **每轮轮询现读**两层状态（`dto.DeriveGoalRunState`），**只在轮边界生效**。
//     它不能也不应该打断正在跑的这一轮：本文件没有任何取消句柄，
//     `Pause` 的副作用只有"写位 + 打点"。
//   - push（`PauseAndInterrupt`，即时中断）：给附属 teammate / subagent 发 paused 是
//     **瞬时中断**，语义等同输入框里那个红色停止键——立刻停掉在飞的子会话，不走轮询。
//     它唯一的副作用点是 `ContinuationDeps.InterruptInFlight`（装配方接到真实取消通道）。
//
// 两条通道**共享同一个状态源**（层二子状态，按 sessionID 维护），生效方式不同：
// pull = 下一轮不再自动继续；push = 立刻停掉在跑的子会话。
//
// 本文件只做**判据与决策**，不做三件事（都留给装配方）：
//   - 不读会话存档（现场由 ContinuationDeps.Facts 一次读面给全）；
//   - 不起回合（唯一副作用是 ContinuationDeps.SubmitConversation，与
//     CompletionApplication 同一条通道）；
//   - 不自己造 idle 判据（SessionIdle 由装配方注入 sessionIdleForAsyncTrigger）。
//
// 另一条写死的语义边界（暂停 ≠ 中断）：`Pause` 只落"别再自动续"这一位，当前这一轮
// 照常跑完。要打断**主会话这一轮**的是用户停止（CancelChat，写 StoppedByUser），
// 那是另一条判据（C3）。

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
	model "github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/goal"
	"github.com/RedHuang-0622/seelex/kernel/limit"
)

// 中断来源（只作打点文案，不参与判据）。
const (
	ContinuationReasonInterrupted = "interrupted"
	ContinuationReasonProvider    = "provider"
	ContinuationReasonRestarted   = "restarted"
)

// continuationTriggeredPrefix 给进程内幂等账加命名空间，避免与
// CompletionApplication 的作业句柄键撞车（同一个 map 由 AsyncConsumer 持有）。
const continuationTriggeredPrefix = "goal-continue|"

// ContinuationFacts 是续跑 gate 的**唯一读面**：一次读全，判据不自己攒。
//
// 现场来自会话存档（SessionRecord.Execution + goal 栈），由装配方提供；
// Attempts / Continued / Truncated 都由存档里的 Continuations 派生
// （派生口径写在 model.ContinuationAttempt 上，只有一处）：
//   - Continued[key]  ← 存在一条**没被截断**的同键记录（真发过续跑）；
//   - Truncated[key]  ← 存在一条同键的截断记录（只记一次止步痕）；
//   - Attempts        ← 最后一条 Manual 记录之后的非 Truncated 记录条数。
type ContinuationFacts struct {
	// GoalStatus 是层一（none / doing / done）。空串按 GoalID 推断（非空 = doing），
	// 只给"手上只有 goal id"的旧调用点用；装配侧应当给真值（goal.StatusFor）。
	GoalStatus dto.GoalRunStatus
	GoalID     string // 空 = goal 已收口（C1 不成立）
	GoalTitle  string
	// TurnStatus / RequestID 是**上一回合**的终态与请求号。
	TurnStatus dto.TurnStatus
	RequestID  string
	// Reason 是中断来源（interrupted / provider / restarted），空则按 interrupted。
	Reason string
	// StoppedByUser 是"用户这次真的按过停止"（C3，跨重启有效）。
	StoppedByUser bool
	// Paused 是层二子状态在存档里的低半位（⏸）。
	Paused bool
	// PausedGoalID 是暂停**绑定在哪张 goal 上**：只有它等于 GoalID 时层二才算 paused，
	// 否则读成 continue（换 goal 不残留，旧档没绑定同样不继承）。见 dto.DeriveGoalRunState。
	PausedGoalID string
	// Attempts 是同一 goal 的连续续跑次数（人工恢复后归零）。
	Attempts int
	// LastAttemptAt 是最近一次续跑的发起时刻（退避判据）。
	LastAttemptAt time.Time
	// InterruptedAt 是上一回合被硬中断的时刻（存档里那个回合终态的写入时间）。
	// 它只喂横条的"stalled 多久"（ContinuationStatus.StalledFor），不参与放行判据：
	// 判据只看 TurnStatus，不看时刻。
	InterruptedAt time.Time
	// Continued 是"真的发过一次续跑"的 (GoalID, RequestID) 集合（⑤ 幂等权威判据）。
	// **截断记录不在这个集合里**：截断没发出任何续跑，所以人工恢复（⟳）之后
	// 同一中断点还能再试一次——这是"继续"按钮在触顶之后仍然有用的原因。
	Continued map[string]bool
	// Truncated 是"已经记过截断痕"的 (GoalID, RequestID) 集合：只用于避免同一
	// 次中断被反复记 truncated 打点（止步痕记一次就够，不刷屏）。
	Truncated map[string]bool
	// Checkpoint 是可选的现场摘要（TaskCheckpoint.CompletedWork/PendingWork 的一行
	// 投影）。空则续跑输入退回"读回现场再继续"的通用措辞。
	Checkpoint string
}

// State 把这一份现场合成为**两层状态**（唯一判定点是 dto.DeriveGoalRunState）。
//
// 这是 pull 通道读的全部：`AutoContinue()` 为假 ⇒ 轮边界不自动续。push 通道不看它
// （push 停的是在跑的子会话，不回答"下一轮要不要自动发"）。
func (facts ContinuationFacts) State() dto.GoalRunState {
	return dto.DeriveGoalRunState(facts.GoalID, facts.GoalStatus, facts.Paused, facts.PausedGoalID)
}

// ContinuationAudit 是一条续跑面的打点（结构化：截断/人工恢复都靠它区分）。
type ContinuationAudit struct {
	Kind      string // goal.continue | goal.pause | goal.resume
	GoalID    string
	RequestID string
	Reason    string
	Attempt   int
	Truncated bool
	Paused    bool
	Manual    bool
	Detail    string
}

// ContinuationStatus 是 goal 顶部那条 stalled 横条的读面（GUI 只读这一份）。
type ContinuationStatus struct {
	GoalID    string
	GoalTitle string
	// GoalStatus / Substate 是两层状态（§2.8.0）：横条据此区分"没目标 / 在跑但被暂停 /
	// 在跑且会自动续"，而不是靠 Paused 一个 bool 猜。
	GoalStatus    dto.GoalRunStatus
	Substate      dto.GoalRunSubstate
	RequestID     string
	Stalled       bool // 未收口 + 被硬中断 + 没停 + AutoContinue()（横条该出现）
	Paused        bool // ⏸ 已按下（层二 = paused；横条显示"已暂停"）
	Truncated     bool // 已达上限，自动续跑停了（等人工 ⟳）
	Attempts      int
	MaxAttempts   int
	NextAttemptAt time.Time // 退避期的下次尝试时刻（零值 = 无退避在等）
	// StalledSince / StalledFor 是"这条横条已经挂了多久"（只读展示）：起点是
	// 中断时刻与最近一次续跑时刻里**较晚**的那个，终点由 Now 给。Stalled=false
	// 时两者为零值（没在等，就没有时长可言）。
	StalledSince time.Time
	StalledFor   time.Duration
	// Interrupted 是最近一次 push（`PauseAndInterrupt`）停掉的子会话 ID：横条据此显示
	// "已停掉 N 个子会话"。它是一次动作的回执，不落盘、不参与任何判据。
	Interrupted []string
}

// ContinuationDeps 是续跑的窄端口集合。全部函数式，装配方可整体缺省——
// 缺省 = 该判据不成立（不是"默认放行"）。
type ContinuationDeps struct {
	Facts              func(sessionID string) ContinuationFacts
	SessionIdle        func(sessionID string) bool
	CurrentRequestID   func(sessionID string) string // 可选：证明"真的起了一个回合"
	SubmitConversation func(ctx context.Context, sessionID, prompt string) error
	RecordAttempt      func(sessionID string, attempt model.ContinuationAttempt)
	// SetPaused 是层二的**唯一写点**（会话粒度）：goalID 是这次暂停/恢复绑定到哪张
	// goal 上（清位时同样带上，便于把存档里的过期绑定一起清掉）。
	SetPaused func(sessionID, goalID string, paused bool, at time.Time)
	// InterruptInFlight 是 **push 通道的唯一副作用点**：立刻中断该会话名下在飞的
	// 子会话（teammate 角色回合 / subagent），语义等同红色停止键——不等轮边界。
	// 返回被打断的子会话 ID（进 ContinuationStatus.Interrupted）。
	//
	// 缺省 = 没有可用的中断通道：push 仍会写位（"下一轮不再自动续"），但不会声称
	// 停掉了任何子会话（返回空切片），不静默假装成功。
	InterruptInFlight func(sessionID string) []string
	Audit             func(sessionID string, entry ContinuationAudit)
	Limits            func() limit.ContinuationLimits
	Now               func() time.Time
}

// ContinuationApplication owns the auto-continuation gate and its pause/resume
// control bits.
type ContinuationApplication struct {
	deps ContinuationDeps
}

// NewContinuation constructs the auto-continuation application.
func NewContinuation(deps ContinuationDeps) *ContinuationApplication {
	return &ContinuationApplication{deps: deps}
}

// TriggerGoalContinuations 扫一次某会话的续跑门。
//
// 返回值语义与 CompletionApplication 一致：>0 = 条件仍成立但这一次没发
// （会话忙 / 退避期未到），定时器该再来一次；0 = 没欠账。
// triggered 是进程内幂等账，由 AsyncConsumer 持有；跨重启的唯一权威判据是
// Facts.Continued（落盘的 ContinuationAttempt 记录）。
func (application *ContinuationApplication) TriggerGoalContinuations(sessionID string, triggered map[string]struct{}) int {
	return application.evaluate(sessionID, triggered, false)
}

// Pause 是 **pull 通道**的 ⏸：置层二子状态 paused（绑定 facts.State().GoalID）+ 记一条
// goal.pause 打点。**每轮轮询读它，只在轮边界生效。**
//
// 它**不**打断当前这一轮：本应用没有任何取消句柄，暂停只影响"轮结束之后"。
// 要立刻停掉在飞的子会话请用 PauseAndInterrupt（push 通道）；要停主会话这一轮是用户
// 停止（CancelChat）。暂停也不写 StoppedByUser —— "别替我继续"与"这一轮别跑了"是两件事。
func (application *ContinuationApplication) Pause(sessionID string) ContinuationStatus {
	sessionID = strings.TrimSpace(sessionID)
	limits := application.continuationLimits()
	if sessionID == "" || !limits.On() || application.deps.SetPaused == nil {
		return application.Status(sessionID)
	}
	now := application.now()
	facts := application.facts(sessionID)
	state := facts.State()
	// 绑定当前 goal（空 = 无目标：位照写，但读侧 I1 会把它读成 continue，不会拦住下一张 goal）。
	application.deps.SetPaused(sessionID, state.GoalID, true, now)
	application.audit(sessionID, ContinuationAudit{
		Kind:      string(goal.EventPause),
		GoalID:    state.GoalID,
		RequestID: facts.RequestID,
		Reason:    facts.reason(),
		Attempt:   facts.Attempts,
		Paused:    true,
		Detail:    "pull 通道：只写暂停位（当前这一轮不被打断，轮结束后不再自动发继续）",
	})
	return application.Status(sessionID)
}

// PauseAndInterrupt 是 **push 通道**：写同一个状态源（层二 paused，绑定当前 goal），
// 然后**立刻**中断该会话名下在飞的子会话（teammate / subagent），不等轮边界、不走轮询。
//
// 顺序写死：先写位、后中断。中断会让那些子回合立刻到终态，而"这个终态要不要触发自动
// 续跑"那一问必须在位已置好之后才被问——反过来的话，中断与写位之间有一个窗口，窗口里
// 自动续跑会把用户刚按停的东西又发起来。
//
// 它不停**主会话自己**正在跑的这一轮（那是 CancelChat 的事）。
func (application *ContinuationApplication) PauseAndInterrupt(sessionID string) ContinuationStatus {
	sessionID = strings.TrimSpace(sessionID)
	limits := application.continuationLimits()
	if sessionID == "" || !limits.On() {
		return application.Status(sessionID)
	}
	now := application.now()
	facts := application.facts(sessionID)
	state := facts.State()
	if state.Doing() && application.deps.SetPaused != nil {
		application.deps.SetPaused(sessionID, state.GoalID, true, now)
	}
	interrupted := application.interruptInFlight(sessionID)
	application.audit(sessionID, ContinuationAudit{
		Kind:      string(goal.EventPause),
		GoalID:    state.GoalID,
		RequestID: facts.RequestID,
		Reason:    facts.reason(),
		Attempt:   facts.Attempts,
		Paused:    true,
		Detail: fmt.Sprintf(
			"push 通道：立刻中断在飞子会话 %d 个 [%s]，主会话 goal 切到 %s/%s",
			len(interrupted), strings.Join(interrupted, ","), state.Status, state.Substate,
		),
	})
	status := application.Status(sessionID)
	status.Interrupted = interrupted
	return status
}

// Continue 是 **pull 通道的清位**（§2.8.4）：只把层二清回 continue，**不发行**——
// 恢复 continue 之后要等**下一次轮边界**（下一次 TriggerGoalContinuations）才续。
//
// 与 Resume 的区别是刻意的：⟳ 是"现在就续一次"（人工意图），Continue 是"接着自动跑吧"。
func (application *ContinuationApplication) Continue(sessionID string) ContinuationStatus {
	sessionID = strings.TrimSpace(sessionID)
	limits := application.continuationLimits()
	if sessionID == "" || !limits.On() {
		return application.Status(sessionID)
	}
	facts := application.facts(sessionID)
	state := facts.State()
	application.clearPaused(sessionID, state.GoalID, application.now())
	application.audit(sessionID, ContinuationAudit{
		Kind:      string(goal.EventResume),
		GoalID:    state.GoalID,
		RequestID: facts.RequestID,
		Reason:    facts.reason(),
		Detail:    "恢复 continue（pull）：只清位，等下一次轮边界才续",
	})
	return application.Status(sessionID)
}

// Resume 是 goal 横条右侧的 ⟳（人工恢复）：清暂停位 → 记一条 goal.resume 打点
// （它同时是"续跑计数被清零"的唯一合法解释）→ **立刻**续一次，不等定时器。
//
// 只发一次：发出去就落一条 ContinuationAttempt，之后的定时扫描读到
// Facts.Continued 直接短路——所以"人工恢复"与"定时策略"不会叠加成双发。
func (application *ContinuationApplication) Resume(sessionID string) ContinuationStatus {
	sessionID = strings.TrimSpace(sessionID)
	limits := application.continuationLimits()
	if sessionID == "" || !limits.On() {
		return application.Status(sessionID)
	}
	now := application.now()
	facts := application.facts(sessionID)
	state := facts.State()
	application.clearPaused(sessionID, state.GoalID, now)
	// 人工恢复打点 + 计数清零标记：一条 append-only 记录记两件事
	// （Attempt=0 & Manual=true ⇒ 读面从这里重新数次数）。
	application.recordAttempt(sessionID, model.ContinuationAttempt{
		At:        now,
		GoalID:    state.GoalID,
		RequestID: facts.RequestID,
		Reason:    facts.reason(),
		Attempt:   0,
		Manual:    true,
	})
	application.audit(sessionID, ContinuationAudit{
		Kind:      string(goal.EventResume),
		GoalID:    state.GoalID,
		RequestID: facts.RequestID,
		Reason:    facts.reason(),
		Manual:    true,
		Detail:    "人工恢复（⟳）：清暂停位、续跑计数清零，并立即续一次",
	})
	application.evaluate(sessionID, map[string]struct{}{}, true)
	return application.Status(sessionID)
}

// Status 是 goal 顶部 stalled 横条的读面（只读，不写任何账）。
func (application *ContinuationApplication) Status(sessionID string) ContinuationStatus {
	limits := application.continuationLimits()
	status := ContinuationStatus{MaxAttempts: limits.Attempts()}
	if !limits.On() || strings.TrimSpace(sessionID) == "" {
		return status
	}
	facts := application.facts(sessionID)
	state := facts.State()
	status.GoalID = state.GoalID
	status.GoalTitle = facts.GoalTitle
	status.GoalStatus = state.Status
	status.Substate = state.Substate
	status.RequestID = facts.RequestID
	status.Paused = state.Paused()
	status.Attempts = facts.Attempts
	status.Truncated = facts.Attempts >= limits.Attempts() ||
		facts.Truncated[continuationKey(state.GoalID, facts.RequestID)]
	status.Stalled = continuable(facts)
	if status.Stalled {
		// stalled 起点 = 中断时刻与最近一次续跑里较晚的那个（续过就从续的那一刻重算：
		// 横条说的是"这一轮等待已经多久了"，不是"第一次中断到现在多久"）。
		since := facts.InterruptedAt
		if facts.LastAttemptAt.After(since) {
			since = facts.LastAttemptAt
		}
		if !since.IsZero() {
			now := application.now()
			status.StalledSince = since
			if now.After(since) {
				status.StalledFor = now.Sub(since)
			}
		}
	}
	if delay := limits.Backoff(facts.Attempts); delay > 0 {
		status.NextAttemptAt = facts.LastAttemptAt.Add(delay)
	}
	return status
}

// View 把横条读面投影成快照里的两层状态视图（唯一转换点：GUI 与工具读的是快照里
// 那一份，不各自再拼一遍 §2.8.0 的两层词表）。
func (status ContinuationStatus) View() *dto.GoalContinuationView {
	view := &dto.GoalContinuationView{
		GoalID:        status.GoalID,
		Status:        status.GoalStatus,
		Substate:      status.Substate,
		Stalled:       status.Stalled,
		Paused:        status.Paused,
		Truncated:     status.Truncated,
		Attempts:      status.Attempts,
		MaxAttempts:   status.MaxAttempts,
		StalledForSec: int64(status.StalledFor / time.Second),
	}
	if !status.NextAttemptAt.IsZero() {
		view.NextAttemptAt = status.NextAttemptAt.Unix()
	}
	return view
}

// evaluate 是唯一的判据实现：定时/信号驱动的扫描与人工恢复共用它，
// 两条路径不可能漂成两套语义。manual=true 只跳过"上限 + 退避"两问
// （人工刚把计数清零，且是"现在就续"的明确意思），**不跳过任何安全门**
// （未收口 / 真被中断 / 没被停止 / 没被暂停 / 空闲 / 幂等）。
func (application *ContinuationApplication) evaluate(sessionID string, triggered map[string]struct{}, manual bool) int {
	limits := application.continuationLimits()
	if !limits.On() { // 开关关 = 一次都不扫，也不写任何账
		return 0
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || application.deps.Facts == nil {
		return 0
	}
	if triggered == nil {
		triggered = make(map[string]struct{})
	}
	facts := application.facts(sessionID)
	if !continuable(facts) {
		return 0
	}
	key := continuationKey(facts.GoalID, facts.RequestID)
	// ⑤ 幂等：同一中断点只续一次（跨重启的唯一权威判据 = 落盘记录）。
	if facts.Continued[key] {
		return 0
	}
	if _, done := triggered[continuationTriggeredPrefix+key]; done {
		return 0
	}
	now := application.now()
	if !manual {
		// ④ 上限：触顶就停（记一条 truncated 打点，不静默重试）。
		// 同一次中断的止步痕只记一次：Truncated[key] 已存在就直接短路。
		if facts.Attempts >= limits.Attempts() {
			if !facts.Truncated[key] {
				application.recordTruncation(sessionID, facts, triggered, now, limits)
			}
			return 0
		}
		// ④ 退避：第 2 次起按 BackoffSec × 2^(n-2) 拉开间隔，封顶。
		if delay := limits.Backoff(facts.Attempts); delay > 0 && now.Before(facts.LastAttemptAt.Add(delay)) {
			return 1
		}
	}
	// C4 空闲门：忙会话不打断、也不往它的队列里塞东西。
	if !application.sessionIdle(sessionID) {
		return 1
	}
	return application.fire(sessionID, facts, triggered, now, manual)
}

// fire 是唯一副作用点：发一条合成"继续"输入，成功才落账。
func (application *ContinuationApplication) fire(
	sessionID string,
	facts ContinuationFacts,
	triggered map[string]struct{},
	now time.Time,
	manual bool,
) int {
	if application.deps.SubmitConversation == nil {
		return 0
	}
	key := continuationKey(facts.GoalID, facts.RequestID)
	before := application.currentRequestID(sessionID)
	triggered[continuationTriggeredPrefix+key] = struct{}{}
	if err := application.deps.SubmitConversation(
		context.Background(),
		sessionID,
		continuationPrompt(facts, manual),
	); err != nil {
		// 没发出去就不算续过：撤掉本次的进程内记账，留给下一次信号。
		delete(triggered, continuationTriggeredPrefix+key)
		log.Printf("[continuation] goal %s 续跑起回合失败（session=%s, request=%s）：%v",
			facts.GoalID, sessionID, facts.RequestID, err)
		return 0
	}
	if application.deps.CurrentRequestID != nil {
		if after := application.currentRequestID(sessionID); after == before {
			// "提交成功"不等于"真的起了一个回合"（可能被 stale 判定丢掉）。
			// 这种情况不落账、不计数，交给定时器下一次再看。
			delete(triggered, continuationTriggeredPrefix+key)
			log.Printf("[continuation] goal %s 续跑提交后回合未启动（session=%s, request=%s）",
				facts.GoalID, sessionID, facts.RequestID)
			return 1
		}
	}
	attempt := model.ContinuationAttempt{
		At:        now,
		GoalID:    facts.GoalID,
		RequestID: facts.RequestID,
		Reason:    facts.reason(),
		Attempt:   facts.Attempts + 1,
		Manual:    manual,
	}
	application.recordAttempt(sessionID, attempt)
	application.audit(sessionID, ContinuationAudit{
		Kind:      string(goal.EventContinue),
		GoalID:    facts.GoalID,
		RequestID: facts.RequestID,
		Reason:    attempt.Reason,
		Attempt:   attempt.Attempt,
		Manual:    manual,
		Detail:    continuationPrompt(facts, manual),
	})
	return 0
}

// recordTruncation 记"被上限截断"：截断痕是一次性的（落盘记录带
// Truncated=true，且同一 (GoalID, RequestID) 之后一律短路）。
func (application *ContinuationApplication) recordTruncation(
	sessionID string,
	facts ContinuationFacts,
	triggered map[string]struct{},
	now time.Time,
	limits limit.ContinuationLimits,
) {
	key := continuationKey(facts.GoalID, facts.RequestID)
	triggered[continuationTriggeredPrefix+key] = struct{}{}
	detail := fmt.Sprintf(
		"截断：同一 goal 已连续自动续跑 %d 次（上限 %d），停止自动续跑；人工恢复（⟳）可清零重来",
		facts.Attempts, limits.Attempts(),
	)
	application.recordAttempt(sessionID, model.ContinuationAttempt{
		At:        now,
		GoalID:    facts.GoalID,
		RequestID: facts.RequestID,
		Reason:    facts.reason(),
		Attempt:   facts.Attempts + 1,
		Truncated: true,
	})
	application.audit(sessionID, ContinuationAudit{
		Kind:      string(goal.EventContinue),
		GoalID:    facts.GoalID,
		RequestID: facts.RequestID,
		Reason:    facts.reason(),
		Attempt:   facts.Attempts + 1,
		Truncated: true,
		Detail:    detail,
	})
	log.Printf("[continuation] goal %s %s（session=%s, request=%s）",
		facts.GoalID, detail, sessionID, facts.RequestID)
}

// continuable 是 C1/C2/C3 的合判：
//
//	C1 goal 未收口     层一 = doing（GoalID 非空且未收口）
//	C2 真的被硬中断    上一回合终态 == TurnInterrupted 且有 requestID
//	C3 人工意图优先    没被用户显式停止；层二 = continue（没被暂停，且暂停不是别的 goal 的）
//
// 前两条的层一/层二部分一次交给 `dto.DeriveGoalRunState`（唯一判定点，含 I1/I2），
// 本函数只补 C2 与"C3 的停止那一半"。
//
// TurnBlocked / TurnNeedsUserDecision 一律不在 C2 里——那两种是"等人"，
// 自动续跑会变成自问自答（设计 §2.1）。
func continuable(facts ContinuationFacts) bool {
	if !facts.State().AutoContinue() {
		return false
	}
	if facts.TurnStatus != dto.TurnInterrupted || strings.TrimSpace(facts.RequestID) == "" {
		return false
	}
	if facts.StoppedByUser {
		return false
	}
	return true
}

func continuationKey(goalID, requestID string) string {
	return strings.TrimSpace(goalID) + "|" + strings.TrimSpace(requestID)
}

func (facts ContinuationFacts) reason() string {
	if reason := strings.TrimSpace(facts.Reason); reason != "" {
		return reason
	}
	return ContinuationReasonInterrupted
}

// continuationPrompt 是合成"继续"输入的正文（模板只此一处）。
func continuationPrompt(facts ContinuationFacts, manual bool) string {
	var body strings.Builder
	if manual {
		body.WriteString("用户按了继续（人工恢复）。")
	}
	fmt.Fprintf(&body, "goal 还没有收口，上一轮被中断（request=%s，原因=%s）。\n",
		facts.RequestID, facts.reason())
	title := strings.TrimSpace(facts.GoalTitle)
	if title == "" {
		title = "（未命名）"
	}
	fmt.Fprintf(&body, "- goal: %s（id=%s）\n", title, facts.GoalID)
	if checkpoint := strings.TrimSpace(facts.Checkpoint); checkpoint != "" {
		fmt.Fprintf(&body, "- 上次进度：%s\n", checkpoint)
	}
	body.WriteString("请接着推进；先把已完成部分复核一遍再动手。")
	body.WriteString("若目标其实已经达成，用 goal_done 收口并只报告结论。")
	return body.String()
}

func (application *ContinuationApplication) facts(sessionID string) ContinuationFacts {
	if application == nil || application.deps.Facts == nil {
		return ContinuationFacts{}
	}
	return application.deps.Facts(sessionID)
}

func (application *ContinuationApplication) continuationLimits() limit.ContinuationLimits {
	if application == nil || application.deps.Limits == nil {
		return limit.ContinuationLimits{}
	}
	return application.deps.Limits()
}

func (application *ContinuationApplication) sessionIdle(sessionID string) bool {
	if application == nil || application.deps.SessionIdle == nil {
		return false // 无读面 = 无从判定空闲：不放行
	}
	return application.deps.SessionIdle(sessionID)
}

// clearPaused 是层二的唯一清位实现（Continue / Resume 共用）。
// 即使 goalID 为空也照写一次：目的是把存档里可能残留的过期绑定一起清掉。
func (application *ContinuationApplication) clearPaused(sessionID, goalID string, at time.Time) {
	if application == nil || application.deps.SetPaused == nil {
		return
	}
	application.deps.SetPaused(sessionID, goalID, false, at)
}

// interruptInFlight 是 push 通道的唯一副作用点（装配方缺省 = 没有可用中断通道）。
func (application *ContinuationApplication) interruptInFlight(sessionID string) []string {
	if application == nil || application.deps.InterruptInFlight == nil {
		return nil
	}
	return application.deps.InterruptInFlight(sessionID)
}

func (application *ContinuationApplication) currentRequestID(sessionID string) string {
	if application == nil || application.deps.CurrentRequestID == nil {
		return ""
	}
	return application.deps.CurrentRequestID(sessionID)
}

func (application *ContinuationApplication) recordAttempt(sessionID string, attempt model.ContinuationAttempt) {
	if application == nil || application.deps.RecordAttempt == nil {
		return
	}
	application.deps.RecordAttempt(sessionID, attempt)
}

func (application *ContinuationApplication) audit(sessionID string, entry ContinuationAudit) {
	if application == nil || application.deps.Audit == nil {
		return
	}
	application.deps.Audit(sessionID, entry)
}

func (application *ContinuationApplication) now() time.Time {
	if application == nil || application.deps.Now == nil {
		return time.Now()
	}
	return application.deps.Now()
}
