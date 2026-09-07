package goal

// a2a.go 承载 goal 条件下的双角色 A2A 契约（Part II MVP，design.md §4-§5 的可运行切片）：
// mainagent（执行）与 TechLeader（只读评估）共享同一会话/goal 栈，经三类结构化消息通信——
//   1. TLEvalSignal    执行事件 → TL（turn/checkpoint/compaction/预算/审批/终态提议）；
//   2. TLSessionEmbed  每次 TL 回合的有界输入（goal 帧强制嵌入 + 会话尾窗 + 待处理信号）；
//   3. TLDirective     TL → mainagent（纠偏/规范提示/校验裁决/升级人工/审批代答）。
// 语义护栏（design §6）：TL 无写工具（只产指令）、队列有界且溢出仅计数（状态可重读追平）、
// 每次评估强制嵌入 goal 帧（TL 不忘目标）、指令注入长度有界。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ---- 有界性常量（MVP 护栏；见 seelexctx.Limits 对齐目标） ----

const (
	// MaxSignalQueue 是待处理信号队列上限（对齐子代理 actor 通道 cap 256 的语义）。
	MaxSignalQueue = 256
	// MaxEmbedTail 是有界会话嵌入保留的最近轮次条数。
	MaxEmbedTail = 8
	// MaxDirectiveQueue 是待 mainagent 排空（drain）的指令队列上限。
	MaxDirectiveQueue = 32
	// MaxDirectiveRunes 是单条 TLDirective.Content 上限（注入边界护栏）。
	MaxDirectiveRunes = 1200
	// MaxSignalDetailRunes 是单条信号 Detail 上限。
	MaxSignalDetailRunes = 400
	// MaxEmbedSignalsPerRound 是单回合嵌入携带的信号条数上限（截断计数）。
	MaxEmbedSignalsPerRound = 16
	// MaxGoalFrameProgress 是嵌入 goal 帧携带的最近 progress 条数。
	MaxGoalFrameProgress = 3
	// DefaultEvalWindow 是非关键信号自动评估的最小轮次间隔（design D5：≤1 次/3-5 轮）。
	DefaultEvalWindow = 3
	// DefaultSupervisorQueueCap 是 Supervisor 默认命令/队列容量上限。
	DefaultSupervisorQueueCap = 64
)

// ---- 信号（exec → TL） ----

// SignalKind 是 mainagent 执行事件信号类型（design §5.2）。
type SignalKind string

const (
	SignalTurnCompleted    SignalKind = "turn_completed"    // 每轮 mainagent 提交后（不触发评估）
	SignalStepCheckpoint   SignalKind = "step_checkpoint"   // plan 节点/task_check_node 打点（可触发）
	SignalContextCompacted SignalKind = "context_compacted" // 压缩后（强制评估，防遗忘）
	SignalBudgetWarning    SignalKind = "budget_warning"    // goal 预算水位（如 70%）（强制）
	SignalApprovalAsked    SignalKind = "approval_asked"    // ask_approve 待批（强制，预筛）
	SignalTerminalProposal SignalKind = "terminal_proposal" // 终态提议（强制，终态 gate）
	SignalGoalUpdated      SignalKind = "goal_updated"      // goal_update（不触发，刷新帧即可）
)

var validSignalKinds = map[SignalKind]bool{
	SignalTurnCompleted: true, SignalStepCheckpoint: true,
	SignalContextCompacted: true, SignalBudgetWarning: true,
	SignalApprovalAsked: true, SignalTerminalProposal: true,
	SignalGoalUpdated: true,
}

// IsCriticalSignal 报告该信号是否强制评估（不受 eval_window 间隔约束）。
func IsCriticalSignal(kind SignalKind) bool {
	switch kind {
	case SignalContextCompacted, SignalBudgetWarning, SignalApprovalAsked, SignalTerminalProposal:
		return true
	}
	return false
}

// TLEvalSignal 是执行侧投递给 TL 的事件信号。
type TLEvalSignal struct {
	Kind   SignalKind `json:"kind"`
	At     int64      `json:"at,omitempty"`
	Source string     `json:"source,omitempty"` // 来源（如 plan 节点 id / task id / tool 名）
	Detail string     `json:"detail,omitempty"` // 有界一句话
	Ref    string     `json:"ref,omitempty"`    // 可选引用（文件/事件）
}

// Validate 校验信号字段。
func (s TLEvalSignal) Validate() error {
	if !validSignalKinds[s.Kind] {
		return fmt.Errorf("%w: 非法 signal kind %q", ErrInvalidArgument, s.Kind)
	}
	if len([]rune(s.Detail)) > MaxSignalDetailRunes {
		return fmt.Errorf("%w: signal detail 超长（> %d runes）", ErrInvalidArgument, MaxSignalDetailRunes)
	}
	return nil
}

// ---- 指令（TL → exec） ----

// DirectiveKind 是 TLDirective 类型（design §5.4 + 审批预筛 §5.5）。
type DirectiveKind string

const (
	DirectiveCorrect         DirectiveKind = "correct"          // 过程纠偏
	DirectiveNormativePrompt DirectiveKind = "normative_prompt" // 规范提示词（代答 human judge）
	DirectiveCheckpointOK    DirectiveKind = "checkpoint_ok"    // 打点校验通过
	DirectiveVerdictDone     DirectiveKind = "verdict_done"     // 终态校验：达成
	DirectiveVerdictNotDone  DirectiveKind = "verdict_not_done" // 终态校验：未达成（附纠偏）
	DirectiveEscalateHuman   DirectiveKind = "escalate_human"   // 越权/无法判定 → 转人工
	DirectiveApprove         DirectiveKind = "approve"          // 审批预筛代答：放行
	DirectiveDeny            DirectiveKind = "deny"             // 审批预筛代答：拒绝
)

var validDirectiveKinds = map[DirectiveKind]bool{
	DirectiveCorrect: true, DirectiveNormativePrompt: true,
	DirectiveCheckpointOK: true, DirectiveVerdictDone: true,
	DirectiveVerdictNotDone: true, DirectiveEscalateHuman: true,
	DirectiveApprove: true, DirectiveDeny: true,
}

// Severity 是 TL 指令严重度（escalate 时携带）。
type Severity string

const (
	SeverityP0 Severity = "P0"
	SeverityP1 Severity = "P1"
	SeverityP2 Severity = "P2"
)

var validSeverities = map[Severity]bool{SeverityP0: true, SeverityP1: true, SeverityP2: true}

// TLDirective 是 TL → mainagent 的结构化指令（design §5.4）。
type TLDirective struct {
	GoalID   string        `json:"goal_id,omitempty"`
	Kind     DirectiveKind `json:"kind"`
	Content  string        `json:"content"`            // 有界指令文本（注入 mainagent 下一轮）
	Refs     []string      `json:"refs,omitempty"`     // 事件/文件引用（≤16）
	Severity Severity      `json:"severity,omitempty"` // P0/P1/P2（escalate 携带）
	At       int64         `json:"at,omitempty"`
}

// Validate 校验指令字段（目标 id 可为空，由 Supervisor 补当前 active goal id）。
func (d TLDirective) Validate() error {
	if !validDirectiveKinds[d.Kind] {
		return fmt.Errorf("%w: 非法 directive kind %q", ErrInvalidArgument, d.Kind)
	}
	if len([]rune(d.Content)) > MaxDirectiveRunes {
		return fmt.Errorf("%w: directive content 超长（> %d runes）", ErrInvalidArgument, MaxDirectiveRunes)
	}
	if len(d.Refs) > 16 {
		return fmt.Errorf("%w: directive refs 超限（> 16）", ErrInvalidArgument)
	}
	if d.Severity != "" && !validSeverities[d.Severity] {
		return fmt.Errorf("%w: 非法 severity %q", ErrInvalidArgument, d.Severity)
	}
	return nil
}

// Summary 返回有界摘要（写入 goal 指令环形记录的形态，design §3.2 TLDirective 摘要）。
func (d TLDirective) Summary() string {
	text := strings.TrimSpace(d.Content)
	if text == "" {
		return string(d.Kind)
	}
	if severity := string(d.Severity); severity != "" {
		text = severity + " " + text
	}
	if kind := string(d.Kind); kind != "" {
		text = "[" + kind + "] " + text
	}
	if len([]rune(text)) > MaxDirectiveRunes {
		runes := []rune(text)
		text = string(runes[:MaxDirectiveRunes])
	}
	return text
}

// ---- 有界会话嵌入（每次 TL 回合的输入） ----

// GoalFrame 是嵌入 TL 输入的栈顶 goal 帧（design §5.3；每次评估强制包含）。
type GoalFrame struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Statement  string     `json:"statement"`
	Acceptance []string   `json:"acceptance,omitempty"`
	OutOfScope []string   `json:"out_of_scope,omitempty"`
	Status     Status     `json:"status"`
	Progress   []Progress `json:"progress,omitempty"` // ≤ MaxGoalFrameProgress
	Directives []string   `json:"tl_directives,omitempty"`
	Budget     Budget     `json:"budget"`
	UpdatedAt  int64      `json:"updated_at"`
}

func goalFrameOf(record *GoalRecord) GoalFrame {
	if record == nil {
		return GoalFrame{}
	}
	frame := GoalFrame{
		ID:         record.ID,
		Title:      record.Title,
		Statement:  record.Statement,
		Acceptance: append([]string(nil), record.Acceptance...),
		OutOfScope: append([]string(nil), record.OutOfScope...),
		Status:     record.Status,
		Directives: append([]string(nil), record.Directives...),
		Budget:     record.Budget,
		UpdatedAt:  record.UpdatedAt,
	}
	start := 0
	if len(record.Progress) > MaxGoalFrameProgress {
		start = len(record.Progress) - MaxGoalFrameProgress
	}
	for _, item := range record.Progress[start:] {
		frame.Progress = append(frame.Progress, item)
	}
	return frame
}

// TurnBrief 是有界会话嵌入中的单轮摘要（设计 §5.3 SessionTail）。
type TurnBrief struct {
	Role    string `json:"role,omitempty"` // "mainagent" | "tool" | "user" | "tl"
	Summary string `json:"summary"`
	Tool    string `json:"tool,omitempty"`
	At      int64  `json:"at,omitempty"`
}

// TLSessionEmbed 是投递给 TL 评估回合的有界输入（design §5.3）。
type TLSessionEmbed struct {
	Goal        GoalFrame      `json:"goal"`                      // 强制嵌入：栈顶 goal
	SessionTail []TurnBrief    `json:"session_tail,omitempty"`    // 最近 ≤ MaxEmbedTail 轮
	Pending     []TLEvalSignal `json:"pending_signals,omitempty"` // ≤ MaxEmbedSignalsPerRound
	Trigger     string         `json:"trigger,omitempty"`         // 本回合触发原因
	TLMemory    []string       `json:"tl_memory,omitempty"`       // 最近 TL 指令摘要（= goal.Directives）
}

// Validate 校验嵌入（有界性快检；供测试与装配护栏使用）。
func (e TLSessionEmbed) Validate() error {
	if len(e.SessionTail) > MaxEmbedTail {
		return fmt.Errorf("%w: session tail 超限（> %d）", ErrInvalidArgument, MaxEmbedTail)
	}
	if len(e.Pending) > MaxEmbedSignalsPerRound {
		return fmt.Errorf("%w: pending signals 超限（> %d）", ErrInvalidArgument, MaxEmbedSignalsPerRound)
	}
	if e.Goal.ID == "" {
		return fmt.Errorf("%w: TL 回合必须携带 goal 帧（防遗忘）", ErrInvalidArgument)
	}
	return nil
}

// TLEvaluator 是 TL 评估引擎接口。MVP：由装配方注入（真实 LLM provider 或测试 stub）。
// 约束（design D6）：评估器只允许返回 TLDirective，不接触任何写工具/会话状态。
type TLEvaluator interface {
	// Evaluate 对一次有界嵌入做一次 TL 回合，返回结构化指令。
	Evaluate(ctx context.Context, embed TLSessionEmbed) (TLDirective, error)
}

// ---- 域错误补充 ----

// TL 域哨兵错误。
var (
	ErrTLDisabled    = errors.New("goal: techleader 未启用（无评估器）")
	ErrNoActiveGoal  = errors.New("goal: 无 active goal 可评估")
	ErrBadDirective  = errors.New("goal: TL 输出非法指令")
	ErrInvalidSignal = errors.New("goal: 非法信号")
)

// TLNow 提供 TL 域时间源（测试可注入）。
func TLNow() func() int64 { return func() int64 { return time.Now().Unix() } }
