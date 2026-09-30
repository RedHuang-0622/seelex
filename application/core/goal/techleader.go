package goal

// techleader.go — DS-A2A 双会话治理的 goal 域编排（协议 v0.1 + 详设 §4 落地）。
//
// 结构（替换旧"同会话双角色共享上下文"实现：旧的 Supervisor 每次回合从 Controller 实时
// 重建 goal 帧 + SetSessionTail 喂 a 尾窗 + AppendDirective 把 TL 摘要写回 goal 共享指令环）：
//
//	Supervisor = goal 域内的 PeerSessionManager（详设 §4）：EXEC(a) 侧唯一编排者
//	  - execSeq    : a 事件账本水位（turn/checkpoint/goal_update 均登记；turn 跳帧不帧化，
//	                 但 turn_completed 携带 Detail 时该摘要进待抽帧缓冲，回合前抽成
//	                 work.progress 帧下发，b 因此看得到 EXEC 干的活）
//	  - AdvisorSession(b)：独立上下文（锚点 + 帧账本 + 自身回合段），只尾部追加
//	  - Mirror on_eval：b 回合前把区间内抽帧集一次性 append（协议 C3/C5）
//	  - DirectiveBus = TechLeaderMailbox：b→a corr 信封队列（cap MaxDirectiveQueue，幂等 drain）
//	  - B4 缺席：回合失败（429/超时）→ gate 按缺席矩阵，a 永不等待 b
//	  - 生命周期：首次回合惰性 bind（goal.start 锚点）；goal 收口/失败 → unbind(reason)+reap
//
// 锁序：Supervisor.mu → Controller.mu（回合内先记 b 上下文再读 controller active goal）；
// Controller 永不回调 Supervisor，无反向死锁。评估器调用是唯一阻塞点（装配层负责超时护栏）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// ---- DirectiveBus（b→a corr 信封队列，协议 §5） ----

// TechLeaderMailbox 是 ADVISOR(b) → EXEC(a) 的有界指令队列：
//   - PublishDirective 发布带 corr 信封的结构化指令（满丢最旧 + 计数）；
//   - DrainDirectives 一次性排空（corr 幂等：排空即消费，重发由 corr 审计去重）。
//
// 不再承载"执行信号入队"（旧同会话信号队列移除——a 事件经 execSeq 账本 + 帧化，见 Notify）。
type TechLeaderMailbox struct {
	mu sync.Mutex

	directives    []TLDirective
	maxDirectives int

	overflowDirectives int64
}

// NewTechLeaderMailbox 构造有界指令队列（cap ≤0 用 MaxDirectiveQueue）。
func NewTechLeaderMailbox(maxDirectives int) *TechLeaderMailbox {
	if maxDirectives <= 0 {
		maxDirectives = MaxDirectiveQueue
	}
	return &TechLeaderMailbox{
		directives:    make([]TLDirective, 0, maxDirectives),
		maxDirectives: maxDirectives,
	}
}

// PublishDirective 发布一条 b→a 指令（corr 信封；满丢最旧并计数，不阻塞）。
func (m *TechLeaderMailbox) PublishDirective(directive TLDirective) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.directives) >= m.maxDirectives {
		m.directives = m.directives[1:]
		m.overflowDirectives++
	}
	m.directives = append(m.directives, directive)
}

// DrainDirectives 一次性排空全部待领取指令（corr 幂等消费）。
func (m *TechLeaderMailbox) DrainDirectives() []TLDirective {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.directives) == 0 {
		return nil
	}
	out := append([]TLDirective(nil), m.directives...)
	m.directives = m.directives[:0]
	return out
}

// PeekDirectives 读取待领取指令的副本（**不消费**）：供"指令产出后在同一个回合
// 里先回放进可见会话"使用——受信注入（下一次 ChatStream 前）仍由 DrainDirectives
// 唯一消费，两者互不影响。
func (m *TechLeaderMailbox) PeekDirectives() []TLDirective {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.directives) == 0 {
		return nil
	}
	return append([]TLDirective(nil), m.directives...)
}

// PendingDirectives 读面计数。
func (m *TechLeaderMailbox) PendingDirectives() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.directives)
}

// Overflow 返回指令溢出计数。
func (m *TechLeaderMailbox) Overflow() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.overflowDirectives
}

// ---- Supervisor ----

// TechLeaderConfig 是 ADVISOR(b) 触发策略配置（详设 §5.2 子集）。
type TechLeaderConfig struct {
	// Enabled 是否启用 b 评估（false 时 gate 回退直连 finish = Part I 语义）。
	Enabled bool
	// EvalWindow 是非关键信号（step_checkpoint）的最小轮次间隔；
	// 0 = 每次非关键信号都评估（测试用）；<0 用 DefaultEvalWindow。
	EvalWindow int
}

// DefaultTechLeaderConfig 返回生产默认（≤1 次/3-5 轮，控制 b 回合频率）。
func DefaultTechLeaderConfig() TechLeaderConfig {
	return TechLeaderConfig{Enabled: true, EvalWindow: DefaultEvalWindow}
}

// Supervisor 是 DS-A2A 编排者（goal 域内的 PeerSessionManager 切片）。
type Supervisor struct {
	ctl       *Controller
	mailbox   *TechLeaderMailbox
	evaluator TLEvaluator
	recorder  TLRoundRecorder
	cfg       TechLeaderConfig
	now       func() int64

	mu sync.Mutex

	// sessionID 是本次评审所在的主会话（工作区）坐标：随 b 回合输入下发（评审者
	// 的执行面据此绑定项目根与角色会话）。空 = 宿主未接通（执行面退回无工具评审）。
	// 由 SetSessionID 在 bundle 构造/装配点写入，与 Supervisor 同生命周期。
	sessionID string

	advisor *AdvisorSession // b（懒 bind：首次回合/快照前创建）

	execSeq            uint64         // a 事件账本水位（EXEC 唯一账本源，协议 §7.1）
	lastSyncedProgress int            // 控制器增量补帧游标（progress 条数；headless goal_update 无接线时的差异帧）
	pendingWork        []workProgress // 待抽帧的 EXEC 工作进展（turn_completed.Detail；回合前 flush，见 MaxWorkFrames）

	// inFlight / inFlightAt 是**当前 b 回合进行中**的正文近端（评审没结束就看得到）。
	//
	// 为什么要有它：b 回合的执行面是流式的（seelebridge 角色会话 ChatStream），但旧实现
	// 把 onChunk 传成 nil——分片被丢掉，前端只能在回合结束后拿到终局裁决，于是"渲染不
	// 及时"。这里按同一次调用内的 ctx 回调把分片收进一个**有界近端**，作为只读快照
	// 暴露（TLState.InFlight）；前端在 peer=evaluating 期间轮询快照即可看到进行中的正文。
	// 它不参与任何裁决：裁决仍然只来自 Evaluate 的返回值（TLDirective）。
	//
	// 锁：inFlightMu 是 s.mu 的**叶子**（只在 s.mu 之内取、从不反向）。旧实现依赖"流式
	// 回调与持 s.mu 者同 goroutine"这条**跨包契约**（seelebridge 把 spec.OnDelta 原样交给
	// 引擎，无从强制），引擎换个 goroutine 回调 onChunk 就是 s.inFlight 的数据竞争；
	// 自带短锁后该契约不再需要（2026-09-29 锁面审计 §2.2 附带缺口）。
	inFlightMu sync.Mutex
	inFlight   string
	inFlightAt int64
	// inFlightSteps 是**本轮/最近一轮** b 回合的过程步骤（工具调用 + 返回），
	// 由执行段的 ReAct 钩子经 ctx 回调写入（见 tl_steps.go）。与 inFlight 正文
	// 的区别：正文在回合结束清空（权威正文是裁决行），步骤保留到**下一轮开始**
	// 才换代——回合跑完后用户仍能看到"刚才评审做了什么"。
	// 锁：与 inFlight 同锁（inFlightMu = s.mu 的叶子）。
	inFlightSteps []TLStep

	// roundInFlight 是回合租约：true = 已有一轮 b 评审在执行段（s.mu 之内准入、
	// s.mu 之外执行）。它就是"回合不可重入"的显式表示——旧实现靠"RunEval 锁住
	// 整轮"顺带实现互斥，代价是 s.mu 横跨模型调用（同 goroutine 回调 s.mu 即自锁死，
	// 另一 goroutine 侧的 s.mu→roundGate 与 roundGate→s.mu 成环 = ABBA）。
	//
	// 与 peer.State=PeerEvaluating 的分工：peer 的状态是**给外部看的自述**（面板
	// 显示"正在评审"），本字段是**准入判定的事实**（谁可以进执行段）。二者同锁更新，
	// 但判重用本字段：状态可能因为一次失败回合被写回，租约只由准入/提交两段掌握。
	roundInFlight bool

	turnsSinceEval int
	evalCount      int64
	lastEvalAt     int64
	lastEvalGoalID string
}

// noteInFlight 记一段 b 回合的进行中正文。
//
// 它在 s.mu 之内被调用（准入段 beginRoundLocked 的补帧路径），但**不依赖**这一点：写入由
// inFlightMu（s.mu 的叶子）保护，因此引擎在不同 goroutine 上回调 onChunk 也不会与
// Snapshot 读取形成数据竞争。执行段（evaluateRound）刻意不持 s.mu，回调走的正是那条路。
//
// 只保留近端（MaxInFlightRunes）：in-flight 是"当前写到哪"的只读快照，不是完整
// 回合正文——完整原文仍由 recorder 落 role draft。它刻意不触发任何推送：前端在
// peer=evaluating 期间轮询快照即可，后端不需要为每个分片做一次投影。
func (s *Supervisor) noteInFlight(delta string) {
	if strings.TrimSpace(delta) == "" {
		return
	}
	s.inFlightMu.Lock()
	defer s.inFlightMu.Unlock()
	s.inFlight = boundInFlightRunes(s.inFlight+delta, MaxInFlightRunes)
	s.inFlightAt = s.now()
}

// clearInFlight 清空进行中正文（回合结束：权威正文是裁决行）。
func (s *Supervisor) clearInFlight() {
	s.inFlightMu.Lock()
	defer s.inFlightMu.Unlock()
	s.inFlight = ""
	s.inFlightAt = 0
}

// boundInFlightRunes 把进行中正文截到近端 max 个 rune（超出时前置省略标记）。
func boundInFlightRunes(text string, max int) string {
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return "…" + string(runes[len(runes)-max:])
}

// workProgress 是一条待抽帧的 EXEC 工作进展（ref_seq 在 flush 时按水位分配）。
type workProgress struct {
	Source string
	Detail string
	At     int64
}

// TLRoundRecord 是一次 b 回合的原文（上下文 = 送给 b 的原文；输出 = b 的原始回答）。
type TLRoundRecord struct {
	Trigger string
	RefSeq  uint64
	Context string
	Output  string
}

// TLRoundRecorder 记录 b 回合原文（装配侧注入：写入 tl 角色 draft 并由 sequencer
// 发布到主文档 + floor；nil = 不记录）。每回合都调用，保证"存上下文原本的内容"。
type TLRoundRecorder interface {
	RecordTLRound(ctx context.Context, record TLRoundRecord) error
	// RecordMainTurn 在 b 回合把发言权交还 a 时发布"本轮由 EXEC 主持"标记
	// （TL-main loop 的收尾由 TL 裁决决定：verdict_done/escalate_human 等终态
	// 不再交还，故不发布）。
	RecordMainTurn(ctx context.Context, record MainTurnRecord) error
}

// MainTurnRecord 是一次 a（EXEC）回合的发布标记（b 交还发言权时产生）。
type MainTurnRecord struct {
	Trigger   string
	RoundID   uint64
	Directive DirectiveKind
}

// loopContinues 报告该裁决是否把发言权交还 EXEC（终态裁决结束循环）。
func loopContinues(kind DirectiveKind) bool {
	switch kind {
	case DirectiveVerdictDone, DirectiveEscalateHuman, DirectiveDeny:
		return false
	}
	return true
}

// SetRoundRecorder 注入 b 回合记录器（装配根在首次会话启动前调用；幂等）。
func (s *Supervisor) SetRoundRecorder(recorder TLRoundRecorder) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.recorder = recorder
	s.mu.Unlock()
}

// SetSessionID 写入本次评审所在的主会话（工作区）坐标：b 回合输入会带上它，评审者
// 的执行面（seelebridge）据此绑定同一项目根、按只读口径跑一轮带工具的评审。
//
// 只影响**尚未 bind** 的 b（已 bind 的 peer 保持自己的坐标），因此调用点应是
// Supervisor 的构造/装配处，而不是每个回合。
func (s *Supervisor) SetSessionID(sessionID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.sessionID = strings.TrimSpace(sessionID)
	if s.advisor != nil {
		s.advisor.SessionID = s.sessionID
	}
	s.mu.Unlock()
}

// directiveText 把 b 的裁决渲染成原文 JSON（记录与展示用，不截断）。
func directiveText(directive TLDirective) string {
	if raw, err := json.Marshal(directive); err == nil {
		return string(raw)
	}
	return directive.Summary()
}

// NewSupervisor 构造编排者（config 零值用默认；mailbox 自动新建）。
func NewSupervisor(ctl *Controller, evaluator TLEvaluator, cfg TechLeaderConfig) *Supervisor {
	if ctl == nil {
		panic("goal: NewSupervisor 需要非 nil controller")
	}
	if cfg.EvalWindow < 0 {
		cfg.EvalWindow = DefaultEvalWindow
	}
	if cfg.Enabled && evaluator == nil {
		cfg.Enabled = false // 无评估器即视为未启用（B4 缺席默认直连语义）
	}
	return &Supervisor{
		ctl:       ctl,
		mailbox:   NewTechLeaderMailbox(0),
		evaluator: evaluator,
		cfg:       cfg,
		now:       TLNow(),
	}
}

// Mailbox 返回 b→a 指令队列（排空/读面）。
func (s *Supervisor) Mailbox() *TechLeaderMailbox { return s.mailbox }

// Enabled 报告 b 是否可用（有评估器且配置开启）。
func (s *Supervisor) Enabled() bool { return s.cfg.Enabled && s.evaluator != nil }

// advisorFor 返回（必要时创建）b 会话；要求存在 active goal。调用方持 s.mu。
func (s *Supervisor) advisorForLocked(active *GoalRecord) *AdvisorSession {
	if s.advisor != nil && s.advisor.State == PeerReaped {
		s.advisor = nil // goal 域终态后 b 已 reap；新 goal 重新 bind
	}
	if s.advisor == nil {
		peer := &AdvisorSession{State: PeerBound, SessionID: s.sessionID}
		now := s.now()
		s.execSeq++ // a 事件：goal.start 锚点（协议 §4 必进）
		peer.markBound(fmt.Sprintf("advisor-%s", active.ID), goalFrameOf(active), s.execSeq, now)
		peer.Head = s.execSeq
		s.lastSyncedProgress = len(active.Progress)
		s.advisor = peer
	}
	return s.advisor
}

// Notify 登记一条 a 事件（EXEC 账本）并按触发策略决定是否自动执行 b 回合。
//   - turn_completed：推进水位、不帧化、不评估（用户例子中 a:6,7 而 b 不动的跳帧）；
//     携带 Detail（本轮 EXEC 工作正文摘要）时先入待抽帧缓冲，b 下次回合前作为
//     work.progress 帧下发——b 因此能看到 EXEC 干的活，而不只是目标陈述。
//   - goal_updated：推进水位、不评估（差异帧在下个回合前补帧）；
//   - step_checkpoint：受 eval_window 抑制；到窗即回合；
//   - 关键信号（compacted/budget/approval/terminal）：立即回合。
//
// 三段式（2026-09-29）：登记与准入在 s.mu 内，**评估在 s.mu 外**。Notify 因此
// 再也不会被"另一个回合正在评审"拖住——登记落在账本上，评估留给下一次触发
// （ErrRoundInFlight 在这里是**正常结果**而不是错误）。
func (s *Supervisor) Notify(ctx context.Context, signal TLEvalSignal) error {
	if err := signal.Validate(); err != nil {
		return err
	}
	if _, ok := s.ctl.ActiveGoal(); !ok {
		return nil
	}
	if signal.At <= 0 {
		signal.At = s.now()
	}

	s.mu.Lock()
	s.execSeq++ // a 事件登记（水位推进；turn 跳帧）
	if s.advisor != nil {
		s.advisor.Head = s.execSeq
	}
	if signal.Kind == SignalTurnCompleted {
		s.noteWorkProgressLocked(signal)
	}
	plan, planErr := s.beginAutoRoundLocked(ctx, signal)
	s.mu.Unlock()

	if plan == nil {
		// 不评估是常态（turn 跳帧 / goal_updated / 窗口抑制 / 未启用 / 已有回合
		// 在飞）：事件已经登记在账本上，下一次触发会把它一起带进 b 的输入。
		return planErr
	}
	_, err := s.completeRound(ctx, plan)
	return err
}

// noteWorkProgressLocked 把 turn_completed 的工作正文摘要入待抽帧缓冲（调用方
// 持 s.mu）。同一内容连续上报只保留一条（iteration_complete 与 chat_end 会报
// 同一轮），缓冲超过 MaxWorkFrames 丢最旧——b 上下文按帧数有界。
func (s *Supervisor) noteWorkProgressLocked(signal TLEvalSignal) {
	detail := strings.TrimSpace(signal.Detail)
	if detail == "" {
		return
	}
	if n := len(s.pendingWork); n > 0 && s.pendingWork[n-1].Detail == detail {
		s.pendingWork[n-1].At = signal.At
		return
	}
	s.pendingWork = append(s.pendingWork, workProgress{Source: signal.Source, Detail: detail, At: signal.At})
	if len(s.pendingWork) > MaxWorkFrames {
		s.pendingWork = append([]workProgress(nil), s.pendingWork[len(s.pendingWork)-MaxWorkFrames:]...)
	}
}

// flushWorkProgressLocked 在 b 回合前把缓冲的 EXEC 工作进展一次性抽成
// work.progress 帧（ref_seq 按 execSeq 水位单调分配）。调用方持 s.mu。
func (s *Supervisor) flushWorkProgressLocked(peer *AdvisorSession, now int64) error {
	if len(s.pendingWork) == 0 {
		return nil
	}
	pending := s.pendingWork
	s.pendingWork = nil
	for _, item := range pending {
		s.execSeq++
		peer.Head = s.execSeq
		at := item.At
		if at <= 0 {
			at = now
		}
		if _, err := peer.appendFrame(Frame{
			Kind: FrameWorkProgress, RefSeq: s.execSeq, At: at,
			Source: item.Source, Detail: item.Detail,
		}); err != nil {
			return err
		}
	}
	return nil
}

// beginAutoRoundLocked 是 Notify 的"要不要评 + 准入"合一判定（调用方持 s.mu）。
//
// 返回 (nil, nil) = 本轮不评估，不是错误：turn 跳帧 / goal_updated 不评、窗口
// 未到不评、b 未启用不评、**已有回合在飞不评**（登记已完成，评估留给下一次触发）。
// 只有准入里的真错误（帧账本 append 失败、b 输入构建失败）才带 err 返回。
func (s *Supervisor) beginAutoRoundLocked(ctx context.Context, signal TLEvalSignal) (*roundPlan, error) {
	switch signal.Kind {
	case SignalTurnCompleted:
		s.turnsSinceEval++
		return nil, nil
	case SignalGoalUpdated:
		return nil, nil
	}
	if !s.Enabled() {
		return nil, nil
	}
	if !IsCriticalSignal(signal.Kind) && s.cfg.EvalWindow > 0 && s.turnsSinceEval < s.cfg.EvalWindow {
		return nil, nil
	}
	plan, err := s.beginRoundLocked(ctx, "signal:"+string(signal.Kind), signal)
	switch {
	case errors.Is(err, ErrRoundInFlight), errors.Is(err, ErrNoActiveGoal):
		// 这两条在 Notify 语义下都等于"本轮不评"：登记照旧、不排队、不报错。
		return nil, nil
	default:
		return plan, err
	}
}

// RunEval 强制执行一次 b 回合（外部/边界触发：终态 gate、审批预筛、headless goal_tl_eval）。
//
// 三段式（2026-09-29 锁面审计 §4.1 整改）：准入与提交在 s.mu 内，**评估本身在 s.mu 外**。
// 旧实现把整轮锁在 RunEval 里，s.mu 因此横跨模型调用——同 goroutine 上一个"回合内
// 回头找 Supervisor"的回调（流式分片/迭代钩子/工具）走到 s.mu 就是自锁死；另一
// goroutine 侧的 s.mu → roundGate 与 roundGate → s.mu 就是环（ABBA）。执行段挪出
// s.mu 之后环在结构上不成立：**不存在"持 s.mu 等 roundGate"的一方**，于是也不需要
// 那条反向边先消失。
func (s *Supervisor) RunEval(ctx context.Context, trigger string) (TLDirective, error) {
	return s.runRound(ctx, trigger, TLEvalSignal{Kind: SignalStepCheckpoint, Source: "manual_eval"})
}

// runRound 是 b 回合的唯一入口（RunEval / Notify / 终态 gate / 审批预筛都走它）：
// 准入（s.mu 内）→ 执行（s.mu 外）→ 提交（s.mu 内）→ 记录（s.mu 外）。
func (s *Supervisor) runRound(ctx context.Context, trigger string, signal TLEvalSignal) (TLDirective, error) {
	plan, err := s.beginRound(ctx, trigger, signal)
	if err != nil {
		return TLDirective{}, err
	}
	return s.completeRound(ctx, plan)
}

// roundPlan 是一次 b 回合的**准入快照**：准入段在 s.mu 内固化输入与环境，执行段
// 不再读共享状态；提交段据 goalStamp 复核"这一期间 goal 有没有变"（B 语义，见 commitRound）。
type roundPlan struct {
	trigger     string
	peer        *AdvisorSession
	embed       TLSessionEmbed
	inputText   string
	cached      int64
	inputTokens int64
	at          int64
	goalID      string
	goalStamp   string
}

// beginRound 是准入段：只拿 s.mu 一小段，做完判定与 b 输入构造就放掉。
func (s *Supervisor) beginRound(ctx context.Context, trigger string, signal TLEvalSignal) (*roundPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.beginRoundLocked(ctx, trigger, signal)
}

// beginRoundLocked 是准入段本体（调用方持 s.mu）：
// 判定（b 可用 / 有 active goal / **无回合在飞**）→ 懒 bind peer → Mirror on_eval
// 补 a 差异帧 + 工作进展帧 → 触发帧 → 渲染 b 自身输入 → 占回合租约。
// **不调评估器**：那是执行段的事（evaluateRound）。
func (s *Supervisor) beginRoundLocked(ctx context.Context, trigger string, signal TLEvalSignal) (*roundPlan, error) {
	if !s.Enabled() {
		return nil, ErrTLDisabled
	}
	if s.roundInFlight {
		return nil, ErrRoundInFlight
	}
	active, ok := s.ctl.ActiveGoal()
	if !ok {
		return nil, ErrNoActiveGoal
	}
	peer := s.advisorForLocked(active)
	now := s.now()
	plan := &roundPlan{
		trigger: trigger, peer: peer, at: now,
		goalID: active.ID, goalStamp: goalStampOf(active),
	}
	// 占回合租约（锁内）：以下任何提前返回都必须释放，否则回合闸门永久卡死。
	s.roundInFlight = true
	peer.State = PeerEvaluating

	// 1) 补 a 差异帧（on_eval：一次性同步区间内抽帧集）。
	if err := s.syncControllerDiffFramesLocked(peer, active, now); err != nil {
		s.abortRoundLocked(peer)
		return nil, err
	}
	// 1.5) 补 a 工作进展帧（turn_completed.Detail）：b 的输入因此包含 EXEC 实际
	// 干了什么（正文摘要/工具名），而不是只有目标陈述与打点。
	if err := s.flushWorkProgressLocked(peer, now); err != nil {
		s.abortRoundLocked(peer)
		return nil, err
	}
	// 2) 触发帧（本回合为何而评）。
	if frame, frameOK := frameForSignal(signal); frameOK {
		s.execSeq++
		peer.Head = s.execSeq
		if _, err := peer.appendFrame(Frame{
			Kind: frame, RefSeq: s.execSeq, At: now,
			Source: signal.Source, Detail: signal.Detail,
		}); err != nil {
			s.abortRoundLocked(peer)
			return nil, err
		}
	}

	// 3) b 回合输入 = b 自身上下文（协议 C3 事务式；前缀稳定 → 命中可测）。
	embed, inputText := peer.renderEmbed(trigger)
	if err := embed.Validate(); err != nil {
		s.abortRoundLocked(peer)
		return nil, fmt.Errorf("%w: b 回合输入构建: %v", ErrInvalidArgument, err)
	}

	// 4) 缓存观测：相邻回合公共前缀即命中（协议 §2 C4）。
	if prevText := peer.cachedInputText; prevText != "" {
		plan.cached = estimateTokens(commonPrefix(prevText, inputText))
	}
	plan.inputTokens = estimateTokens(inputText)
	plan.embed, plan.inputText = embed, inputText
	peer.cachedInputText = inputText
	return plan, nil
}

// abortRoundLocked 释放回合租约并把 peer 落回稳态（准入失败路径；调用方持 s.mu）。
func (s *Supervisor) abortRoundLocked(peer *AdvisorSession) {
	s.roundInFlight = false
	if peer != nil && peer.State == PeerEvaluating {
		peer.State = PeerAdvisoryPending
	}
}

// completeRound 跑完一个已准入的回合：执行段（锁外）→ 提交段（锁内）→ 记录（锁外）。
func (s *Supervisor) completeRound(ctx context.Context, plan *roundPlan) (TLDirective, error) {
	directive, evalErr := s.evaluateRound(ctx, plan)
	directive, record, mainTurn, err := s.commitRound(ctx, plan, directive, evalErr)
	if record != nil {
		s.recordRound(ctx, record, mainTurn)
	}
	return directive, err
}

// evaluateRound 是执行段：**刻意不持 s.mu**。
//
// 这一段里跑的是模型调用 + b 的只读工具回合（真实耗时以秒/分钟计），期间
//   - 前端轮询的 Snapshot 拿得到 s.mu：面板在 evaluating 期间是**活的**，in-flight
//     近端因此真的读得到（旧实现把它锁在整轮之后，放行时已被 clearInFlight 清空，
//     等于从没亮过）；
//   - 回合内任何回头找 Supervisor 的回调（流式分片、迭代钩子、工具）走到 s.mu 时
//     不会自锁死（同 goroutine 重入非重入锁 = 死锁）；
//   - 与 roundGate 不再成环：不存在"持 s.mu 等 roundGate"的一方。
func (s *Supervisor) evaluateRound(ctx context.Context, plan *roundPlan) (TLDirective, error) {
	// 本轮的**进行中**观察面：模型分片不再被丢弃（旧实现给 ChatStream 传 nil），
	// 而是经 ctx 回调进 in-flight 近端，作为只读快照暴露给前端（快照查看）。
	// defer 清理：本回合任何返回路径（含错误）都不把中间态留给下一次裁决。
	ctx = WithTLDeltaSink(ctx, s.noteInFlight)
	// 本轮的过程观察面：b 在角色会话里调的**只读工具**（read_file/grep/glob…）经
	// ReAct 钩子进步骤列表，作为只读快照暴露给前端（"评审过程"面板）。
	// 与正文分片的分工见 tl_steps.go；步骤**不在回合结束清空**（下一轮开始才换代），
	// 因此回合跑完后仍可查看"刚刚评审做了什么"。
	s.clearRoundSteps()
	ctx = WithTLStepSink(ctx, s.noteStep)
	defer s.clearInFlight()
	return s.evaluator.Evaluate(ctx, plan.embed)
}

// commitRound 是提交段入口：拿 s.mu 一小段做提交（body 见 commitRoundLocked）。
func (s *Supervisor) commitRound(ctx context.Context, plan *roundPlan, directive TLDirective, evalErr error) (TLDirective, *TLRoundRecord, *MainTurnRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.commitRoundLocked(ctx, plan, directive, evalErr)
}

// commitRoundLocked 是提交段本体（调用方持 s.mu）：复核 goal → 校验裁决 → 落 b 回合段
// + corr 信封 → 记账。记录器**不在这一段**调用（见 recordRound）：宿主/落盘活压在回合
// 锁上会又造一条 s.mu → 宿主锁的边。
func (s *Supervisor) commitRoundLocked(ctx context.Context, plan *roundPlan, directive TLDirective, evalErr error) (TLDirective, *TLRoundRecord, *MainTurnRecord, error) {
	peer := plan.peer
	s.abortRoundLocked(peer) // 释放租约；peer.State 由下面的分支写终值

	if evalErr != nil {
		// 分类不要在这里写死：**裁决不可用**（ErrBadDirective：原文不可解析 / 域校验
		// 不过 / goal 漂移）与**缺席**（429/超时/回合失败）是两件事。把 429/超时的标签
		// 贴到前者身上，用户读到的收口说明会与实际状态相反（gate.go 按同一条边界分支）。
		switch {
		case errors.Is(evalErr, ErrBadDirective):
			return TLDirective{}, nil, nil, fmt.Errorf("b 回合已作答但裁决不可用: %w", evalErr)
		case errors.Is(evalErr, ErrRoundGoalGone):
			return TLDirective{}, nil, nil, evalErr // 保留"结论丢弃"的可判定语义（不贴缺席标签）
		default:
			return TLDirective{}, nil, nil, fmt.Errorf("b 回合失败(429/超时 → B4 缺席矩阵): %w", evalErr)
		}
	}

	if directive.At <= 0 {
		directive.At = plan.at
	}
	if directive.GoalID == "" {
		directive.GoalID = plan.goalID
	}
	directive.Corr = peer.nextCorr()
	if err := directive.Validate(); err != nil {
		return TLDirective{}, nil, nil, fmt.Errorf("%w: %v", ErrBadDirective, err)
	}
	if directive.GoalID != plan.goalID {
		return TLDirective{}, nil, nil, fmt.Errorf("%w: goal 漂移（directive %s ≠ active %s）", ErrBadDirective, directive.GoalID, plan.goalID)
	}

	// 5) 复核顶栈 goal（执行段在锁外，这一期间 a/人类可以改或收口 goal）：
	//   - 收口 / 取消（栈空，或顶栈已换成别的 goal）→ 这一回合的结论无处落地：
	//     **丢弃**（不发 corr 信封、不落回合段、不记录）= B 语义"取消 → 丢弃结论"。
	//     必须显式判的后果：gate 否则会拿一份属于旧 goal 的裁决去 Finish **新**的栈顶。
	//   - 同一个 goal 被改（更新）→ 结论仍然有效，但 b 的记忆要跟上：补一条
	//     goal.update 差异帧（B 语义"变更 → 做出 update"），下一回合的输入即含新事实。
	active, ok := s.ctl.ActiveGoal()
	if !ok {
		return TLDirective{}, nil, nil, fmt.Errorf("%w: b 回合期间 goal 已收口", ErrRoundGoalGone)
	}
	if active.ID != plan.goalID {
		return TLDirective{}, nil, nil, fmt.Errorf("%w: b 回合期间顶栈 goal 已更换（%s → %s）", ErrRoundGoalGone, plan.goalID, active.ID)
	}
	goalChanged := goalStampOf(active) != plan.goalStamp

	// 6) b 自身回合段 append（= 用户例子 6(b)）+ corr 信封发布（不回写 goal 共享状态）。
	refSeq := peer.Applied
	peer.appendRound(Round{
		At:           plan.at,
		Trigger:      plan.trigger,
		RefSeq:       refSeq,
		Corr:         directive.Corr,
		Kind:         directive.Kind,
		Summary:      directive.Summary(),
		InputTokens:  plan.inputTokens,
		CachedTokens: plan.cached,
	})
	s.mailbox.PublishDirective(directive)
	if goalChanged {
		s.noteGoalChangedLocked(peer, active)
	}
	peer.Cache = cacheStatsOf(peer.Rounds)
	peer.State = PeerAdvisoryPending
	s.evalCount++
	s.lastEvalAt = plan.at
	s.lastEvalGoalID = active.ID
	s.turnsSinceEval = 0

	// 每回合记录 b 看到的原文与它的原始回答（在锁外交给记录器；持久化/展示失败
	// 不阻断治理）。终态裁决不交还发言权，因此不产生 main_turn 记录。
	record := &TLRoundRecord{
		Trigger: plan.trigger, RefSeq: refSeq, Context: plan.inputText,
		Output: directiveText(directive),
	}
	var mainTurn *MainTurnRecord
	if loopContinues(directive.Kind) {
		mainTurn = &MainTurnRecord{Trigger: plan.trigger, RoundID: refSeq, Directive: directive.Kind}
	}
	return directive, record, mainTurn, nil
}

// noteGoalChangedLocked 在提交段发现"顶栈 goal 在回合期间被改过"时补一条
// goal.update 差异帧（B 语义：变更 → 做出 update，而不是丢弃、也不是假装没发生）。
//
// 补帧游标同时对齐到当前 progress 条数：下一次准入的 syncControllerDiffFramesLocked
// 因此不会为同一段差异再补一遍。补帧失败只吞掉——帧账本是"可重补的近似"，本回合的
// 结论以裁决为准，不因为一条差异帧写不进去而作废。
func (s *Supervisor) noteGoalChangedLocked(peer *AdvisorSession, active *GoalRecord) {
	s.execSeq++
	peer.Head = s.execSeq
	if _, err := peer.appendFrame(Frame{
		Kind: FrameGoalUpdated, RefSeq: s.execSeq, At: s.now(),
		Source: "goal_updated_during_round", Detail: latestProgressSummary(active),
	}); err != nil {
		return
	}
	s.lastSyncedProgress = len(active.Progress)
}

// recordRound 把回合原文交给记录器——**锁外**。两重理由：① 记录器是宿主/落盘活
// （tl role draft → sequencer → 主文档），压在 s.mu 上就是让整轮治理等文件 I/O；
// ② 它可能回头取宿主侧锁（ViewMu 等），在 s.mu 内调用即又造一条 s.mu → 宿主锁的边。
// best-effort：记录失败不阻断治理（与旧行为一致，只是挪出了锁）。
func (s *Supervisor) recordRound(ctx context.Context, record *TLRoundRecord, mainTurn *MainTurnRecord) {
	s.mu.Lock()
	recorder := s.recorder
	s.mu.Unlock()
	if recorder == nil {
		return
	}
	_ = recorder.RecordTLRound(ctx, *record)
	if mainTurn != nil {
		_ = recorder.RecordMainTurn(ctx, *mainTurn)
	}
}

// goalStampOf 是顶栈 goal 的轻量指纹：回答"这一回合执行期间 goal 变了吗"。
//
// 只取会影响裁决依据的字段（身份 / 状态 / 标题 / 正文 / 完成条件 / 进度尾条）。
// 不拿 UpdatedAt 单判：域时间是秒级（time.Now().Unix），同一秒内的两次更新会
// 得到同一个时间戳——"改过但判不出"正是这里最不该有的漏。
func goalStampOf(record *GoalRecord) string {
	if record == nil {
		return ""
	}
	lastProgress := ""
	if count := len(record.Progress); count > 0 {
		lastProgress = record.Progress[count-1].Content
	}
	return strings.Join([]string{
		record.ID, string(record.Status), record.Title, record.Statement,
		strings.Join(record.Acceptance, "\x1e"), lastProgress,
		fmt.Sprintf("%d", len(record.Progress)),
	}, "\x1f")
}

// syncControllerDiffFramesLocked 把 headless 直接改 goal（无 Notify 接线）的差异补成
// goal.update 帧（on_eval：b 回合前一次性同步；diff 游标 = progress 条数，不依赖时间戳）。
// 保证 b 从 a 事件学习而非实时偷看（协议 C3/C5）。
func (s *Supervisor) syncControllerDiffFramesLocked(peer *AdvisorSession, active *GoalRecord, now int64) error {
	if len(active.Progress) <= s.lastSyncedProgress {
		return nil
	}
	detail := latestProgressSummary(active)
	s.execSeq++
	peer.Head = s.execSeq
	if _, err := peer.appendFrame(Frame{
		Kind: FrameGoalUpdated, RefSeq: s.execSeq, At: now,
		Source: "goal_update", Detail: detail,
	}); err != nil {
		return err
	}
	s.lastSyncedProgress = len(active.Progress)
	return nil
}

// latestProgressSummary 取最近一条 progress 作 update 帧详情（有界）。
func latestProgressSummary(active *GoalRecord) string {
	if active == nil || len(active.Progress) == 0 {
		return fmt.Sprintf("title=%s", active.Title)
	}
	item := active.Progress[len(active.Progress)-1]
	return fmt.Sprintf("title=%s progress=[%s] %s", active.Title, item.Kind, item.Content)
}

// unbindIfTerminal 在 goal 收口后置 b 终态（协议 §9：done/abort → unbind + reap 会话对象，
// b 回合记录留在审计/快照不再展示）。
func (s *Supervisor) unbindIfTerminal(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.advisor != nil && s.advisor.State != PeerReaped {
		s.advisor.State = PeerReaped
		s.advisor.Reason = reason
	}
}

// Snapshot 返回 b/治理状态快照（headless goal_tl_snapshot / 前端 ADVISOR 面板素材）。
func (s *Supervisor) Snapshot() TLState {
	s.inFlightMu.Lock()
	inFlight, inFlightAt := s.inFlight, s.inFlightAt
	steps := append([]TLStep(nil), s.inFlightSteps...)
	s.inFlightMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	state := TLState{
		Enabled:            s.Enabled(),
		EvalCount:          s.evalCount,
		LastEvalAt:         s.lastEvalAt,
		LastEvalGoalID:     s.lastEvalGoalID,
		PendingDirectives:  s.mailbox.PendingDirectives(),
		OverflowDirectives: s.mailbox.Overflow(),
		TurnsSinceEval:     s.turnsSinceEval,
		InFlight:           inFlight,
		InFlightChars:      len([]rune(inFlight)),
		InFlightAt:         inFlightAt,
		RoundSteps:         steps,
	}
	if active, ok := s.ctl.ActiveGoal(); ok {
		state.ActiveGoalID = active.ID
		state.ActiveGoalTitle = active.Title
	}
	if s.advisor != nil {
		state.Peer = s.advisor.State
		state.AppliedSeq = s.advisor.Applied
		state.HeadSeq = s.advisor.Head
		state.UnbindReason = s.advisor.Reason
		if s.advisor.Head > s.advisor.Applied {
			state.Behind = s.advisor.Head - s.advisor.Applied
		}
		state.FrameCount = len(s.advisor.Frames)
		state.RoundCount = len(s.advisor.Rounds)
		state.Cache = s.advisor.Cache
		state.Frames = append([]Frame(nil), s.advisor.Frames...)
		state.Rounds = append([]Round(nil), s.advisor.Rounds...)
	}
	return state
}

// frameForSignal 映射执行信号 → 抽帧集（协议 §4 默认集；turn/goal_updated 无帧）。
func frameForSignal(signal TLEvalSignal) (FrameKind, bool) {
	switch signal.Kind {
	case SignalStepCheckpoint:
		return FrameStepCheckpoint, true
	case SignalContextCompacted:
		return FrameContextCompacted, true
	case SignalApprovalAsked:
		return FrameApprovalRequested, true
	case SignalTerminalProposal:
		return FrameTerminalProposed, true
	}
	return "", false
}

// cacheStatsOf 汇总回合缓存观测（命中回升可验证：cached/input 单调不降趋稳）。
func cacheStatsOf(rounds []Round) CacheStats {
	stats := CacheStats{Rounds: len(rounds)}
	for _, round := range rounds {
		stats.TotalInputTokens += round.InputTokens
		stats.TotalCached += round.CachedTokens
		stats.LastInputTokens = round.InputTokens
		stats.LastCached = round.CachedTokens
	}
	if stats.LastInputTokens > 0 {
		stats.HitRatio = int(stats.LastCached * 100 / stats.LastInputTokens)
	}
	return stats
}

// commonPrefix 返回两个输入文本的公共前缀（相邻回合命中段；协议 C4 全等复核即全命中）。
func commonPrefix(left, right string) string {
	if left == "" || right == "" {
		return ""
	}
	max := len(left)
	if len(right) < max {
		max = len(right)
	}
	index := 0
	for index < max && left[index] == right[index] {
		index++
	}
	return left[:index]
}

// ---- 读面 ----

// TLState 是 Supervisor 读面快照（前端 ADVISOR 面板/审计素材）。
// 读面数值字段不带 omitempty：wire 稳定（0 也下发），避免客户端复用解码残留旧值。
type TLState struct {
	Enabled            bool       `json:"enabled"`
	ActiveGoalID       string     `json:"active_goal_id,omitempty"`
	ActiveGoalTitle    string     `json:"active_goal_title,omitempty"`
	Peer               PeerState  `json:"peer_state"`
	AppliedSeq         uint64     `json:"applied_seq"`
	HeadSeq            uint64     `json:"head_seq"`
	Behind             uint64     `json:"behind"`
	UnbindReason       string     `json:"unbind_reason"`
	FrameCount         int        `json:"frame_count"`
	RoundCount         int        `json:"round_count"`
	Cache              CacheStats `json:"cache,omitempty"`
	Frames             []Frame    `json:"frames,omitempty"`
	Rounds             []Round    `json:"rounds,omitempty"`
	EvalCount          int64      `json:"eval_count"`
	LastEvalAt         int64      `json:"last_eval_at,omitempty"`
	LastEvalGoalID     string     `json:"last_eval_goal_id,omitempty"`
	PendingDirectives  int        `json:"pending_directives"`
	OverflowDirectives int64      `json:"overflow_directives"`
	TurnsSinceEval     int        `json:"turns_since_eval"`
	// InFlight / InFlightChars / InFlightAt 是当前 b 回合进行中的正文近端（只读快照）。
	// 回合结束即清空：裁决落地后，权威正文是 tl_directive 行，不是这段中间态。
	InFlight      string `json:"in_flight,omitempty"`
	InFlightChars int    `json:"in_flight_chars,omitempty"`
	InFlightAt    int64  `json:"in_flight_at,omitempty"`
	// RoundSteps 是**本轮/最近一轮** b 回合的过程步骤（工具调用 + 返回）。
	// 与 InFlight 的区别：InFlight 是模型正文的近端（回合结束清空），RoundSteps
	// 是"评审者做了什么"的可核对事实（保留到下一轮开始才换代）。
	RoundSteps []TLStep `json:"round_steps,omitempty"`
}

// MaxInFlightRunes 是进行中正文的可见上限（保留**近端**：in-flight 的价值在
// "当前写到哪"，不是完整回合正文；完整原文仍由 TLRecorder 落 role draft）。
const MaxInFlightRunes = 1200
