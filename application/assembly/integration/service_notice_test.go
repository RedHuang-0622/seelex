package integration

import (
	. "github.com/RedHuang-0622/seelex/application/assembly"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"testing"
)

func TestServiceAddNoticeAppendsSystemMessage(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	ComponentsFor(service).Session.View.AddNotice("startup warning")
	waitForSnapshot(t, service, func(snapshot model.Snapshot) bool {
		if len(snapshot.Conversation) == 0 {
			return false
		}
		last := snapshot.Conversation[len(snapshot.Conversation)-1]
		return last.Role == "system" && last.Content == "startup warning"
	})
}
