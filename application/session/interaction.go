package session

import (
	"context"

	switching "github.com/RedHuang-0622/seelex/application/session/switchpolicy"
	"github.com/RedHuang-0622/seelex/contract"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

// InteractionDeps supplies the current view interaction, optional background
// approval lookup and the operations selected by an interaction kind.
type InteractionDeps struct {
	Current                func() *protocol.Interaction
	PendingApprovalSession func(string) (string, bool)
	ResolveApproval        func(id, optionID string) error
	CloseInteraction       func(id string)
	ResumeSession          func(sessionID string) error
	AddNotice              func(string)
	SelectAccount          func(context.Context, string) error
}

// InteractionApplication owns only the interaction decision tree. Session,
// account and approval state remain in their current owners.
type InteractionApplication struct {
	deps InteractionDeps
}

func NewInteraction(deps InteractionDeps) *InteractionApplication {
	return &InteractionApplication{deps: deps}
}

func (application *InteractionApplication) Resolve(ctx context.Context, id, optionID string) error {
	if application == nil {
		return contract.ErrInteractionNotFound
	}
	interaction := application.deps.Current()
	if interaction == nil || interaction.ID != id {
		if application.deps.PendingApprovalSession != nil && application.deps.ResolveApproval != nil {
			if _, ok := application.deps.PendingApprovalSession(id); ok {
				return application.deps.ResolveApproval(id, optionID)
			}
		}
		return contract.ErrInteractionNotFound
	}
	if optionID == switching.InteractionCancelOptionID {
		if interaction.Kind == switching.InteractionKindApproval {
			return application.deps.ResolveApproval(id, optionID)
		}
		application.deps.CloseInteraction(id)
		return nil
	}
	switch interaction.Kind {
	case switching.InteractionKindApproval:
		return application.deps.ResolveApproval(id, optionID)
	case switching.InteractionKindSession:
		if err := application.deps.ResumeSession(optionID); err != nil {
			application.deps.AddNotice("恢复失败: " + err.Error())
			application.deps.CloseInteraction(id)
			return err
		}
	case switching.InteractionKindAccount:
		if err := application.deps.SelectAccount(ctx, optionID); err != nil {
			application.deps.AddNotice("账号切换失败: " + err.Error())
			application.deps.CloseInteraction(id)
			return err
		}
	default:
		return switching.UnsupportedInteractionKind(interaction.Kind)
	}
	application.deps.CloseInteraction(id)
	return nil
}
