package execution

// continuation.go — goal 未收口 + 被硬中断时的**自动续跑**，以及它的两个人工
// 控制位：⏸ 暂停 / ⟳ 继续。
//
// 设计：docs/arch/goal-auto-continue.md（§2 触发/输入/防呆/可观测，§2.8 暂停与继续）。
//
// 本文件只做**判据与决策**，不做三件事（都留给装配方）：
//   - 不读会话存档（现场由 ContinuationDeps.Facts 一次读面给全）；
//   - 不起回合（唯一副作用是 ContinuationDeps.SubmitConversation，与
//     CompletionApplication 同一条通道）；
//   - 不自己造 idle 判据（SessionIdle 由装配方注入 sessionIdleForAsyncTrigger）。
//
// 一条写死的语义边界（暂停 ≠ 中断）：Pause 只落"别再自动续"这一位，本应用
// 里**没有任何取消句柄**，所以它不可能打断正在跑的那一轮；当前这一轮照常
// 跑完，只是轮结束后的下一次扫描不再发"继续"。要打断当前轮的是用户停止
// （CancelChat，写 StoppedByUser），那是另一条判据（C3）。

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
	GoalID    string // 空 = goal 已收口（C1 不成立）
	GoalTitle string
	// TurnStatus / RequestID 是**上一回合**的终态与请求号。
	TurnStatus dto.TurnStatus
	RequestID  string
	// Reason 是中断来源（interrupted / provider / restarted），空则按 interrupted。
	Reason string
	// StoppedByUser 是"用户这次真的按过停止"（C3，跨重启有效）。
	StoppedByUser bool
	// Paused 是"暂停自动续跑"位（⏸）。
	Paused bool
	// Attempts 是同一 goal 的连续续跑次数（人工恢复后归零）。
	Attempts int
	// LastAttemptAt 是最近一次续跑的发起时刻（退避判据）。
	LastAttemptAt time.Time
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
	GoalID        string
	GoalTitle     string
	RequestID     string
	Stalled       bool // 未收口 + 被硬中断 + 没停/没暂停（横条该出现）
	Paused        bool // ⏸ 已按下（横条显示"已暂停"）
	Truncated     bool // 已达上限，自动续跑停了（等人工 ⟳）
	Attempts      int
	MaxAttempts   int
	NextAttemptAt time.Time // 退避期的下次尝试时刻（零值 = 无退避在等）
}

// ContinuationDeps 是续跑的窄端口集合。全部函数式，装配方可整体缺省——
// 缺省 = 该判据不成立（不是"默认放行"）。
type ContinuationDeps struct {
	Facts              func(sessionID string) ContinuationFacts
	SessionIdle        func(sessionID string) bool
	CurrentRequestID   func(sessionID string) string // 可选：证明"真的起了一个回合"
	SubmitConversation func(ctx context.Context, sessionID, prompt string) error
	RecordAttempt      func(sessionID string, attempt model.ContinuationAttempt)
	SetPaused          func(sessionID string, paused bool, at time.Time)
	Audit              func(sessionID string, entry ContinuationAudit)
	Limits             func() limit.ContinuationLimits
	Now                func() time.Time
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

// Pause 是 goal 横条右侧的 ⏸：置"暂停自动续跑"位 + 记一条 goal.pause 打点。
//
// 它**不**打断当前这一轮：本应用没有任何取消句柄，暂停只影响"轮结束之后"。
// 它也不写 StoppedByUser —— 暂停是"别替我继续"，停止是"这一轮别跑了"，两件事。
func (application *ContinuationApplication) Pause(sessionID string) ContinuationStatus {
	sessionID = strings.TrimSpace(sessionID)
	limits := application.continuationLimits()
	if sessionID == "" || !limits.On() || application.deps.SetPaused == nil {
		return application.Status(sessionID)
	}
	now := application.now()
	application.deps.SetPaused(sessionID, true, now)
	facts := application.facts(sessionID)
	application.audit(sessionID, ContinuationAudit{
		Kind:      string(goal.EventPause),
		GoalID:    facts.GoalID,
		RequestID: facts.RequestID,
		Reason:    facts.Reason,
		Attempt:   facts.Attempts,
		Paused:    true,
		Detail:    "用户暂停自动续跑（当前这一轮不被打断，轮结束后不再自动发继续）",
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
	if application.deps.SetPaused != nil {
		application.deps.SetPaused(sessionID, false, now)
	}
	facts := application.facts(sessionID)
	// 人工恢复打点 + 计数清零标记：一条 append-only 记录记两件事
	// （Attempt=0 & Manual=true ⇒ 读面从这里重新数次数）。
	application.recordAttempt(sessionID, model.ContinuationAttempt{
		At:        now,
		GoalID:    facts.GoalID,
		RequestID: facts.RequestID,
		Reason:    facts.Reason,
		Attempt:   0,
		Manual:    true,
	})
	application.audit(sessionID, ContinuationAudit{
		Kind:      string(goal.EventResume),
		GoalID:    facts.GoalID,
		RequestID: facts.RequestID,
		Reason:    facts.Reason,
		Manual:    true,
		Detail:    "人工恢复：清暂停位、续跑计数清零，并立即续一次",
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
	status.GoalID = facts.GoalID
	status.GoalTitle = facts.GoalTitle
	status.RequestID = facts.RequestID
	status.Paused = facts.Paused
	status.Attempts = facts.Attempts
	status.Truncated = facts.Attempts >= limits.Attempts() ||
		facts.Truncated[continuationKey(facts.GoalID, facts.RequestID)]
	status.Stalled = continuable(facts) && !facts.Paused
	if delay := limits.Backoff(facts.Attempts); delay > 0 {
		status.NextAttemptAt = facts.LastAttemptAt.Add(delay)
	}
	return status
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
//	C1 goal 未收口      GoalID != ""（Active != nil 且非终态）
//	C2 真的被硬中断    上一回合终态 == TurnInterrupted 且有 requestID
//	C3 人工意图优先    没被用户显式停止、没被暂停
//
// TurnBlocked / TurnNeedsUserDecision 一律不在 C2 里——那两种是"等人"，
// 自动续跑会变成自问自答（设计 §2.1）。
func continuable(facts ContinuationFacts) bool {
	if strings.TrimSpace(facts.GoalID) == "" {
		return false
	}
	if facts.TurnStatus != dto.TurnInterrupted || strings.TrimSpace(facts.RequestID) == "" {
		return false
	}
	if facts.StoppedByUser || facts.Paused {
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
