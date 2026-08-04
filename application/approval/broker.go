// Package approval manages asynchronous user approval requests.
package approval

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/model"
)

var (
	ErrInteractionNotFound = errors.New("interaction not found")
	ErrInteractionResolved = errors.New("interaction already resolved")
)

type (
	Interaction       = model.Interaction
	InteractionOption = model.InteractionOption
)

const (
	EventInteractionOpened = event.EventInteractionOpened
	EventInteractionClosed = event.EventInteractionClosed
)

type ApprovalRequest struct {
	ID                string
	Question          string
	Options           []model.InteractionOption
	Risk              string
	ToolName          string
	Preview           string
	Timeout           time.Duration
	PermissionRequest bool
}
type ApprovalDecision struct {
	OptionID string `json:"option_id"`
}
type approvalPending struct {
	interaction model.Interaction
	result      chan ApprovalDecision
}

type ApprovalBroker struct {
	mu                    sync.Mutex
	pending               map[string]*approvalPending
	events                *event.EventHub
	observer              func(*model.Interaction)
	autoApprovePermission bool
}

func NewApprovalBroker(events *event.EventHub) *ApprovalBroker {
	return &ApprovalBroker{pending: make(map[string]*approvalPending), events: events}
}

// SetObserver receives a copy of each newly opened interaction.
func (broker *ApprovalBroker) SetObserver(observer func(*model.Interaction)) {
	broker.mu.Lock()
	broker.observer = observer
	broker.mu.Unlock()
}

// SetPermissionAutoApproval makes future permission-gate requests resolve
// without opening an interaction. It deliberately excludes non-permission
// approvals such as Plan/manual gates, which must always retain user control.
func (broker *ApprovalBroker) SetPermissionAutoApproval(on bool) {
	broker.mu.Lock()
	broker.autoApprovePermission = on
	broker.mu.Unlock()
}

func (broker *ApprovalBroker) Request(ctx context.Context, request ApprovalRequest) (ApprovalDecision, error) {
	if request.ID == "" {
		request.ID = fmt.Sprintf("approval-%d", time.Now().UnixNano())
	}
	interaction := Interaction{ID: request.ID, Kind: "approval", Title: "操作审批", Question: request.Question, Risk: request.Risk, ToolName: request.ToolName, Preview: request.Preview, Options: append([]InteractionOption(nil), request.Options...), OpenedAt: time.Now(), Timeout: request.Timeout}
	pending := &approvalPending{interaction: interaction, result: make(chan ApprovalDecision, 1)}
	broker.mu.Lock()
	if request.PermissionRequest && broker.autoApprovePermission {
		broker.mu.Unlock()
		return ApprovalDecision{OptionID: "always"}, nil
	}
	if _, exists := broker.pending[request.ID]; exists {
		broker.mu.Unlock()
		return ApprovalDecision{}, fmt.Errorf("approval %q already pending", request.ID)
	}
	broker.pending[request.ID] = pending
	observer := broker.observer
	broker.mu.Unlock()
	if observer != nil {
		observer(&interaction)
	} else if broker.events != nil {
		broker.events.Publish(EventInteractionOpened, 0, request.ID, interaction)
	}
	waitContext := ctx
	var cancel context.CancelFunc
	if request.Timeout > 0 {
		waitContext, cancel = context.WithTimeout(ctx, request.Timeout)
		defer cancel()
	}
	select {
	case decision := <-pending.result:
		return decision, nil
	case <-waitContext.Done():
		broker.remove(request.ID)
		return ApprovalDecision{}, waitContext.Err()
	}
}

func (broker *ApprovalBroker) Resolve(id string, decision ApprovalDecision) error {
	broker.mu.Lock()
	pending, ok := broker.pending[id]
	if ok {
		delete(broker.pending, id)
	}
	observer := broker.observer
	broker.mu.Unlock()
	if !ok {
		return ErrInteractionNotFound
	}
	select {
	case pending.result <- decision:
	default:
		return ErrInteractionResolved
	}
	if observer != nil {
		observer(nil)
	} else if broker.events != nil {
		broker.events.Publish(EventInteractionClosed, 0, id, decision)
	}
	return nil
}

// ResolveAll completes every currently pending approval with the same
// explicit decision. It is used when the user enables full access while a
// tool is already waiting at the permission gate.
func (broker *ApprovalBroker) ResolveAll(decision ApprovalDecision) int {
	broker.mu.Lock()
	pending := broker.pending
	broker.pending = make(map[string]*approvalPending)
	observer := broker.observer
	broker.mu.Unlock()
	for id, request := range pending {
		select {
		case request.result <- decision:
		default:
			continue
		}
		if observer == nil && broker.events != nil {
			broker.events.Publish(EventInteractionClosed, 0, id, decision)
		}
	}
	if len(pending) > 0 && observer != nil {
		observer(nil)
	}
	return len(pending)
}

func (broker *ApprovalBroker) Shutdown() {
	broker.mu.Lock()
	pending := broker.pending
	broker.pending = make(map[string]*approvalPending)
	observer := broker.observer
	broker.mu.Unlock()
	for _, request := range pending {
		select {
		case request.result <- ApprovalDecision{OptionID: "__CANCEL__"}:
		default:
		}
	}
	if observer != nil {
		observer(nil)
	}
}

func (broker *ApprovalBroker) remove(id string) {
	broker.mu.Lock()
	delete(broker.pending, id)
	observer := broker.observer
	broker.mu.Unlock()
	if observer != nil {
		observer(nil)
	}
}
