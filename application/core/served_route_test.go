package core

import (
	session "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"testing"
	"time"
)

// ── served route 回填（stampServedRoute）─────────────────────────────────────
//
// 口径（2026-10-10 用户决定）：路由事实由请求解析中间件（账号池 completer 租约
// 路径）得出，回合收尾随 response 回填**真相源**——assistant transcript 事件行
// （冷恢复/轨迹审计）+ 可见窗口本轮 assistant 消息（前端 message 块元数据行）。
// 会话内路由通知（system 消息 / request.meta 瞬态事件）已退役。

func TestStampServedRouteFillsAssistantMessageAndPublishes(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	service.ViewMu.RLock()
	sessionID := service.Core.Snapshot.Session.ID
	service.ViewMu.RUnlock()
	if sessionID == "" {
		t.Skip("测试服务没有绑定会话，回填用例无法构造")
	}
	// 本轮 assistant 消息（startChatFor 在请求前追加的占位形状）。
	service.ViewMu.Lock()
	appended := service.appendMessageWithOriginLocked("assistant", "", nil, model.MessageOrigin{RoleName: RoleNameMain})
	service.ViewMu.Unlock()

	runtime := &fakeRuntime{}
	runtime.servedRouteSession = sessionID
	runtime.servedRoute = dto.ServedRoute{
		Role: "agent", Account: "agent-1", Provider: "openai", Model: "glm-5.3-flash", At: time.Now(),
	}
	service.Deps.Runtime = runtime

	service.stampServedRoute(sessionID, "chat-request-1")

	if appended.ConfigRole != "agent" || appended.ConfigModel != "glm-5.3-flash" || appended.ConfigProvider != "openai" {
		t.Fatalf("可见消息未回填路由：%+v", appended)
	}
	// 快照镜像同步带上（前端基线刷新同源）。
	waitForSnapshot(t, service, func(snapshot model.Snapshot) bool {
		for index := len(snapshot.Conversation) - 1; index >= 0; index-- {
			message := snapshot.Conversation[index]
			if message.Role != "assistant" || message.Tool != nil {
				continue
			}
			return message.ConfigRole == "agent" && message.ConfigModel == "glm-5.3-flash" && message.ConfigProvider == "openai"
		}
		return false
	})
	// 读即清：没有下一轮残留事实。
	if _, ok := runtime.TakeServedRoute(sessionID); ok {
		t.Fatal("TakeServedRoute 必须读即清")
	}
}

func TestStampServedRouteWithoutFactIsNoop(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	service.ViewMu.RLock()
	sessionID := service.Core.Snapshot.Session.ID
	service.ViewMu.RUnlock()
	service.ViewMu.Lock()
	appended := service.appendMessageWithOriginLocked("assistant", "", nil, model.MessageOrigin{RoleName: RoleNameMain})
	service.ViewMu.Unlock()

	service.stampServedRoute(sessionID, "chat-request-1")

	if appended.ConfigRole != "" || appended.ConfigModel != "" {
		t.Fatalf("无路由事实时不得写消息：%+v", appended)
	}
}

func TestStampServedRouteSkipsAlreadyStampedTail(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	service.ViewMu.RLock()
	sessionID := service.Core.Snapshot.Session.ID
	service.ViewMu.RUnlock()
	service.ViewMu.Lock()
	service.appendMessageWithOriginLocked("assistant", "", nil, model.MessageOrigin{RoleName: RoleNameMain, RoundID: 7})
	service.ViewMu.Unlock()
	// 上一轮消息已带路由：本轮占位缺失（异常形状）时不得把新路由串上去。
	service.ViewMu.Lock()
	service.components.view.SessionViewMutateLocked(sessionID, func(view *session.View) {
		last := &view.Conversation[len(view.Conversation)-1]
		last.ConfigRole = "agent"
		last.ConfigModel = "old-model"
		last.ConfigProvider = "openai"
	})
	service.ViewMu.Unlock()

	runtime := &fakeRuntime{}
	runtime.servedRouteSession = sessionID
	runtime.servedRoute = dto.ServedRoute{Role: "agent", Model: "new-model", Provider: "openai", At: time.Now()}
	service.Deps.Runtime = runtime

	service.stampServedRoute(sessionID, "chat-request-2")

	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	view := service.sessionViewLocked(sessionID)
	last := view.Conversation[len(view.Conversation)-1]
	if last.ConfigModel != "old-model" {
		t.Fatalf("已带路由的尾部消息不得被改写：%+v", last)
	}
}

// StampAssistantRouteFor 的行级口径：只写本轮（TaskID 匹配）的 assistant 行，
// 幂等，不碰 user/tool 行。
func TestStampAssistantRouteForScopeAndIdempotence(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	service.ViewMu.RLock()
	sessionID := service.Core.Snapshot.Session.ID
	service.ViewMu.RUnlock()
	service.components.tasks.AppendTranscriptEventLocked(model.TranscriptEvent{TaskID: "req-1", Role: "user", Content: "q"})
	service.components.tasks.AppendTranscriptEventLocked(model.TranscriptEvent{TaskID: "req-1", Role: "assistant", Content: "step"})
	service.components.tasks.AppendTranscriptEventLocked(model.TranscriptEvent{TaskID: "req-2", Role: "assistant", Content: "other round"})
	service.components.tasks.AppendTranscriptEventLocked(model.TranscriptEvent{TaskID: "req-1", Role: "tool", Content: "result"})

	stamped := service.components.tasks.StampAssistantRouteFor(sessionID, "req-1", "agent", "glm-5.3-flash", "openai")
	if stamped != 1 {
		t.Fatalf("应恰好打中本轮一条 assistant 行，实际 %d", stamped)
	}
	if again := service.components.tasks.StampAssistantRouteFor(sessionID, "req-1", "agent", "glm-5.3-flash", "openai"); again != 0 {
		t.Fatalf("重复回填必须幂等（0 行），实际 %d", again)
	}
	for _, event := range service.components.tasks.TranscriptFor(sessionID) {
		switch {
		case event.TaskID != "req-1":
			if event.ConfigModel != "" {
				t.Fatalf("别的轮次被串写：%+v", event)
			}
		case event.Role != "assistant":
			if event.ConfigModel != "" {
				t.Fatalf("非 assistant 行被串写：%+v", event)
			}
		default:
			if event.ConfigRole != "agent" || event.ConfigModel != "glm-5.3-flash" || event.ConfigProvider != "openai" {
				t.Fatalf("assistant 行路由不完整：%+v", event)
			}
		}
	}
}
