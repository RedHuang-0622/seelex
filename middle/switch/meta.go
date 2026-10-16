// Package switching（目录名 `middle/switch`；包名不能叫 `switch`——那是 Go 关键字，
// 与 `middle/view/state` 落成 `view_state` 是同类处理）是中间层"会话切换与多会话运行"的落点（plan §2.4）：
// 当前落进来的是**会话展示元数据**的读写判据（`session_meta.go` 家族），
// 生命周期/切换/队列/驻留调度器家族随 R6 续做搬入（`history/` 已是本包的子域）。
//
// 落点沿革：2026-10-16 随 M5-R6-session-meta **接收者转移**从
// `application/core/session_meta.go` 归位（plan §2.4 的 `session_meta.go` → `middle/switch/`）。
package switching

import (
	"errors"
	"strings"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// MetaStore 是会话展示元数据的读写口（结构满足 `session.SessionMetaPort`，
// 因此宿主无需适配层）。
type MetaStore interface {
	SetSessionMeta(sessionID string, meta model.SessionMeta) error
	SessionMeta(sessionID string) (model.SessionMeta, error)
}

// MetaDeps 是展示元数据用例的宿主面。
type MetaDeps struct {
	// Store 是元数据读写口；nil = 未装配（最小宿主/测试桩），报 ErrMetaUnsupported。
	Store MetaStore
	// Wake 唤醒目录刷新（元数据改变目录排序与标题呈现 ⇒ 走既有目录刷新发布全局
	// `snapshot.changed`，不另发明一条通知路径）。nil = 不唤醒。
	Wake func()
}

// ErrMetaUnsupported 表示装配的会话端口不提供展示元数据存取（例如最小宿主或测试桩）。
// 目录枚举不受影响，只是元数据恒为零值。
var ErrMetaUnsupported = errors.New("session meta storage is not assembled")

// SetMeta 写单个会话的展示元数据，随后唤醒目录刷新让所有客户端重拉。
// meta 全为零值即清除该会话的条目（取消置顶 + 清空别名 + 复位排序）。
func SetMeta(deps MetaDeps, sessionID string, meta model.SessionMeta) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session ID is required")
	}
	if deps.Store == nil {
		return ErrMetaUnsupported
	}
	if err := deps.Store.SetSessionMeta(sessionID, meta); err != nil {
		return err
	}
	if deps.Wake != nil {
		deps.Wake()
	}
	return nil
}

// ReadMeta 读取单个会话的展示元数据（未装配元数据存储时返回 ErrMetaUnsupported）。
func ReadMeta(deps MetaDeps, sessionID string) (model.SessionMeta, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return model.SessionMeta{}, errors.New("session ID is required")
	}
	if deps.Store == nil {
		return model.SessionMeta{}, ErrMetaUnsupported
	}
	return deps.Store.SessionMeta(sessionID)
}
