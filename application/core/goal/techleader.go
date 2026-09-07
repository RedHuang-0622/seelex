package goal

// techleader.go — DS-A2A 双会话治理的 goal 域编排（协议 v0.1 + 详设 §4 落地）。
//
// 结构（替换旧"同会话双角色共享上下文"实现：旧的 Supervisor 每次回合从 Controller 实时
// 重建 goal 帧 + SetSessionTail 喂 a 尾窗 + AppendDirective 把 TL 摘要写回 goal 共享指令环）：
//
//	Supervisor = goal 域内的 PeerSessionManager（详设 §4）：EXEC(a) 侧唯一编排者
//	  - execSeq    : a 事件账本水位（turn/checkpoint/goal_update 均登记；turn 跳帧不帧化）
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
	"fmt"
	"sync"
)

// ---- DirectiveBus（b→a corr 信封队列，协议 §5） ----

// TechLeaderMailbox 是 ADVISOR(b) → EXEC(a) 的有界指令队列：
//   - PublishDirective 发布带 corr 信封的结构化指令（满丢最旧 + 计数）；
//   - DrainDirectives 一次性排空（corr 幂等：排空即消费，重发由 corr 审计去重）。
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
	cfg       TechLeaderConfig
	now       func() int64

	mu sync.Mutex

	advisor *AdvisorSession // b（懒 bind：首次回合/快照前创建）

	execSeq           uint64 // a 事件账本水位（EXEC 唯一账本源，协议 §7.1）
	lastSyncedProgress int   // 控制器增量补帧游标（progress 条数；headless goal_update 无接线时的差异帧）

	turnsSinceEval int
	evalCount      int64
	lastEvalAt     int64
	lastEvalGoalID string
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
		peer := &AdvisorSession{State: PeerBound}
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
//   - goal_updated：推进水位、不评估（差异帧在下个回合前补帧）；
//   - step_checkpoint：受 eval_window 抑制；到窗即回合；
//   - 关键信号（compacted/budget/approval/terminal）：立即回合。
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
	defer s.mu.Unlock()

	s.execSeq++ // a 事件登记（水位推进；turn 跳帧）
	if s.advisor != nil {
		s.advisor.Head = s.execSeq
	}
	return s.maybeAutoEvalLocked(ctx, signal)
}

func (s *Supervisor) maybeAutoEvalLocked(ctx context.Context, signal TLEvalSignal) error {
	switch signal.Kind {
	case SignalTurnCompleted:
		s.turnsSinceEval++
		return nil
	case SignalGoalUpdated:
		return nil
	}
	if !s.Enabled() {
		return nil
	}
	if !IsCriticalSignal(signal.Kind) && s.cfg.EvalWindow > 0 && s.turnsSinceEval < s.cfg.EvalWindow {
		return nil
	}
	_, err := s.runRoundLocked(ctx, "signal:"+string(signal.Kind), signal)
	return err
}

// RunEval 强制执行一次 b 回合（外部/边界触发：终态 gate、审批预筛、headless goal_tl_eval）。
func (s *Supervisor) RunEval(ctx context.Context, trigger string) (TLDirective, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runRoundLocked(ctx, trigger, TLEvalSignal{Kind: SignalStepCheckpoint, Source: "manual_eval"})
}

// runRoundLocked 执行一次 b 回合（调用方持 s.mu）：
// Mirror on_eval：先补 a 差异帧（goal.update 补帧）+ 触发帧 → b 上下文 append →
// 渲染 b 自身输入（锚点+帧+自身回合记忆）→ 评估 → corr 信封发布 → 缓存观测记录。
func (s *Supervisor) runRoundLocked(ctx context.Context, trigger string, signal TLEvalSignal) (TLDirective, error) {
	if !s.Enabled() {
		return TLDirective{}, ErrTLDisabled
	}
	active, ok := s.ctl.ActiveGoal()
	if !ok {
		return TLDirective{}, ErrNoActiveGoal
	}
	peer := s.advisorForLocked(active)
	peer.State = PeerEvaluating
	now := s.now()

	// 1) 补 a 差异帧（on_eval：一次性同步区间内抽帧集）。
	if err := s.syncControllerDiffFramesLocked(peer, active, now); err != nil {
		return TLDirective{}, err
	}
	// 2) 触发帧（本回合为何而评）。
	frame, frameOK := frameForSignal(signal)
	if frameOK {
		s.execSeq++
		peer.Head = s.execSeq
		if _, err := peer.appendFrame(Frame{
			Kind: frame, RefSeq: s.execSeq, At: now,
			Source: signal.Source, Detail: signal.Detail,
		}); err != nil {
			return TLDirective{}, err
		}
	}

	// 3) b 回合输入 = b 自身上下文（协议 C3 事务式；前缀稳定 → 命中可测）。
	embed, inputText := peer.renderEmbed(trigger)
	if err := embed.Validate(); err != nil {
		return TLDirective{}, fmt.Errorf("%w: b 回合输入构建: %v", ErrInvalidArgument, err)
	}

	// 4) 缓存观测：相邻回合公共前缀即命中（协议 §2 C4）。
	prevText := peer.cachedInputText
	cached := int64(0)
	if prevText != "" {
		cached = estimateTokens(commonPrefix(prevText, inputText))
	}
	inputTokens := estimateTokens(inputText)
	peer.cachedInputText = inputText

	directive, err := s.evaluator.Evaluate(ctx, embed)
	if err != nil {
		peer.State = PeerAdvisoryPending
		return TLDirective{}, fmt.Errorf("b 回合失败(429/超时 → B4 缺席矩阵): %w", err)
	}
	if directive.At <= 0 {
		directive.At = now
	}
	if directive.GoalID == "" {
		directive.GoalID = active.ID
	}
	directive.Corr = peer.nextCorr()
	if err := directive.Validate(); err != nil {
		return TLDirective{}, fmt.Errorf("%w: %v", ErrBadDirective, err)
	}
	if directive.GoalID != active.ID {
		return TLDirective{}, fmt.Errorf("%w: goal 漂移（directive %s ≠ active %s）", ErrBadDirective, directive.GoalID, active.ID)
	}

	// 5) b 自身回合段 append（= 用户例子 6(b)）+ corr 信封发布（不回写 goal 共享状态）。
	peer.appendRound(Round{
		At:           now,
		Trigger:      trigger,
		RefSeq:       peer.Applied,
		Corr:         directive.Corr,
		Kind:         directive.Kind,
		Summary:      directive.Summary(),
		InputTokens:  inputTokens,
		CachedTokens: cached,
	})
	s.mailbox.PublishDirective(directive)
	peer.Cache = cacheStatsOf(peer.Rounds)
	peer.State = PeerAdvisoryPending
	s.evalCount++
	s.lastEvalAt = now
	s.lastEvalGoalID = active.ID
	s.turnsSinceEval = 0
	return directive, nil
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
	s.mu.Lock()
	defer s.mu.Unlock()
	state := TLState{
		Enabled:           s.Enabled(),
		EvalCount:         s.evalCount,
		LastEvalAt:        s.lastEvalAt,
		LastEvalGoalID:    s.lastEvalGoalID,
		PendingDirectives: s.mailbox.PendingDirectives(),
		OverflowDirectives: s.mailbox.Overflow(),
		TurnsSinceEval:    s.turnsSinceEval,
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
	Enabled            bool        `json:"enabled"`
	ActiveGoalID       string      `json:"active_goal_id,omitempty"`
	ActiveGoalTitle    string      `json:"active_goal_title,omitempty"`
	Peer               PeerState   `json:"peer_state"`
	AppliedSeq         uint64      `json:"applied_seq"`
	HeadSeq            uint64      `json:"head_seq"`
	Behind             uint64      `json:"behind"`
	UnbindReason       string      `json:"unbind_reason"`
	FrameCount         int         `json:"frame_count"`
	RoundCount         int         `json:"round_count"`
	Cache              CacheStats  `json:"cache,omitempty"`
	Frames             []Frame     `json:"frames,omitempty"`
	Rounds             []Round     `json:"rounds,omitempty"`
	EvalCount          int64       `json:"eval_count"`
	LastEvalAt         int64       `json:"last_eval_at,omitempty"`
	LastEvalGoalID     string      `json:"last_eval_goal_id,omitempty"`
	PendingDirectives  int         `json:"pending_directives"`
	OverflowDirectives int64       `json:"overflow_directives"`
	TurnsSinceEval     int         `json:"turns_since_eval"`
}
