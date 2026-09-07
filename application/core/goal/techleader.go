package goal

// techleader.go：TechLeader A2A 的 MVP 实现（design.md §5 的可运行切片）。
//
// 结构：
//   - TechLeaderMailbox：有界通信信箱（信号/指令队列 + 溢出计数 + 排空）；
//     语义对齐设计 §5.1（有界命令队列、溢出不阻塞仅计数、状态可经 goal 栈重读追平）。
//     MVP 为同步实现（无独立 goroutine）：回合在 harness 边界（终态 gate / 审批预筛 /
//     信号触发）同步执行，保证确定性可测；生产接线升级为 CSP actor 时队列语义不变。
//   - Supervisor：TL 监督器——接收执行信号（Notify）、构建有界嵌入（每次强制带 goal 帧）、
//     按触发策略执行 TL 回合（evaluator 注入）、把指令发布回信箱并写入 goal 指令环。
//
// 锁序约定：Supervisor.mu → Controller.mu（回合内先记窗口再改 goal）；Controller 永不回调
// Supervisor，故无反向死锁。评估器调用在持有 Supervisor.mu 之外不成立——回合内调用
// evaluator 是唯一阻塞点，由注入方保证超时/有界（design §5.1 10s 护栏由装配层施加）。

import (
	"context"
	"fmt"
	"sync"
)

// ---- TechLeaderMailbox ----

// TechLeaderMailbox 是 exec ⇄ TL 的有界信箱：
//   - 信号（TLEvalSignal）入队有界，溢出丢弃最旧 + 计数（新状态可重读追平）；
//   - 指令（TLDirective）发布有界环形，溢出丢弃最旧 + 计数；
//   - 排空（DrainDirectives）对应 ChatStream 边界 mainagent 下一轮取指令。
type TechLeaderMailbox struct {
	mu sync.Mutex

	signals    []TLEvalSignal
	directives []TLDirective

	maxSignals    int
	maxDirectives int

	overflowSignals    int64
	overflowDirectives int64
}

// NewTechLeaderMailbox 构造有界信箱（cap ≤0 用默认常量）。
func NewTechLeaderMailbox(maxSignals, maxDirectives int) *TechLeaderMailbox {
	if maxSignals <= 0 {
		maxSignals = MaxSignalQueue
	}
	if maxDirectives <= 0 {
		maxDirectives = MaxDirectiveQueue
	}
	return &TechLeaderMailbox{
		signals:       make([]TLEvalSignal, 0, maxSignals),
		directives:    make([]TLDirective, 0, maxDirectives),
		maxSignals:    maxSignals,
		maxDirectives: maxDirectives,
	}
}

// EnqueueSignal 入队一条执行信号（有界；满时丢最旧并计数，不阻塞）。
func (m *TechLeaderMailbox) EnqueueSignal(signal TLEvalSignal) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.signals) >= m.maxSignals {
		m.signals = m.signals[1:]
		m.overflowSignals++
	}
	m.signals = append(m.signals, signal)
}

// TakeSignals 取走至多 limit 条待处理信号（供单回合嵌入；剩余保留给下一回合）。
func (m *TechLeaderMailbox) TakeSignals(limit int) []TLEvalSignal {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit <= 0 || len(m.signals) == 0 {
		return nil
	}
	if limit > len(m.signals) {
		limit = len(m.signals)
	}
	out := append([]TLEvalSignal(nil), m.signals[:limit]...)
	m.signals = m.signals[limit:]
	return out
}

// PublishDirective 发布一条 TL 指令（有界环形；满时丢最旧并计数）。
func (m *TechLeaderMailbox) PublishDirective(directive TLDirective) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.directives) >= m.maxDirectives {
		m.directives = m.directives[1:]
		m.overflowDirectives++
	}
	m.directives = append(m.directives, directive)
}

// DrainDirectives 排空全部待 mainagent 领取的指令（边界排空，幂等可重入）。
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

// PendingSignals / PendingDirectives 读面计数。
func (m *TechLeaderMailbox) PendingSignals() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.signals)
}

func (m *TechLeaderMailbox) PendingDirectives() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.directives)
}

func (m *TechLeaderMailbox) Overflow() (signals, directives int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.overflowSignals, m.overflowDirectives
}

// ---- Supervisor ----

// TechLeaderConfig 是 TL 触发策略配置（design §5.2 D5 MVP 子集）。
type TechLeaderConfig struct {
	// Enabled 是否启用 TL 评估（false 时 Notify 仅入队、gate 回退直连 finish）。
	Enabled bool
	// EvalWindow 是非关键信号（step_checkpoint）的最小轮次间隔；
	// 0 = 每次非关键信号都评估（测试用）；<0 用 DefaultEvalWindow。
	EvalWindow int
}

// DefaultTechLeaderConfig 返回生产默认（design：≤1 次/3-5 轮）。
func DefaultTechLeaderConfig() TechLeaderConfig {
	return TechLeaderConfig{Enabled: true, EvalWindow: DefaultEvalWindow}
}

// Supervisor 是 TechLeader 监督器。
type Supervisor struct {
	ctl       *Controller
	mailbox   *TechLeaderMailbox
	evaluator TLEvaluator
	cfg       TechLeaderConfig
	now       func() int64

	mu             sync.Mutex
	tail           []TurnBrief
	turnsSinceEval int
	evalPending    bool
	evalCount      int64
	lastEvalAt     int64
	lastEvalGoalID string
}

// NewSupervisor 构造监督器（config 零值用默认；mailbox nil 时新建默认信箱）。
func NewSupervisor(ctl *Controller, evaluator TLEvaluator, cfg TechLeaderConfig) *Supervisor {
	if ctl == nil {
		panic("goal: NewSupervisor 需要非 nil controller")
	}
	if cfg.EvalWindow < 0 {
		cfg.EvalWindow = DefaultEvalWindow
	}
	if cfg.Enabled && evaluator == nil {
		cfg.Enabled = false // 无评估器即视为未启用（回退直连语义）
	}
	return &Supervisor{
		ctl:       ctl,
		mailbox:   NewTechLeaderMailbox(0, 0),
		evaluator: evaluator,
		cfg:       cfg,
		now:       TLNow(),
	}
}

// Mailbox 返回信箱（排空/读面）。
func (s *Supervisor) Mailbox() *TechLeaderMailbox { return s.mailbox }

// Enabled 报告 TL 是否启用（有评估器且配置开启）。
func (s *Supervisor) Enabled() bool { return s.cfg.Enabled && s.evaluator != nil }

// SetSessionTail 更新有界会话尾窗（最近 ≤MaxEmbedTail 轮；由执行侧在回合前喂入）。
func (s *Supervisor) SetSessionTail(tail []TurnBrief) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(tail) > MaxEmbedTail {
		tail = tail[len(tail)-MaxEmbedTail:]
	}
	s.tail = append([]TurnBrief(nil), tail...)
}

// Notify 接收执行侧信号：无 active goal 时信号无意义（忽略，不排队、不评估）；
// 有 goal 时入队并按触发策略决定是否自动执行 TL 回合（关键信号立即；
// step_checkpoint 受 eval_window 约束；turn_completed/goal_updated 不触发）。
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
	s.mailbox.EnqueueSignal(signal)
	return s.maybeAutoEval(ctx, signal)
}

func (s *Supervisor) maybeAutoEval(ctx context.Context, signal TLEvalSignal) error {
	// 决策与执行都在 s.mu 下完成：评估器调用是唯一阻塞点（MVP 同步回合，
	// 装配层负责给 evaluator 加超时；design §5.1 10s 护栏）。
	s.mu.Lock()
	defer s.mu.Unlock()

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
	if s.evalPending {
		return nil
	}
	s.evalPending = true
	defer func() { s.evalPending = false }()
	_, err := s.runEvalLocked(ctx, "signal:"+string(signal.Kind))
	return err
}

// RunEval 强制执行一次 TL 回合（外部/边界触发：终态 gate、审批预筛、headless goal_tl_eval）。
func (s *Supervisor) RunEval(ctx context.Context, trigger string) (TLDirective, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runEvalLocked(ctx, trigger)
}

// runEvalLocked 在持锁下执行 TL 回合（调用方须持有 s.mu）。
// 构建嵌入（强制带 goal 帧）→ 评估 → 校验 → 发布信箱 + 写 goal 指令环；
// goal 帧由 Controller 实况读取（防遗忘），回合后窗口计数复位。
func (s *Supervisor) runEvalLocked(ctx context.Context, trigger string) (TLDirective, error) {
	if !s.Enabled() {
		return TLDirective{}, ErrTLDisabled
	}
	active, ok := s.ctl.ActiveGoal()
	if !ok {
		return TLDirective{}, ErrNoActiveGoal
	}
	pending := s.mailbox.TakeSignals(MaxEmbedSignalsPerRound)
	embed := TLSessionEmbed{
		Goal:        goalFrameOf(active),
		SessionTail: append([]TurnBrief(nil), s.tail...),
		Pending:     pending,
		Trigger:     trigger,
		TLMemory:    append([]string(nil), active.Directives...),
	}
	if err := embed.Validate(); err != nil {
		return TLDirective{}, fmt.Errorf("%w: 嵌入构建: %v", ErrInvalidArgument, err)
	}
	directive, err := s.evaluator.Evaluate(ctx, embed)
	if err != nil {
		return TLDirective{}, fmt.Errorf("TL 回合失败: %w", err)
	}
	if directive.At <= 0 {
		directive.At = s.now()
	}
	if directive.GoalID == "" {
		directive.GoalID = active.ID
	}
	if err := directive.Validate(); err != nil {
		return TLDirective{}, fmt.Errorf("%w: %v", ErrBadDirective, err)
	}
	if directive.GoalID != active.ID {
		return TLDirective{}, fmt.Errorf("%w: goal 漂移（directive %s ≠ active %s）", ErrBadDirective, directive.GoalID, active.ID)
	}
	// 指令写入 active goal 指令环（Goal 帧/TLMemory 素材）+ 发布待领取队列。
	if _, err := s.ctl.AppendDirective(ctx, directive.Summary()); err != nil {
		return TLDirective{}, err
	}
	s.mailbox.PublishDirective(directive)
	s.evalCount++
	s.lastEvalAt = directive.At
	s.lastEvalGoalID = active.ID
	s.turnsSinceEval = 0
	return directive, nil
}

// Snapshot 返回 TL 状态快照（headless goal_tl_snapshot / 读面）。
func (s *Supervisor) Snapshot() TLState {
	s.mu.Lock()
	defer s.mu.Unlock()
	overflowSignals, overflowDirectives := s.mailbox.Overflow()
	state := TLState{
		Enabled:            s.Enabled(),
		EvalCount:          s.evalCount,
		LastEvalAt:         s.lastEvalAt,
		LastEvalGoalID:     s.lastEvalGoalID,
		PendingSignals:     s.mailbox.PendingSignals(),
		PendingDirectives:  s.mailbox.PendingDirectives(),
		OverflowSignals:    overflowSignals,
		OverflowDirectives: overflowDirectives,
		TurnsSinceEval:     s.turnsSinceEval,
	}
	if active, ok := s.ctl.ActiveGoal(); ok {
		state.ActiveGoalID = active.ID
		state.ActiveGoalTitle = active.Title
	}
	return state
}

// TLState 是 TL 监督器读面快照。
type TLState struct {
	Enabled            bool   `json:"enabled"`
	ActiveGoalID       string `json:"active_goal_id,omitempty"`
	ActiveGoalTitle    string `json:"active_goal_title,omitempty"`
	EvalCount          int64  `json:"eval_count"`
	LastEvalAt         int64  `json:"last_eval_at,omitempty"`
	LastEvalGoalID     string `json:"last_eval_goal_id,omitempty"`
	PendingSignals     int    `json:"pending_signals"`
	PendingDirectives  int    `json:"pending_directives"`
	OverflowSignals    int64  `json:"overflow_signals"`
	OverflowDirectives int64  `json:"overflow_directives"`
	TurnsSinceEval     int    `json:"turns_since_eval"`
}
