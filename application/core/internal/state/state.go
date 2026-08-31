// Package state is the shared state kernel of application/core.
// 锁、权威 Snapshot、外部端口依赖与事件/审批通道在此集中；core 根门面与
// 各域子包（session_runtime/task_context/...）都以嵌入/注入方式共享它，
// 避免域之间直接持有彼此实现，形成循环依赖。
package state

import (
	"sync"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/model"
)

// New 构造共享状态内核。deps 必须是装配根已补齐默认值后的依赖集合；
// Events/Approval 直接引用 deps 注入的通道，不在此处二次创建。
func New(deps contract.Dependencies) *Core {
	return &Core{
		Deps:         deps,
		Events:       deps.Events,
		Approval:     deps.Approval,
		SessionViews: make(map[string]*SessionView),
	}
}

// Core 是共享状态内核：Mu 保护 Snapshot；Deps 是装配注入的外部端口；
// Events/Approval 是事件发布与异步审批通道（窄接口，可替换实现）。
type Core struct {
	Mu       sync.RWMutex
	Snapshot model.Snapshot
	// SessionViews 是每会话可见投影状态（阶段 1：Snapshot 会话字段收进
	// 每会话 scope，Snapshot 保留当前会话的只读镜像；后台会话也维护自己
	// 的 conversation/chat/readFiles，hot_attach 回看不再为空）。
	// Core.Mu 保护；view_state 域是唯一写方。
	SessionViews map[string]*SessionView
	Deps         contract.Dependencies
	Events       event.Hub
	Approval     contract.ApprovalBroker
}

// SessionView 是单会话的可见投影状态（阶段 1/2 生命周期单元）。
// 与 Snapshot 会话字段一一对应：活跃会话的 SessionView 是 Snapshot 的
// 数据源（bump/切换时镜像）。
type SessionView struct {
	Conversation       []model.Message
	Chat               model.ChatState
	ReadFiles          []model.ReadFileRef
	TotalMessages      int
	HistoryOffset      int
	HasMoreHistory     bool
	ConversationWindow int
}
