package core

import (
	"context"

	"github.com/RedHuang-0622/seelex/application/core/view_state"
)

// Service 门面快照/订阅/投影/消息写入委托（实现位于 view_state 域包）。

func (service *Service) Snapshot() Snapshot {
	return service.components.view.SnapshotView()
}

func (service *Service) Subscribe(buffer int) Subscription {
	return service.components.view.Subscribe(buffer)
}

func (service *Service) collectRuntimeProjection(ctx context.Context) view_state.RuntimeStateProjection {
	return service.components.view.CollectRuntimeProjection(ctx)
}

func (service *Service) applyRuntimeProjectionLocked(projection view_state.RuntimeStateProjection) {
	service.components.view.ApplyRuntimeProjectionLocked(projection)
}

func (service *Service) appendMessageLocked(role, content string, tool *ToolCall) *Message {
	return service.components.view.AppendMessageLocked(role, content, tool)
}

func (service *Service) bumpLocked() uint64 {
	return service.components.view.BumpLocked()
}

func (service *Service) addNotice(notice string) {
	service.components.view.AddNotice(notice)
}

// AddNotice 追加一条系统通知（以 system 消息进入可见会话并发布
// message.added 事件）。启动期配置警告等非致命错误用它呈现给前端。
func (service *Service) AddNotice(notice string) {
	service.addNotice(notice)
}

func (service *Service) resetConversation(notice string) {
	service.components.view.ResetConversation(notice)
}

// advanceMessageSeqLocked 按既有消息 ID 推进消息序列（恢复路径委托）。
func (service *Service) advanceMessageSeqLocked(messages []Message) {
	service.components.view.AdvanceMessageSeqLocked(messages)
}
