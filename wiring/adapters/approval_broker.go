package adapters

import (
	"github.com/RedHuang-0622/seelex/backend/limit/approval"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/middle/protocol/event"
)

// NewApprovalBrokerFactory returns the composition-root factory for the
// Backend-owned approval broker.
func NewApprovalBrokerFactory() func(event.Hub) contract.ApprovalBroker {
	return func(events event.Hub) contract.ApprovalBroker {
		return approval.NewApprovalBroker(events)
	}
}
