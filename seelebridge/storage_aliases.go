package seelebridge

import (
	"github.com/RedHuang-0622/seelex/seelebridge/internal/storage"
)

// ── 会话存储域兼容别名（storage.go 已迁 seelebridge/internal/storage）──────
// application/adapters 等外部调用面继续经 seelebridge.* 使用，API 不变。

type Message = storage.Message
type SessionMeta = storage.SessionMeta
type SessionStore = storage.SessionStore
type NestedSessionStore = storage.NestedSessionStore

// NewSessionStore / NewNestedSessionStore 是构造器函数值别名。
var (
	NewSessionStore       = storage.NewSessionStore
	NewNestedSessionStore = storage.NewNestedSessionStore
)
