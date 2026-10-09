package core

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// ── 账号路由提示的呈现面（consumeRoutingNotices 的消费端）────────────────────
//
// 口径：路由事实呈现为当前可见会话的 system 消息（AddNotice 一路）；归属
// 其它会话的路由事实不串进前台（历史污染），只落进程日志。

func TestPresentRoutingNoticeAppendsSystemMessage(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	notice := dto.RoutingNotice{
		SessionID:   "", // 进程级 / 草稿视图：照单呈现
		At:          time.Now(),
		Reason:      dto.RoutingReasonFailover,
		From:        "agent-1",
		To:          "subagent-1",
		Model:       "child-model",
		Provider:    "openai",
		FailureKind: "quota_exhausted",
	}
	service.presentRoutingNotice(notice)
	waitForSnapshot(t, service, func(snapshot Snapshot) bool {
		if len(snapshot.Conversation) == 0 {
			return false
		}
		last := snapshot.Conversation[len(snapshot.Conversation)-1]
		return last.Role == "system" && last.Content == notice.Message()
	})
}

func TestPresentRoutingNoticeSkipsForeignSession(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	service.ViewMu.RLock()
	sessionID := service.Core.Snapshot.Session.ID
	service.ViewMu.RUnlock()
	if sessionID == "" {
		t.Skip("测试服务没有绑定会话， foreign-session 分支无法构造")
	}
	before := len(service.Core.Snapshot.Conversation)
	notice := dto.RoutingNotice{
		SessionID: sessionID + "-other",
		At:        time.Now(),
		Reason:    dto.RoutingReasonUnpinnedSelection,
		To:        "subagent-1",
		Model:     "child-model",
		Provider:  "openai",
	}
	service.presentRoutingNotice(notice)
	service.ViewMu.RLock()
	after := len(service.Core.Snapshot.Conversation)
	service.ViewMu.RUnlock()
	if after != before {
		t.Fatalf("其它会话的路由事实不得进入当前会话：%d → %d", before, after)
	}
}

func TestRoutingNoticeMessageWordings(t *testing.T) {	failover := dto.RoutingNotice{
		Reason: dto.RoutingReasonFailover, From: "agent-1", To: "subagent-1",
		Model: "child-model", Provider: "openai", FailureKind: "quota_exhausted",
	}
	if got := failover.Message(); got == "" || failover.From == failover.To {
		t.Fatalf("failover 文案异常：%q", got)
	}
	unpinned := dto.RoutingNotice{
		Reason: dto.RoutingReasonUnpinnedSelection, To: "subagent-1",
		Model: "child-model", Provider: "openai",
	}
	if got := unpinned.Message(); got == "" {
		t.Fatal("unpinned 文案异常")
	}
}

// ── request.meta 呈现面（presentRequestMeta）────────────────────────────────
//
// 口径：request 钩子（每请求一条）是**瞬态**元信息——投成 request.meta 会话
// 事件供前端实时渲染 message 块元信息条，**不进**会话消息（不污染真相源）。

func TestPresentRequestMetaPublishesSessionEvent(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	service.ViewMu.RLock()
	sessionID := service.Core.Snapshot.Session.ID
	service.ViewMu.RUnlock()
	if sessionID == "" {
		t.Skip("测试服务没有绑定会话，会话级事件路由无法构造")
	}
	subscription := service.Subscribe(16)
	defer subscription.Close()

	at := time.Now()
	service.presentRoutingNotice(dto.RoutingNotice{
		SessionID:       sessionID,
		At:              at,
		Reason:          dto.RoutingReasonRequest,
		Role:            string(dto.RoleAgent),
		To:              "agent-1",
		Model:           "main-model",
		Provider:        "openai",
		ReasoningEffort: "high",
	})

	deadline := time.After(2 * time.Second)
	for {
		select {
		case event, ok := <-subscription.Events:
			if !ok {
				t.Fatal("事件流提前关闭")
			}
			if event.Kind != EventRequestMeta {
				continue
			}
			var meta dto.RequestMeta
			if err := json.Unmarshal(event.Payload, &meta); err != nil {
				t.Fatalf("request.meta 载荷解码失败：%v", err)
			}
			if meta.Role != string(dto.RoleAgent) || meta.Account != "agent-1" ||
				meta.Model != "main-model" || meta.Provider != "openai" || meta.ReasoningEffort != "high" {
				t.Fatalf("request.meta 载荷 = %+v", meta)
			}
			return
		case <-deadline:
			t.Fatal("未收到 request.meta 事件")
		}
	}
}

func TestPresentRequestMetaSkipsNodeAndForeignSession(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	service.ViewMu.RLock()
	sessionID := service.Core.Snapshot.Session.ID
	service.ViewMu.RUnlock()
	if sessionID == "" {
		t.Skip("测试服务没有绑定会话，会话级事件路由无法构造")
	}
	subscription, _ := service.SubscribeSession(sessionID, 16)
	defer subscription.Close()

	// 节点请求（NodeID 非空）：元信息归子代理视图，不得进主会话事件流。
	service.presentRoutingNotice(dto.RoutingNotice{
		SessionID: sessionID, NodeID: "node-1", At: time.Now(),
		Reason: dto.RoutingReasonRequest, Role: string(dto.RoleSubAgent), To: "subagent-1",
	})
	// 外来会话：事件按它自己的会话路由键投递，不得落进当前会话的订阅。
	service.presentRoutingNotice(dto.RoutingNotice{
		SessionID: sessionID + "-other", At: time.Now(),
		Reason: dto.RoutingReasonRequest, Role: string(dto.RoleAgent), To: "agent-1",
	})

	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case event := <-subscription.Events:
			if event.Kind == EventRequestMeta {
				t.Fatalf("节点/外来会话的 request 钩子不应落进当前会话订阅，得到 sid=%s", event.SessionID)
			}
			continue
		case <-deadline:
			return
		}
	}
}

func TestPresentRequestMetaResolvesSessionEffort(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	service.ViewMu.RLock()
	sessionID := service.Core.Snapshot.Session.ID
	service.ViewMu.RUnlock()
	if sessionID == "" {
		t.Skip("测试服务没有绑定会话，会话级事件路由无法构造")
	}
	// knobReasonings 的当前读数（测试服务未设置时为空串）：载荷里不得再出现
	// 字面 "session"——要么解析成 wire 值，要么解析成空（由前端显示成"跟随档位"）。
	subscription := service.Subscribe(16)
	defer subscription.Close()

	service.presentRoutingNotice(dto.RoutingNotice{
		SessionID: sessionID, At: time.Now(),
		Reason: dto.RoutingReasonRequest, Role: string(dto.RoleAgent), To: "agent-1",
		ReasoningEffort: dto.ReasoningEffortSession,
	})

	deadline := time.After(2 * time.Second)
	for {
		select {
		case event, ok := <-subscription.Events:
			if !ok {
				t.Fatal("事件流提前关闭")
			}
			if event.Kind != EventRequestMeta {
				continue
			}
			var meta dto.RequestMeta
			if err := json.Unmarshal(event.Payload, &meta); err != nil {
				t.Fatalf("request.meta 载荷解码失败：%v", err)
			}
			if meta.ReasoningEffort == dto.ReasoningEffortSession {
				t.Fatalf("request.meta 载荷仍携带字面 session：%+v", meta)
			}
			return
		case <-deadline:
			t.Fatal("未收到 request.meta 事件")
		}
	}
}
