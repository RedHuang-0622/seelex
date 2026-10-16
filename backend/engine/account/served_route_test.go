package account

import (
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/telemetry"
)

// P21-3 域级用例：路由事实注册表的四条判据（哪些原因覆盖、只有主链路才记、读即清、
// unpinned 摘要去重）与残格回收。装配层的端到端用例仍走宿主转发面（`TakeServedRoute`）。

// countingRegistry 造一例带摘要计数的注册表（"记了几条摘要"是去重判据的可观测面）。
func countingRegistry() (*ServedRouteRegistry, *int) {
	count := new(int)
	registry := NewServedRouteRegistry(ServedRouteDeps{
		RecordSummary: func(event telemetry.SummaryEvent) { *count++ },
	})
	return registry, count
}

func TestServedRouteOnlyMainChainRequestsAreRecorded(t *testing.T) {
	registry := NewServedRouteRegistry(ServedRouteDeps{})
	at := time.Unix(11, 0)
	// 子代理/plan 节点请求：不记录（它们的路由归各自的节点视图）。
	registry.Observe(dto.RoutingNotice{
		Reason: dto.RoutingReasonRequest, SessionID: "session-a", NodeID: "node-1", To: "acct-node", At: at,
	})
	if route, ok := registry.Take("session-a"); ok {
		t.Fatalf("节点请求不得进注册表：%+v", route)
	}
	// 进程级请求（无会话归属）：无处回填，不记录。
	registry.Observe(dto.RoutingNotice{Reason: dto.RoutingReasonRequest, To: "acct-process", At: at})
	// 主链路请求：记录，且字段逐项对上。
	registry.Observe(dto.RoutingNotice{
		Reason: dto.RoutingReasonRequest, SessionID: "session-a", Role: "agent",
		To: "acct-main", Provider: "openai", Model: "glm-5.3", At: at,
	})
	route, ok := registry.Take("session-a")
	if !ok {
		t.Fatal("主链路请求必须记录")
	}
	if route.Account != "acct-main" || route.Provider != "openai" || route.Model != "glm-5.3" ||
		route.Role != "agent" || !route.At.Equal(at) {
		t.Fatalf("承接路由字段不对：%+v", route)
	}
}

func TestServedRouteTakeIsReadAndClear(t *testing.T) {
	registry := NewServedRouteRegistry(ServedRouteDeps{})
	registry.Observe(dto.RoutingNotice{Reason: dto.RoutingReasonRequest, SessionID: "s", To: "acct"})
	if _, ok := registry.Take("s"); !ok {
		t.Fatal("第一次取应命中")
	}
	if route, ok := registry.Take("s"); ok {
		t.Fatalf("读即清：第二次取不得再命中：%+v", route)
	}
	if _, ok := registry.Take(""); ok {
		t.Fatal("空会话 ID 不得命中")
	}
	if _, ok := (*ServedRouteRegistry)(nil).Take("s"); ok {
		t.Fatal("nil 注册表不得命中")
	}
}

func TestServedRouteFailoverOverwritesAndRepeatsSummary(t *testing.T) {
	registry, summaries := countingRegistry()
	registry.Observe(dto.RoutingNotice{Reason: dto.RoutingReasonFailover, SessionID: "s", From: "acct-1", To: "acct-2"})
	registry.Observe(dto.RoutingNotice{Reason: dto.RoutingReasonFailover, SessionID: "s", From: "acct-2", To: "acct-3"})
	if *summaries != 2 {
		t.Fatalf("failover 不去重（每次都要可审计）：summaries = %d, want 2", *summaries)
	}
	route, ok := registry.Take("s")
	if !ok || route.Account != "acct-3" {
		t.Fatalf("最后一次 failover 应覆盖承接路由：%+v (ok=%v)", route, ok)
	}
	// request 只记路由、不记摘要（它是每次请求都发生的派发钩子）。
	registry.Observe(dto.RoutingNotice{Reason: dto.RoutingReasonRequest, SessionID: "s", To: "acct-4"})
	if *summaries != 2 {
		t.Fatalf("request 不应记摘要：summaries = %d, want 2", *summaries)
	}
}

func TestServedRouteUnpinnedSelectionDedupsSummaryAndSkipsRoute(t *testing.T) {
	registry, summaries := countingRegistry()
	registry.Observe(dto.RoutingNotice{Reason: dto.RoutingReasonUnpinnedSelection, SessionID: "s", To: "acct-9"})
	registry.Observe(dto.RoutingNotice{Reason: dto.RoutingReasonUnpinnedSelection, SessionID: "s", To: "acct-9"})
	if *summaries != 1 {
		t.Fatalf("unpinned 应去重（同会话+账号一次）：summaries = %d, want 1", *summaries)
	}
	// 换账号 = 新键，再记一次；换会话同理。
	registry.Observe(dto.RoutingNotice{Reason: dto.RoutingReasonUnpinnedSelection, SessionID: "s", To: "acct-10"})
	registry.Observe(dto.RoutingNotice{Reason: dto.RoutingReasonUnpinnedSelection, SessionID: "other", To: "acct-10"})
	if *summaries != 3 {
		t.Fatalf("新键应各记一次：summaries = %d, want 3", *summaries)
	}
	// unpinned_selection **不覆盖**承接路由（只有 request/failover 算数）。
	if route, ok := registry.Take("s"); ok {
		t.Fatalf("unpinned 不进承接路由注册表：%+v", route)
	}
}

func TestServedRouteForgetDropsSessionCells(t *testing.T) {
	registry, summaries := countingRegistry()
	registry.Observe(dto.RoutingNotice{Reason: dto.RoutingReasonRequest, SessionID: "sess-a", To: "acct"})
	registry.Observe(dto.RoutingNotice{Reason: dto.RoutingReasonUnpinnedSelection, SessionID: "sess-a", To: "acct"})
	registry.Observe(dto.RoutingNotice{Reason: dto.RoutingReasonUnpinnedSelection, SessionID: "sess-b", To: "acct"})
	if *summaries != 2 {
		t.Fatalf("播种：summaries = %d, want 2", *summaries)
	}
	registry.Forget("sess-a")
	if route, ok := registry.Take("sess-a"); ok {
		t.Fatalf("承接路由残格未清：%+v", route)
	}
	registry.Observe(dto.RoutingNotice{Reason: dto.RoutingReasonUnpinnedSelection, SessionID: "sess-a", To: "acct"})
	if *summaries != 3 {
		t.Fatalf("去重表残格未清：应重新记一次摘要（summaries = %d, want 3）", *summaries)
	}
	registry.Observe(dto.RoutingNotice{Reason: dto.RoutingReasonUnpinnedSelection, SessionID: "sess-b", To: "acct"})
	if *summaries != 3 {
		t.Fatalf("别的会话的去重键被误删：summaries = %d, want 3", *summaries)
	}
	registry.Forget("") // 空会话 ID：无操作（不 panic）
}

func TestRoutingNoticeMessageCarriesOnlyRoutingFacts(t *testing.T) {
	message := RoutingNoticeMessage(dto.RoutingNotice{
		Reason: dto.RoutingReasonFailover, SessionID: "session-a", From: "acct-1", To: "acct-2",
		Model: "glm-5.3", Provider: "openai", FailureKind: string(FailureQuota),
	})
	for _, want := range []string{"acct-2", "glm-5.3"} {
		if !strings.Contains(message, want) {
			t.Fatalf("摘要文案应带上 %q：%q", want, message)
		}
	}
}
