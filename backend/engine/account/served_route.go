package account

// served_route.go 是**每会话最近一次请求的"实际承接路由"注册表**（P21-3「域方法随类型走」：
// 从 `backend/engine/assemble/runtime_account.go` 把状态与判据搬进账号路由域）。
//
// 它回答一个问题：这一轮真实请求最终是由哪个账号/provider/model 承接的？事实由流式
// completer 在租约路径上同步投进来（`Observe`），由回合收尾取走一次（`Take`，**读即清**：
// 残留会让下一轮在没有请求真正派发时误用旧路由）。三条判据随域走：
//
//  1. 只有 `request` / `failover` 两种原因才覆盖注册表（最终承接账号覆盖前面的尝试）；
//  2. 只有**主链路请求**（`NodeID` 空、会话非空）才记录——子代理/plan 节点的路由归各自的
//     节点视图，进程级请求（无会话归属）无处回填；
//  3. `unpinned_selection` 按「会话+账号」**只记一次摘要**（主链路未固定账号时它每轮都发生，
//     重复记录是噪音）；`failover` 不去重——每一次都必须可审计。

import (
	"sync"

	"github.com/RedHuang-0622/seelex/internal/telemetry"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// ServedRouteDeps 是注册表的宿主面（只差一个摘要落点）。
type ServedRouteDeps struct {
	// RecordSummary 落一条 B 类摘要（nil = 不记摘要，判据与注册表照常工作）。
	RecordSummary func(event telemetry.SummaryEvent)
}

// ServedRouteRegistry 是路由事实注册表（按会话键控；读即清）。
type ServedRouteRegistry struct {
	deps ServedRouteDeps

	mu     sync.Mutex
	routes map[string]dto.ServedRoute
	seen   map[string]bool // unpinned_selection 摘要去重（键 = 会话|账号）
}

// NewServedRouteRegistry 装配一例（装配点是组合根 `assemble.Runtime` 的构造器）。
func NewServedRouteRegistry(deps ServedRouteDeps) *ServedRouteRegistry {
	return &ServedRouteRegistry{
		deps:   deps,
		routes: make(map[string]dto.ServedRoute),
		seen:   make(map[string]bool),
	}
}

// Observe 是流式 completer 的路由事实观察者（租约路径上同步调用，必须非阻塞）。
func (registry *ServedRouteRegistry) Observe(notice dto.RoutingNotice) {
	if registry == nil {
		return
	}
	if notice.Reason == dto.RoutingReasonRequest || notice.Reason == dto.RoutingReasonFailover {
		registry.record(notice)
	}
	// unpinned_selection 每个会话+账号 只记一次摘要：主链路未固定账号时它每轮
	// 都会发生，重复记录是噪音；换号（failover）不去重——每一次都必须可审计。
	if notice.Reason == dto.RoutingReasonUnpinnedSelection {
		key := notice.SessionID + "|" + notice.To
		registry.mu.Lock()
		seen := registry.seen[key]
		registry.seen[key] = true
		registry.mu.Unlock()
		if seen {
			return
		}
	}
	if notice.Reason != dto.RoutingReasonFailover && notice.Reason != dto.RoutingReasonUnpinnedSelection {
		return
	}
	if registry.deps.RecordSummary == nil {
		return
	}
	registry.deps.RecordSummary(telemetry.SummaryEvent{
		Kind:   "route",
		Name:   "reroute",
		Status: "completed",
		At:     notice.At,
		NodeID: notice.NodeID,
		Detail: RoutingNoticeMessage(notice),
	})
}

// record 把一次路由事实覆盖进该会话的注册表（只有主链路请求才记录，判据见文件头）。
func (registry *ServedRouteRegistry) record(notice dto.RoutingNotice) {
	if notice.SessionID == "" || notice.NodeID != "" {
		return
	}
	registry.mu.Lock()
	registry.routes[notice.SessionID] = dto.ServedRoute{
		Role:     notice.Role,
		Account:  notice.To,
		Provider: notice.Provider,
		Model:    notice.Model,
		At:       notice.At,
	}
	registry.mu.Unlock()
}

// Take 取出（并清掉）该会话最近一次请求的实际承接路由（读即清；无事实返回 ok=false）。
func (registry *ServedRouteRegistry) Take(sessionID string) (dto.ServedRoute, bool) {
	if registry == nil || sessionID == "" {
		return dto.ServedRoute{}, false
	}
	registry.mu.Lock()
	route, ok := registry.routes[sessionID]
	delete(registry.routes, sessionID)
	registry.mu.Unlock()
	return route, ok
}

// Forget 丢掉某个会话留在注册表与去重表里的残格（会话卸载/删除时调用；
// 去重表键是 `会话|账号`，按前缀清理）。
func (registry *ServedRouteRegistry) Forget(sessionID string) {
	if registry == nil || sessionID == "" {
		return
	}
	registry.mu.Lock()
	delete(registry.routes, sessionID)
	prefix := sessionID + "|"
	for key := range registry.seen {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			delete(registry.seen, key)
		}
	}
	registry.mu.Unlock()
}

// RoutingNoticeMessage 把内部路由事实翻成 B 类摘要的一句可读说明
// （与 `dto.RoutingNotice.Message` 同源文案；只带非敏感路由属性）。
func RoutingNoticeMessage(notice dto.RoutingNotice) string {
	return dto.RoutingNotice{
		SessionID:   notice.SessionID,
		NodeID:      notice.NodeID,
		At:          notice.At,
		Reason:      notice.Reason,
		Role:        notice.Role,
		From:        notice.From,
		To:          notice.To,
		Model:       notice.Model,
		Provider:    notice.Provider,
		FailureKind: notice.FailureKind,
	}.Message()
}
