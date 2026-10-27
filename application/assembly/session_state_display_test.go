package assembly

import (
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"testing"
	"time"
)

// TestBackgroundSessionStateChangeNotifiesDisplaySurfaces 钉住"状态 owner 到
// 显示面"的集成：后台会话的运行态变化没有自己的视图订阅，必须发一条**进程级**
// snapshot.changed（sessionID 为空），前端据此重拉权威快照、按会话单元重算目录行。
//
// 当前视图会话则不需要这条额外通知（它自己的 chat.changed / snapshot.changed
// 已经到达显示面）——通知重复会让每次状态变化多一次整份刷新。

func TestBackgroundSessionStateChangeNotifiesDisplaySurfaces(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.components.lifecycleApplication.Shutdown()

	subscription := service.Events.Subscribe(64)
	defer subscription.Close()

	service.ViewMu.Lock()
	service.Core.Snapshot.Session = model.SessionState{ID: "sess-active"}
	background := service.sessionUnitLocked("sess-background")
	service.ViewMu.Unlock()
	background.SetChatState(model.ChatState{Running: true}, nil)

	service.publishChatStateFor("sess-background")
	service.publishChatStateFor("sess-active")

	var processEvents, activeEvents int
	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case event := <-subscription.Events:
			switch {
			case event.Kind == EventSessionsChanged && event.SessionID == "":
				processEvents++
			case event.Kind == EventSessionsChanged && event.SessionID == "sess-active":
				activeEvents++
			}
			if processEvents >= 1 && activeEvents >= 0 {
				// 再收一小段，确认 active 会话没有多发进程级通知。
				select {
				case extra := <-subscription.Events:
					if extra.Kind == EventSessionsChanged && extra.SessionID == "" {
						t.Fatalf("当前视图会话的状态变化不应再发进程级通知：%+v", extra)
					}
				case <-time.After(50 * time.Millisecond):
				}
				return
			}
		case <-deadline:
			t.Fatalf("后台会话状态变化没有通知显示面：process=%d active=%d", processEvents, activeEvents)
		}
	}
}
