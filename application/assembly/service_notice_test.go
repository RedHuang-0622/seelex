package assembly

import (
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"testing"
)

func TestServiceAddNoticeAppendsSystemMessage(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	service.components.view.AddNotice("startup warning")
	waitForSnapshot(t, service, func(snapshot model.Snapshot) bool {
		if len(snapshot.Conversation) == 0 {
			return false
		}
		last := snapshot.Conversation[len(snapshot.Conversation)-1]
		return last.Role == "system" && last.Content == "startup warning"
	})
}
