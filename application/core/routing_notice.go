package core

import (
	"log"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// consumeRoutingNotices 是生命周期消费者之一：账号路由事实 → 会话 system 消息。
//
// 口径（与启动警告一路同源）：
//   - failover（账号资格失败换号）：**必须可见**——用户配的 agent 账号挂了、
//     请求被换到别的账号继续，这种「说的和跑的不是同一个账号」如果静默发生，
//     排障成本极高；
//   - unpinned_selection（会话未固定账号，池内选定）：runtime 侧已按
//     会话+账号 去重，这里照单呈现。
//
// 呈现面是 AddNotice（system 消息进可见会话 + message.added 事件）。只有
// 路由事实归属的会话正是当前可见会话（或解析不到会话）时才进会话——后台
// 会话的路由事实不串进前台（用户切到那个会话前的历史里没有它的必要）。
func (service *Service) consumeRoutingNotices() {
	notices := service.Deps.Runtime.RoutingNoticeChannel()
	if notices == nil {
		return
	}
	for {
		select {
		case notice, ok := <-notices:
			if !ok {
				return
			}
			service.safeLifecycleCall(func() { service.presentRoutingNotice(notice) })
		case <-service.lifecycleStop:
			return
		}
	}
}

// presentRoutingNotice 把一条路由事实呈现为当前可见会话的 system 消息。
// 会话不匹配（后台会话的路由事实）只记日志，不打进前台会话。
func (service *Service) presentRoutingNotice(notice dto.RoutingNotice) {
	// request 钩子（每请求一条）走独立呈现面：瞬态 request.meta 会话事件，
	// **不进**会话消息（不污染真相源，见 dto.RequestMeta 的口径注释）。
	if notice.Reason == dto.RoutingReasonRequest {
		service.presentRequestMeta(notice)
		return
	}
	message := notice.Message()
	service.ViewMu.RLock()
	sessionID := service.Core.Snapshot.Session.ID
	service.ViewMu.RUnlock()
	if notice.SessionID != "" && notice.SessionID != sessionID {
		// 归属会话不在前台（含草稿视图 sessionID 为空的情形）：system 消息串进
		// 别的会话是历史污染，只落日志。
		log.Printf("seelebridge: 路由提示（会话 %s）：%s", notice.SessionID, message)
		return
	}
	service.addNotice(message)
}

// presentRequestMeta 把一条 request 钩子路由事实投成 request.meta 会话事件
// （瞬态、不落历史）。只有主链路请求（NodeID 空）才投：子代理/plan 节点的
// 元信息归各自的子代理视图，不串进主会话事件流；解析不到会话的进程级请求
// 无处路由，只记日志。
func (service *Service) presentRequestMeta(notice dto.RoutingNotice) {
	if notice.NodeID != "" {
		return
	}
	sessionID := notice.SessionID
	if sessionID == "" {
		log.Printf("seelebridge: 请求派发（无会话归属）：%s/%s role=%s", notice.Provider, notice.Model, notice.Role)
		return
	}
	effort := notice.ReasoningEffort
	if effort == dto.ReasoningEffortSession {
		// 跟随会话档位的账号：把 "session" 解析成当前会话档位对应的 wire 强度，
		// 前端不必再自己联表。
		effort = service.knobReasonings()
	}
	service.publishSessionEvent(EventRequestMeta, 0, "", sessionID, dto.RequestMeta{
		Role:            notice.Role,
		Account:         notice.To,
		Provider:        notice.Provider,
		Model:           notice.Model,
		ReasoningEffort: effort,
		At:              notice.At,
	})
}
