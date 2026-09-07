package goal

// a2a.go 承载 goal 条件下的 DS-A2A 双会话契约（协议 v0.1 的可运行切片，取代旧"同会话
// 双角色共享上下文"方案 A 语义）：
//   1. TLEvalSignal    EXEC(a) 事件登记 → 治理编排（turn 跳帧/checkpoint/compaction/审批/终态）；
//   2. TLSessionEmbed  ADVISOR(b) 回合输入 = b 自身上下文（锚点 + 帧账本 + 自身回合记忆），
//                      前缀稳定只尾部追加 ⇒ 缓存命中可观测（协议 §2 C1-C5）；
//   3. TLDirective     b → a 结构化建议帧（corr 信封幂等；经 DirectiveBus 受信注入，不回写 goal）。
// 治理编排与 b 生命周期见 advisor.go / techleader.go；终态 gate 与缺席默认见 gate.go。
// 有界护栏：指令队列 cap MaxDirectiveQueue、内容 ≤ MaxDirectiveRunes、信号 Detail 有界、
// b 回合记忆 ≤ MaxEmbedRounds、锚点必带（TL 不忘目标 = b 上下文含锚点快照）。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ---- 有界性常量（DS-A2A 护栏） ----

const (
	// MaxDirectiveQueue 是 b→a 指令信封队列上限。
	MaxDirectiveQueue = 32
	// MaxDirectiveRunes 是单条 TLDirective.Content 上限（注入边界护栏）。
	MaxDirectiveRunes = 1200
	// MaxSignalDetailRunes 是单条信号 Detail 上限。
	MaxSignalDetailRunes = 400
	// MaxGoalFrameProgress 是锚点 goal 帧携带的 progress 条数上限。
	MaxGoalFrameProgress = 3
	// DefaultEvalWindow 是非关键信号自动评估的最小轮次间隔（≤1 次/3-5 轮）。
	DefaultEvalWindow = 3
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

// TLDirective 是 b(ADVISOR) → a(EXEC) 的结构化指令（协议 §5 建议帧/裁决；corr 幂等信封）。
type TLDirective struct {
	GoalID   string        `json:"goal_id,omitempty"`
	Kind     DirectiveKind `json:"kind"`
	Content  string        `json:"content"`            // 有界指令文本（受信注入 a，不回写 goal 共享状态）
	Refs     []string      `json:"refs,omitempty"`     // 事件/文件引用（≤16）
	Severity Severity      `json:"severity,omitempty"` // P0/P1/P2（escalate 携带）
	Corr     string        `json:"corr,omitempty"`     // 关联 id：b 回合 Round.Corr（去重/审计）
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

// TLSessionEmbed 是一次 ADVISOR(b) 回合的有界输入（DS-A2A 协议 §5.3 语义）：
// 内容全部来自 b 自身上下文 = PeerID + 锚点 goal.start 快照 + 追加帧账本（ref_seq 单调）
// + b 自身回合记忆；不再包含 a 的实时会话尾窗 / 待处理共享信号（同会话共享治理已移除，
// 见 docs/2026-09-07-goal-domain-techleader/techleader-mvp.md §旧制 vs ds-a2a-protocol.md）。
type TLSessionEmbed struct {
	PeerID   string    `json:"peer_id"`             // b 会话 id（协议 peer.bind）
	Goal     GoalFrame `json:"goal"`                // 锚点快照（bind 时一次快照，不随 a 后续变化）
	Frames   []Frame   `json:"frames,omitempty"`    // 追加帧（ref_seq 单调；跳帧自由）
	TLMemory []string  `json:"tl_memory,omitempty"` // b 自身回合记忆（TL 记得自己说过什么，≤ MaxEmbedRounds）
	Trigger  string    `json:"trigger,omitempty"`   // 本回合触发原因
	Corr     string    `json:"corr,omitempty"`      // 本回合产物关联 id（协议 §5 幂等）
}

// Validate 校验回合嵌入（有界性快检；供测试与装配护栏使用）。
func (e TLSessionEmbed) Validate() error {
	if e.PeerID == "" {
		return fmt.Errorf("%w: b 回合必须携带 peer 会话 id", ErrInvalidArgument)
	}
	if e.Goal.ID == "" {
		return fmt.Errorf("%w: b 回合必须携带锚点 goal 帧（防遗忘）", ErrInvalidArgument)
	}
	for index := 1; index < len(e.Frames); index++ {
		if e.Frames[index].RefSeq <= e.Frames[index-1].RefSeq {
			return fmt.Errorf("%w: 帧 ref_seq 须严格递增", ErrInvalidArgument)
		}
	}
	if len(e.TLMemory) > MaxEmbedRounds {
		return fmt.Errorf("%w: tl memory 超限（> %d）", ErrInvalidArgument, MaxEmbedRounds)
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
