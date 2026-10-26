package table

import (
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/middle/view/worktable"
)

func TestApplicationCoalescesAndOnlyResendsChangedTree(t *testing.T) {
	tree := []dto.SubAgentTreeNode{{ID: "node-1", Goal: "first"}}
	published := make(chan model.WorkTableEvent, 4)
	application := New(Deps{
		SubAgentTree: func() []dto.SubAgentTreeNode {
			return append([]dto.SubAgentTreeNode(nil), tree...)
		},
		Publish: func(_ worktable.WorkTableUpdate, payload model.WorkTableEvent) {
			published <- payload
		},
	})
	defer application.Close()

	application.Send(worktable.WorkTableUpdate{Revision: 1, Items: []model.WorkItem{{ID: "todo:1"}}})
	first := receivePayload(t, published)
	if len(first.SubAgentTree) != 1 || first.SubAgentTree[0].ID != "node-1" {
		t.Fatalf("first payload tree = %+v", first.SubAgentTree)
	}

	application.Send(worktable.WorkTableUpdate{Revision: 2, Items: []model.WorkItem{{ID: "todo:1", Status: "doing"}}})
	second := receivePayload(t, published)
	if second.SubAgentTree != nil {
		t.Fatalf("unchanged tree must be omitted: %+v", second.SubAgentTree)
	}

	tree = []dto.SubAgentTreeNode{{ID: "node-1", Goal: "changed"}}
	application.Send(worktable.WorkTableUpdate{Revision: 3, Items: []model.WorkItem{{ID: "todo:1", Status: "done"}}})
	third := receivePayload(t, published)
	if len(third.SubAgentTree) != 1 || third.SubAgentTree[0].Goal != "changed" {
		t.Fatalf("changed tree must be resent: %+v", third.SubAgentTree)
	}
}

func TestApplicationCloseDrainsTailAndSendAfterCloseIsSafe(t *testing.T) {
	published := make(chan model.WorkTableEvent, 2)
	application := New(Deps{
		Publish: func(_ worktable.WorkTableUpdate, payload model.WorkTableEvent) {
			published <- payload
		},
	})
	application.Send(worktable.WorkTableUpdate{Revision: 7, Items: []model.WorkItem{{ID: "tail"}}})
	application.Close()
	select {
	case payload := <-published:
		if len(payload.Items) != 1 || payload.Items[0].ID != "tail" {
			t.Fatalf("tail payload = %+v", payload)
		}
	case <-time.After(time.Second):
		t.Fatal("close did not publish the tail")
	}
	application.Send(worktable.WorkTableUpdate{Revision: 8})
}

func receivePayload(t *testing.T, published <-chan model.WorkTableEvent) model.WorkTableEvent {
	t.Helper()
	select {
	case payload := <-published:
		return payload
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for worktable payload")
		return model.WorkTableEvent{}
	}
}
