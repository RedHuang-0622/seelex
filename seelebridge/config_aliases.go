package seelebridge

import (
	"github.com/RedHuang-0622/seelex/seelebridge/internal/config"
)

// ── 账号配置域兼容别名（config.go 已迁 seelebridge/internal/config）──────

// accountLimits 是账号的上下文/输出预算（internal/config.AccountLimits）。
type accountLimits = config.AccountLimits

// defaultContextWindow / defaultMaxOutputTokens 是未配置时的默认预算
// （internal/config 常量；runtime.go currentAccountLimits 兜底用）。
const (
	defaultContextWindow   = config.DefaultContextWindow
	defaultMaxOutputTokens = config.DefaultMaxOutputTokens
)
