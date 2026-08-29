package core

import "testing"

func TestServiceAddNoticeAppendsSystemMessage(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	service.AddNotice("startup warning")
	waitForSnapshot(t, service, func(snapshot Snapshot) bool {
		if len(snapshot.Conversation) == 0 {
			return false
		}
		last := snapshot.Conversation[len(snapshot.Conversation)-1]
		return last.Role == "system" && last.Content == "startup warning"
	})
}
