// Package table owns WorkTable change coalescing and event payload assembly.
package table

import (
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	worktable "github.com/RedHuang-0622/seelex/kernel/worktable"
)

// Deps supplies the host tree snapshot and the event publication sink.
type Deps struct {
	SubAgentTree func() []dto.SubAgentTreeNode
	Publish      func(worktable.WorkTableUpdate, model.WorkTableEvent)
}

// Application owns the coalescing publisher and the last subagent-tree payload
// signature. Only the publisher goroutine reads or writes that signature.
type Application struct {
	deps        Deps
	publisher   *worktable.WorkTablePublisher
	lastTreeSig string
}

// New constructs the WorkTable publishing application.
func New(deps Deps) *Application {
	application := &Application{deps: deps}
	application.publisher = worktable.NewWorkTablePublisher(application.publish)
	return application
}

// Send submits one table update to the latest-wins coalescer.
func (application *Application) Send(update worktable.WorkTableUpdate) {
	if application == nil || application.publisher == nil {
		return
	}
	application.publisher.Send(update)
}

// Close publishes the latest tail and stops the publisher goroutine.
func (application *Application) Close() {
	if application == nil || application.publisher == nil {
		return
	}
	application.publisher.Close()
}

func (application *Application) publish(update worktable.WorkTableUpdate) {
	payload := model.WorkTableEvent{Items: update.Items, Batches: update.Batches}
	var tree []dto.SubAgentTreeNode
	if application.deps.SubAgentTree != nil {
		tree = application.deps.SubAgentTree()
	}
	signature := worktable.SubagentTreePayloadSignature(tree)
	if signature != application.lastTreeSig {
		application.lastTreeSig = signature
		if tree == nil {
			tree = []dto.SubAgentTreeNode{}
		}
		payload.SubAgentTree = tree
	}
	if application.deps.Publish != nil {
		application.deps.Publish(update, payload)
	}
}
