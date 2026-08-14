package core

import (
	"fmt"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// SubscribeSubagentLive 订阅 node 第一视角实时流（阶段+工具事件即时投递，
// 非轮询/缓存）：返回只读事件通道与取消函数（取消幂等）。
func (service *Service) SubscribeSubagentLive(nodeID string) (<-chan dto.SubagentLiveEvent, func(), error) {
	if service == nil || service.deps.Engine == nil {
		return nil, func() {}, fmt.Errorf("subagent live: engine unavailable")
	}
	if nodeID == "" {
		return nil, func() {}, fmt.Errorf("subagent live: node id required")
	}
	return service.deps.Engine.SubscribeSubagentLive(nodeID)
}
