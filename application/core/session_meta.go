package core

// session_meta.go — 会话展示元数据的**宿主适配 + 门面转发**。
//
// 判据（会话 ID 校验、未装配元数据存储的错误、写后唤醒目录刷新）已在
// M5-R6-session-meta 随接收者转移归位 `middle/switch`（plan §2.4 的 `session_meta.go` → `middle/switch/`）；
// 本文件只剩"把 `Deps.Sessions` 上的可选元数据能力断言出来"这条装配缝，以及两个一行转发。
//
// 会话展示元数据（置顶 / 别名 / 手动排序位）描述"用户怎么看这个会话"，不参与执行、也不影响存储归属，
// 因此持久化在存储层的项目级 meta blob 里，随会话目录一起下发（客户端不再各自保存在 localStorage，
// 避免跨窗口分叉）。

import (
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/middle/switch"
	"github.com/RedHuang-0622/seelex/middle/switch/sessionusecase"
)

// metaDeps 装配展示元数据用例的宿主面（每次调用构造一次值语义的 Deps；无状态）。
func (service *Service) metaDeps() switching.MetaDeps {
	if service == nil {
		return switching.MetaDeps{}
	}
	return switching.MetaDeps{
		Store: service.metaPort(),
		Wake:  service.wakeCatalogRefresh,
	}
}

// metaPort 返回会话端口的可选展示元数据扩展（未装配 = 元数据恒为零值，不算失败）。
func (service *Service) metaPort() session.SessionMetaPort {
	if service == nil || service.Deps.Sessions == nil {
		return nil
	}
	port, ok := service.Deps.Sessions.(session.SessionMetaPort)
	if !ok {
		return nil
	}
	return port
}

// wakeCatalogRefresh 唤醒会话目录 worker 重拉（元数据/标题变化后让所有客户端看到新排序）。
func (service *Service) wakeCatalogRefresh() {
	if service == nil || service.components.sessions == nil {
		return
	}
	service.components.sessions.RequestCatalogRefresh()
}

// SetSessionMeta 写单个会话的展示元数据，随后唤醒目录刷新让所有客户端重拉。
// meta 全为零值即清除该会话的条目（取消置顶 + 清空别名 + 复位排序）。判据在 `middle/switch`。
func (service *Service) SetSessionMeta(sessionID string, meta model.SessionMeta) error {
	return switching.SetMeta(service.metaDeps(), sessionID, meta)
}

// SessionMeta 读取单个会话的展示元数据（未装配元数据存储时返回 switch.ErrMetaUnsupported）。
func (service *Service) SessionMeta(sessionID string) (model.SessionMeta, error) {
	return switching.ReadMeta(service.metaDeps(), sessionID)
}
