// Package approval manages asynchronous user approval requests.
package approval

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
	ID string
	// SessionID 是审批的会话级归属（波 4：awaiting_approval/待批列表按
	// sid 分格；空 = 进程级/无会话路径——权限门与旧测试夹具的 legacy 面）。
	SessionID         string
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
	sessionID   string
	result      chan ApprovalDecision
}

// PendingApproval 是 broker 待批集合的会话归属视图（快照/目录待批列表用）。
type PendingApproval struct {
	SessionID   string
	Interaction model.Interaction
}

type ApprovalBroker struct {
	mu                    sync.Mutex
	pending               map[string]*approvalPending
	events                event.Hub
	observer              func(sessionID, requestID string, interaction *model.Interaction)
	autoApprovePermission bool
}

// NewApprovalBroker 构造异步审批 broker。events 可为 nil（无事件发布）。
// 参数是 event.Hub 窄接口，*event.EventHub 以及测试桩均可满足。
func NewApprovalBroker(events event.Hub) *ApprovalBroker {
	return &ApprovalBroker{pending: make(map[string]*approvalPending), events: events}
}

// SetObserver receives a copy of each newly opened/closed interaction together
// with its session attribution: open → (sessionID, requestID, interaction);
// close → (sessionID, requestID, nil). requestID 在结案时仍可区分同一会话
// 的多笔待批；sessionID 为空表示进程级/无会话审批（视图单格回退面）。
func (broker *ApprovalBroker) SetObserver(observer func(sessionID, requestID string, interaction *model.Interaction)) {
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
	interaction := Interaction{ID: request.ID, SessionID: request.SessionID, Kind: "approval", Title: "操作审批", Question: request.Question, Risk: request.Risk, ToolName: request.ToolName, Preview: request.Preview, Options: append([]InteractionOption(nil), request.Options...), OpenedAt: time.Now(), Timeout: request.Timeout}
	pending := &approvalPending{interaction: interaction, sessionID: request.SessionID, result: make(chan ApprovalDecision, 1)}
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
		observer(request.SessionID, request.ID, &interaction)
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
	sessionID := ""
	if ok {
		sessionID = pending.sessionID
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
		observer(sessionID, id, nil)
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
		if observer != nil {
			observer(request.sessionID, id, nil)
		} else if broker.events != nil {
			broker.events.Publish(EventInteractionClosed, 0, id, decision)
		}
	}
	return len(pending)
}

// Pending 返回全部待批审批的会话归属快照（按审批 ID 排序，确定性）。
func (broker *ApprovalBroker) Pending() []PendingApproval {
	if broker == nil {
		return nil
	}
	broker.mu.Lock()
	defer broker.mu.Unlock()
	ids := make([]string, 0, len(broker.pending))
	for id := range broker.pending {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]PendingApproval, 0, len(ids))
	for _, id := range ids {
		entry := broker.pending[id]
		out = append(out, PendingApproval{
			SessionID:   entry.sessionID,
			Interaction: cloneInteraction(entry.interaction),
		})
	}
	return out
}

// PendingBySession 返回指定会话的待批审批（会话激活/镜像时补取用）。
func (broker *ApprovalBroker) PendingBySession(sessionID string) []model.Interaction {
	if broker == nil {
		return nil
	}
	all := broker.Pending()
	out := make([]model.Interaction, 0, len(all))
	for _, entry := range all {
		if entry.SessionID == sessionID {
			out = append(out, entry.Interaction)
		}
	}
	return out
}

func cloneInteraction(interaction model.Interaction) model.Interaction {
	interaction.Options = append([]model.InteractionOption(nil), interaction.Options...)
	return interaction
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
		if observer != nil {
			observer(request.sessionID, request.interaction.ID, nil)
		}
	}
}

func (broker *ApprovalBroker) remove(id string) {
	broker.mu.Lock()
	pending, ok := broker.pending[id]
	sessionID := ""
	if ok {
		sessionID = pending.sessionID
		delete(broker.pending, id)
	}
	observer := broker.observer
	broker.mu.Unlock()
	if ok && observer != nil {
		observer(sessionID, id, nil)
	}
}
