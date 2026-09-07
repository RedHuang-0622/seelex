// Package goal 承载 Goal 域（Part I 原型）：会话粒度的 goal 对象、goal 栈
// （LIFO，默认深度 1 = 会话单例）、状态机、事件订阅与前端投影。
//
// 设计来源：docs/2026-09-07-goal-domain-techleader/{architecture,design,prototype}.md
// 本包自持全部可变状态（Controller.mu），不依赖 application/core 其它子包，
// 便于在既有运行链路外先行验证 goal 语义，再按 design.md §2 落位
// task_context/sessionstore/gui/seelebridge 的接线。
package goal

import (
	"errors"
	"fmt"
	"strings"
)

// 域内长度上限（有界性；与 seelexctx Limits 对齐的 goal 侧护栏）。
const (
	// DefaultStackDepth 是会话默认 goal 栈深：1 = 会话单例（D2）。
	DefaultStackDepth = 1
	// MaxStackDepth 是放开嵌套时的上限（防御性护栏）。
	MaxStackDepth = 16
	// MaxStatementRunes 限制目标正文长度。
	MaxStatementRunes = 4000
	// MaxAcceptanceItems 限制完成条件条数。
	MaxAcceptanceItems = 64
	// MaxProgressItems 限制栈顶 progress 保留条数（环形，丢最旧）。
	MaxProgressItems = 32
	// MaxProgressRunes 限制单条 progress 长度。
	MaxProgressRunes = 800
	// DefaultTLTokensShare 是 TechLeader 评估预算占 goal 总 token 的默认比例（%）。
	DefaultTLTokensShare = 20
	// MaxDirectives 是 GoalRecord.Directives 环形保留上限（Part II 预留）。
	MaxDirectives = 5
)

// Status 是 goal 生命周期状态（design §3.3 P0 子集 + 终态判定）。
type Status string

const (
	StatusActive       Status = "active"        // 栈顶当前目标
	StatusPaused       Status = "paused"        // 栈下层被挂起（嵌套时自动）
	StatusReviewing    Status = "reviewing"     // 终态校验中（P2 TL gate 用）
	StatusCompleted    Status = "completed"     // finish 收口
	StatusFailed       Status = "failed"        // 判不可达成（预留）
	StatusAborted      Status = "aborted"       // 显式放弃
	StatusWaitingHuman Status = "waiting_human" // 预算耗尽/越权，等人工
)

var validStatuses = map[Status]bool{
	StatusActive: true, StatusPaused: true, StatusReviewing: true,
	StatusCompleted: true, StatusFailed: true, StatusAborted: true,
	StatusWaitingHuman: true,
}

// ParseStatus 解析并校验状态字符串。
func ParseStatus(value string) (Status, error) {
	status := Status(value)
	if !validStatuses[status] {
		return "", fmt.Errorf("%w: 非法状态 %q", ErrInvalidStatus, value)
	}
	return status, nil
}

// IsTerminal 报告状态是否终态（不再停留在 goal 栈上）。
func IsTerminal(status Status) bool {
	switch status {
	case StatusCompleted, StatusFailed, StatusAborted:
		return true
	}
	return false
}

// 域错误（哨兵 + 可包装）。
var (
	ErrInvalidArgument  = errors.New("goal: 非法参数")
	ErrInvalidStatus    = errors.New("goal: 非法状态")
	ErrStackEmpty       = errors.New("goal: 无 active goal")
	ErrStackFull        = errors.New("goal: goal 栈已满（会话单例：先 finish 当前目标或显式 aborted）")
	ErrGoalNotActive    = errors.New("goal: 仅 active goal 可更新")
	ErrStoreUnavailable = errors.New("goal: 存储不可用")
)

// Budget 是 goal 级预算护栏（design §3.2）。0 = 无限/未设。
type Budget struct {
	MaxLoops      int `json:"max_loops,omitempty"`       // goal 总循环护栏
	MaxTokens     int `json:"max_tokens,omitempty"`      // goal 总 token 护栏
	TLTokensShare int `json:"tl_tokens_share,omitempty"` // TL 评估预算占比（默认 DefaultTLTokensShare）
}

// Normalized 返回带默认值的预算副本。
func (b Budget) Normalized() Budget {
	if b.TLTokensShare <= 0 {
		b.TLTokensShare = DefaultTLTokensShare
	}
	return b
}

// ProgressKind 是 goal 进度条目类型（milestone/finding/decision/risk）。
type ProgressKind string

const (
	ProgressMilestone ProgressKind = "milestone"
	ProgressFinding   ProgressKind = "finding"
	ProgressDecision  ProgressKind = "decision"
	ProgressRisk      ProgressKind = "risk"
)

var validProgressKinds = map[ProgressKind]bool{
	ProgressMilestone: true, ProgressFinding: true,
	ProgressDecision: true, ProgressRisk: true,
}

// Progress 是 goal_update 追加的一条进度/发现。
type Progress struct {
	At      int64        `json:"at"`
	Kind    ProgressKind `json:"kind"`
	Content string       `json:"content"`
}

// GoalRecord 是 goal 栈的持久化单元（design §3.2 GoalRecord）。
type GoalRecord struct {
	ID         string            `json:"id"`
	Title      string            `json:"title"`
	Statement  string            `json:"statement"`
	Acceptance []string          `json:"acceptance,omitempty"`
	OutOfScope []string          `json:"out_of_scope,omitempty"`
	Budget     Budget            `json:"budget"`
	Status     Status            `json:"status"`
	Progress   []Progress        `json:"progress,omitempty"`
	Directives []string          `json:"tl_directives,omitempty"` // Part II 预留（环形）
	CreatedAt  int64             `json:"created_at"`
	UpdatedAt  int64             `json:"updated_at"`
	FinishedAt int64             `json:"finished_at,omitempty"`
	Meta       map[string]string `json:"meta,omitempty"`
}

// Clone 深拷贝记录（返回副本，避免锁外读到栈内可变引用）。
func (r *GoalRecord) Clone() *GoalRecord {
	if r == nil {
		return nil
	}
	copyRecord := *r
	copyRecord.Acceptance = append([]string(nil), r.Acceptance...)
	copyRecord.OutOfScope = append([]string(nil), r.OutOfScope...)
	copyRecord.Progress = make([]Progress, len(r.Progress))
	for index := range r.Progress {
		copyRecord.Progress[index] = r.Progress[index]
	}
	copyRecord.Directives = append([]string(nil), r.Directives...)
	if r.Meta != nil {
		copyRecord.Meta = make(map[string]string, len(r.Meta))
		for key, value := range r.Meta {
			copyRecord.Meta[key] = value
		}
	}
	return &copyRecord
}

func (r *GoalRecord) validateBegin() error {
	if r == nil {
		return fmt.Errorf("%w: nil record", ErrInvalidArgument)
	}
	if strings.TrimSpace(r.Title) == "" {
		return fmt.Errorf("%w: title 必填", ErrInvalidArgument)
	}
	if len([]rune(r.Statement)) > MaxStatementRunes {
		return fmt.Errorf("%w: statement 超长（> %d runes）", ErrInvalidArgument, MaxStatementRunes)
	}
	if len(r.Acceptance) > MaxAcceptanceItems {
		return fmt.Errorf("%w: acceptance 条数超限（> %d）", ErrInvalidArgument, MaxAcceptanceItems)
	}
	for _, item := range r.Acceptance {
		if strings.TrimSpace(item) == "" {
			return fmt.Errorf("%w: acceptance 条目不可为空", ErrInvalidArgument)
		}
	}
	if r.Budget.MaxLoops < 0 || r.Budget.MaxTokens < 0 || r.Budget.TLTokensShare < 0 {
		return fmt.Errorf("%w: budget 字段不可为负", ErrInvalidArgument)
	}
	return nil
}

// BeginRequest 是 goal_begin 的入参。
type BeginRequest struct {
	Title      string   `json:"title"`
	Statement  string   `json:"statement"`
	Acceptance []string `json:"acceptance,omitempty"`
	OutOfScope []string `json:"out_of_scope,omitempty"`
	Budget     *Budget  `json:"budget,omitempty"`
}

// newGoalRecord 由 BeginRequest 构造记录并应用默认值。
func newGoalRecord(id string, request BeginRequest, now int64) *GoalRecord {
	record := &GoalRecord{
		ID:         id,
		Title:      strings.TrimSpace(request.Title),
		Statement:  strings.TrimSpace(request.Statement),
		Acceptance: append([]string(nil), request.Acceptance...),
		OutOfScope: append([]string(nil), request.OutOfScope...),
		Status:     StatusActive,
		CreatedAt:  now,
		UpdatedAt:  now,
		Budget:     Budget{}.Normalized(),
	}
	if request.Budget != nil {
		record.Budget = request.Budget.Normalized()
	}
	return record
}

// UpdateRequest 是 goal_update 的入参（仅作用于栈顶 active goal）。
// 指针字段 = 有值才改；切片字段 = 非 nil 才替换。
type UpdateRequest struct {
	Title           *string      `json:"title,omitempty"`
	Statement       *string      `json:"statement,omitempty"`
	Acceptance      []string     `json:"acceptance,omitempty"`
	OutOfScope      []string     `json:"out_of_scope,omitempty"`
	ProgressKind    ProgressKind `json:"progress_kind,omitempty"`
	ProgressContent string       `json:"progress_content,omitempty"`
}

// FinishRequest 是 goal_finish / goal_abort 的入参（reason/result 审计）。
type FinishRequest struct {
	Reason string `json:"reason,omitempty"`
	Result string `json:"result,omitempty"`
}
