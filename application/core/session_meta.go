package core

// 会话展示元数据（置顶 / 别名 / 手动排序位）。它们描述"用户怎么看这个会话"，
// 不参与执行、也不影响存储归属，因此持久化在存储层的项目级 meta blob 里，
// 随会话目录一起下发（客户端不再各自保存在 localStorage，避免跨窗口分叉）。

import (
	"errors"
	"strings"

	"github.com/RedHuang-0622/seelex/session"
)

// ErrSessionMetaUnsupported 表示装配的会话端口不提供展示元数据存取（例如最小
// 宿主或测试桩）。目录枚举不受影响，只是元数据恒为零值。
var ErrSessionMetaUnsupported = errors.New("session meta storage is not assembled")

// metaPort 返回会话端口的可选展示元数据扩展。
func (service *Service) metaPort() (session.SessionMetaPort, bool) {
	port, ok := service.Deps.Sessions.(session.SessionMetaPort)
	return port, ok
}

// SetSessionMeta 写单个会话的展示元数据，随后唤醒目录刷新让所有客户端重拉。
// meta 全为零值即清除该会话的条目（取消置顶 + 清空别名 + 复位排序）。
func (service *Service) SetSessionMeta(sessionID string, meta SessionMeta) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session ID is required")
	}
	port, ok := service.metaPort()
	if !ok {
		return ErrSessionMetaUnsupported
	}
	if err := port.SetSessionMeta(sessionID, meta); err != nil {
		return err
	}
	// 元数据改变目录排序与标题呈现：走既有的目录刷新（发布全局
	// snapshot.changed），不在这里另发明一条通知路径。
	service.components.sessions.RequestCatalogRefresh()
	return nil
}

// SessionMeta 读取单个会话的展示元数据（未装配元数据存储时返回
// ErrSessionMetaUnsupported）。
func (service *Service) SessionMeta(sessionID string) (SessionMeta, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return SessionMeta{}, errors.New("session ID is required")
	}
	port, ok := service.metaPort()
	if !ok {
		return SessionMeta{}, ErrSessionMetaUnsupported
	}
	return port.SessionMeta(sessionID)
}
