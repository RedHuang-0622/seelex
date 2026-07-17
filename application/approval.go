package application

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrInteractionNotFound = errors.New("interaction not found")
	ErrInteractionResolved = errors.New("interaction already resolved")
)

type ApprovalRequest struct {
	ID       string
	Question string
	Options  []InteractionOption
	Risk     string
	ToolName string
	Preview  string
	Timeout  time.Duration
}
type ApprovalDecision struct {
	OptionID string `json:"option_id"`
}
type approvalPending struct {
	interaction Interaction
	result      chan ApprovalDecision
}

type ApprovalBroker struct {
	mu       sync.Mutex
	pending  map[string]*approvalPending
	events   *EventHub
	observer func(*Interaction)
}

func NewApprovalBroker(events *EventHub) *ApprovalBroker {
	return &ApprovalBroker{pending: make(map[string]*approvalPending), events: events}
}

func (broker *ApprovalBroker) setObserver(observer func(*Interaction)) {
	broker.mu.Lock()
	broker.observer = observer
	broker.mu.Unlock()
}

func (broker *ApprovalBroker) Request(ctx context.Context, request ApprovalRequest) (ApprovalDecision, error) {
	if request.ID == "" {
		request.ID = fmt.Sprintf("approval-%d", time.Now().UnixNano())
	}
	interaction := Interaction{ID: request.ID, Kind: "approval", Title: "操作审批", Question: request.Question, Risk: request.Risk, ToolName: request.ToolName, Preview: request.Preview, Options: append([]InteractionOption(nil), request.Options...), OpenedAt: time.Now(), Timeout: request.Timeout}
	pending := &approvalPending{interaction: interaction, result: make(chan ApprovalDecision, 1)}
	broker.mu.Lock()
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
