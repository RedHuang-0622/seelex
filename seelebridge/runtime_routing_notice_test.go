package seelebridge

import (
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/internal/stream"
)

// ── served route 注册表（observeRoutingNotice → TakeServedRoute）─────────────
//
// 口径（2026-10-10 用户决定）：request/failover 事实覆盖 per-session 注册表，
// application 回合收尾经 TakeServedRoute 读即清、回填 assistant 消息真相源；
// 会话内路由通知（system 消息 / channel 投递）已退役，failover/unpinned 只落
// B 类摘要（durable，审计用）。

func TestRoutingNoticeObserverRecordsServedRouteAndSummary(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()

	notice := stream.RoutingNotice{
		SessionID:   "session-1",
		At:          time.Now(),
		Reason:      stream.ReasonFailover,
		Role:        "agent",
		From:        "agent-1",
		To:          "subagent-1",
		Model:       "child-model",
		Provider:    "openai",
		FailureKind: string(seelFailureKindQuota),
	}
	runtime.observeRoutingNotice(notice)

	route, ok := runtime.TakeServedRoute("session-1")
	if !ok {
		t.Fatal("failover 事实未进入 served route 注册表")
	}
	if route.Role != "agent" || route.Account != "subagent-1" || route.Model != "child-model" || route.Provider != "openai" {
		t.Fatalf("served route = %+v", route)
	}
	// B 类摘要：durable 记录一条 route 摘要，Detail 非空。
	summaries := runtime.summaryLog.Summaries()
	if len(summaries) == 0 {
		t.Fatal("路由事实未写入 B 类摘要日志")
	}
	last := summaries[len(summaries)-1]
	if last.Kind != "route" || last.Detail == "" {
		t.Fatalf("route 摘要 = %+v", last)
	}
	// 读即清：二次取回为空。
	if _, ok := runtime.TakeServedRoute("session-1"); ok {
		t.Fatal("TakeServedRoute 必须读即清")
	}
}

func TestRoutingNoticeObserverOverwritesRouteWithFinalAttempt(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()

	// 首次派发（request 钩子）→ 换号重试（failover）：最终承接账号覆盖注册表。
	runtime.observeRoutingNotice(stream.RoutingNotice{
		SessionID: "session-1", At: time.Now(),
		Reason: stream.ReasonRequest, Role: "agent", To: "agent-1",
		Model: "main-model", Provider: "openai",
	})
	runtime.observeRoutingNotice(stream.RoutingNotice{
		SessionID: "session-1", At: time.Now(),
		Reason: stream.ReasonFailover, Role: "agent", From: "agent-1", To: "agent-2",
		Model: "fallback-model", Provider: "openai",
	})
	route, ok := runtime.TakeServedRoute("session-1")
	if !ok {
		t.Fatal("served route 丢失")
	}
	if route.Account != "agent-2" || route.Model != "fallback-model" {
		t.Fatalf("最终承接账号必须覆盖注册表：%+v", route)
	}
}

func TestRoutingNoticeObserverSkipsNodeAndProcessLevelRequests(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()

	// 节点请求：路由归子代理/plan 节点视图，不进主会话注册表。
	runtime.observeRoutingNotice(stream.RoutingNotice{
		SessionID: "session-1", NodeID: "node-1", At: time.Now(),
		Reason: stream.ReasonRequest, Role: "subagent", To: "subagent-1",
	})
	// 进程级请求（无会话归属）：无处回填。
	runtime.observeRoutingNotice(stream.RoutingNotice{
		SessionID: "", At: time.Now(),
		Reason: stream.ReasonRequest, Role: "agent", To: "agent-1",
	})
	if _, ok := runtime.TakeServedRoute("session-1"); ok {
		t.Fatal("节点请求不得进入注册表")
	}
}

func TestRoutingNoticeObserverDedupesUnpinnedSummaryPerSessionAndAccount(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()

	notice := stream.RoutingNotice{
		SessionID: "session-1",
		At:        time.Now(),
		Reason:    stream.ReasonUnpinnedSelection,
		To:        "subagent-1",
		Model:     "child-model",
		Provider:  "openai",
	}
	runtime.observeRoutingNotice(notice)
	if got := len(runtime.summaryLog.Summaries()); got != 1 {
		t.Fatalf("首次 unpinned_selection 应记 1 条摘要，实际 %d", got)
	}
	// 同会话同账号：去重。
	runtime.observeRoutingNotice(notice)
	if got := len(runtime.summaryLog.Summaries()); got != 1 {
		t.Fatalf("unpinned_selection 被重复记录：%d", got)
	}
	// 同会话不同账号：再记一次。
	runtime.observeRoutingNotice(stream.RoutingNotice{
		SessionID: "session-1", At: time.Now(),
		Reason: stream.ReasonUnpinnedSelection, To: "goalplan-1",
		Model: "plan-model", Provider: "openai",
	})
	// 不同会话：再记一次。
	runtime.observeRoutingNotice(stream.RoutingNotice{
		SessionID: "session-2", At: time.Now(),
		Reason: stream.ReasonUnpinnedSelection, To: "subagent-1",
		Model: "child-model", Provider: "openai",
	})
	if got := len(runtime.summaryLog.Summaries()); got != 3 {
		t.Fatalf("摘要数 = %d，期望 3", got)
	}
}

// seelFailureKindQuota 复用 account.FailureQuota 的词表（同包测试的别名，避免
// 测试文件再 import 一个包——词表唯一来源仍是 seelebridge/account）。
const seelFailureKindQuota = "quota_exhausted"
