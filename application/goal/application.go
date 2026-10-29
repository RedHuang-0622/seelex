// Package goal owns Goal application routing. Goal state machines remain in
// kernel/goal behind a narrow coordinator port.
package goal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/contract/dto"
	goaldomain "github.com/RedHuang-0622/seelex/kernel/goal"
)

// ErrCoordinatorUnavailable reports that the session goal coordinator is not
// assembled.
var ErrCoordinatorUnavailable = errors.New("goal: 会话 goal 协调器未装配")

// CoordinatorPort is the application-facing surface of the session goal
// coordinator.
type CoordinatorPort interface {
	GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView
	Begin(context.Context, string, goaldomain.BeginRequest) (*goaldomain.GoalRecord, error)
	Update(context.Context, string, goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error)
	FinishDirect(context.Context, string, goaldomain.FinishRequest, bool) (*goaldomain.GoalRecord, error)
	StatusFor(sessionID string) goaldomain.StatusView
}

// Deps supplies the current coordinator and runtime projection refresh.
type Deps struct {
	Coordinator    func() CoordinatorPort
	Refresh        func(sessionID string)
	WriteSessionID func(context.Context) string
	ReadSessionID  func(context.Context) string
	// Continuation 是自动续跑的两层状态读面（不属于 goal 域状态机：层二由用户意图写、
	// 由会话活状态持有）。缺省 = 未装配续跑能力，治理视图不带 Continuation 字段。
	Continuation func(sessionID string) *dto.GoalContinuationView
}

// Application owns goal use-case routing without owning the goal state machine.
type Application struct {
	deps Deps
}

// New constructs the Goal application.
func New(deps Deps) *Application {
	return &Application{deps: deps}
}

func (application *Application) coordinatorFor(sessionID string) (CoordinatorPort, error) {
	if application == nil || application.deps.Coordinator == nil {
		return nil, ErrCoordinatorUnavailable
	}
	coordinator := application.deps.Coordinator()
	if coordinator == nil {
		return nil, ErrCoordinatorUnavailable
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("goal: session ID is required")
	}
	return coordinator, nil
}

// GovernanceViewFor returns the session's goal governance projection.
func (application *Application) GovernanceViewFor(sessionID string) *dto.GoalGovernanceView {
	if application == nil || application.deps.Coordinator == nil {
		return nil
	}
	coordinator := application.deps.Coordinator()
	if coordinator == nil {
		return nil
	}
	view := coordinator.GoalGovernanceViewFor(sessionID)
	if view == nil || application.deps.Continuation == nil {
		return view
	}
	// 续跑两层状态随治理视图一起发布：读的人不必再去问第二个入口（§2.8.1 横条只读一份）。
	copied := *view
	copied.Continuation = application.deps.Continuation(sessionID)
	return &copied
}

// BeginFor begins a session goal.
func (application *Application) BeginFor(ctx context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error) {
	coordinator, err := application.coordinatorFor(sessionID)
	if err != nil {
		return nil, err
	}
	record, err := coordinator.Begin(ctx, sessionID, request)
	if err != nil {
		return nil, err
	}
	application.deps.Refresh(sessionID)
	return record, nil
}

// UpdateFor updates a session goal.
func (application *Application) UpdateFor(ctx context.Context, sessionID string, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error) {
	coordinator, err := application.coordinatorFor(sessionID)
	if err != nil {
		return nil, err
	}
	record, err := coordinator.Update(ctx, sessionID, request)
	if err == nil {
		application.deps.Refresh(sessionID)
	}
	return record, err
}

// DoneFor finishes or aborts a session goal.
func (application *Application) DoneFor(ctx context.Context, sessionID string, request goaldomain.FinishRequest, abort bool) (*goaldomain.GoalRecord, error) {
	coordinator, err := application.coordinatorFor(sessionID)
	if err != nil {
		return nil, err
	}
	record, err := coordinator.FinishDirect(ctx, sessionID, request, abort)
	if err == nil {
		application.deps.Refresh(sessionID)
	}
	return record, err
}

// StatusFor returns the session goal status.
func (application *Application) StatusFor(sessionID string) (goaldomain.StatusView, error) {
	coordinator, err := application.coordinatorFor(sessionID)
	if err != nil {
		return goaldomain.StatusView{}, err
	}
	return coordinator.StatusFor(sessionID), nil
}

// AuditFor appends a non-terminal continuation event to the session goal ledger.
func (application *Application) AuditFor(ctx context.Context, sessionID string, entry goaldomain.AuditEntry) error {
	coordinator, err := application.coordinatorFor(sessionID)
	if err != nil {
		return err
	}
	auditor, ok := coordinator.(interface {
		AuditFor(context.Context, string, goaldomain.AuditEntry) error
	})
	if !ok {
		return nil
	}
	return auditor.AuditFor(ctx, sessionID, entry)
}

// BeginHandler adapts the goal_begin model tool.
func (application *Application) BeginHandler(ctx context.Context, argsJSON string) (string, error) {
	var request goaldomain.BeginRequest
	if err := json.Unmarshal([]byte(argsJSON), &request); err != nil {
		return "", fmt.Errorf("goal_begin: invalid arguments: %w", err)
	}
	record, err := application.BeginFor(ctx, application.writeSessionID(ctx), request)
	if err != nil {
		return "", err
	}
	return marshalResult(record)
}

// UpdateHandler adapts the goal_update model tool.
func (application *Application) UpdateHandler(ctx context.Context, argsJSON string) (string, error) {
	var request goaldomain.UpdateRequest
	if err := json.Unmarshal([]byte(argsJSON), &request); err != nil {
		return "", fmt.Errorf("goal_update: invalid arguments: %w", err)
	}
	record, err := application.UpdateFor(ctx, application.writeSessionID(ctx), request)
	if err != nil {
		return "", err
	}
	return marshalResult(record)
}

// DoneHandler adapts the goal_done model tool.
func (application *Application) DoneHandler(ctx context.Context, argsJSON string) (string, error) {
	var raw struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
		Result string `json:"result"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return "", fmt.Errorf("goal_done: invalid arguments: %w", err)
	}
	abort := false
	switch action := strings.ToLower(strings.TrimSpace(raw.Action)); action {
	case "", "finish":
	case "abort":
		abort = true
	default:
		return "", fmt.Errorf("goal_done: action 只能是 finish 或 abort（得到 %q）", raw.Action)
	}
	record, err := application.DoneFor(ctx, application.writeSessionID(ctx), goaldomain.FinishRequest{
		Reason: raw.Reason,
		Result: raw.Result,
	}, abort)
	if err != nil {
		return "", fmt.Errorf("goal_done: %w", err)
	}
	return marshalResult(record)
}

// StatusHandler adapts the goal_status model tool.
func (application *Application) StatusHandler(ctx context.Context, _ string) (string, error) {
	status, err := application.StatusFor(application.readSessionID(ctx))
	if err != nil {
		return "", err
	}
	return marshalResult(status)
}

func (application *Application) writeSessionID(ctx context.Context) string {
	if application == nil || application.deps.WriteSessionID == nil {
		return ""
	}
	return strings.TrimSpace(application.deps.WriteSessionID(ctx))
}

func (application *Application) readSessionID(ctx context.Context) string {
	if application == nil || application.deps.ReadSessionID == nil {
		return ""
	}
	return strings.TrimSpace(application.deps.ReadSessionID(ctx))
}

func marshalResult(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("goal: encode result: %w", err)
	}
	return string(raw), nil
}
