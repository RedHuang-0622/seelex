package assembly

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestServiceAddNoticeAppendsSystemMessage(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	frontendOfTestService(service).AddNotice("startup warning")
	waitForSnapshot(t, service, func(snapshot model.Snapshot) bool {
		if len(snapshot.Conversation) == 0 {
			return false
		}
		last := snapshot.Conversation[len(snapshot.Conversation)-1]
		return last.Role == "system" && last.Content == "startup warning"
	})
}
