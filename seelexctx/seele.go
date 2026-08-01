// Package seelexctx — 对 Seele 基础设施能力的持有层。
//
// 这里 re-export Seele 的关键方法，使 seelex 的消费者无需直接 import
// "github.com/RedHuang-0622/Seele/seelectx" 即可使用 token 估算、
// 历史压缩等能力。
package seelexctx

import (
	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/seelectx/ctx_manager"
	"github.com/RedHuang-0622/Seele/types"
)

// ── Token 估算 ──────────────────────────────────────────────────

// EstimateTokens 估算文本的 token 数（保守公式 len/3）。
// 委托给 seelectx.EstimateTokens。
var EstimateTokens = seelectx.EstimateTokens

// ── 历史管理 ────────────────────────────────────────────────────

// NeedCompression 判断历史消息是否需要压缩。
var NeedCompression = seelectx.NeedCompression

// TrimHistory 硬截断消息历史以适应 maxTokens 限制。
var TrimHistory = seelectx.TrimHistory

// ── 类型别名 ────────────────────────────────────────────────────

// DefaultContextConfig 返回推荐的上下文配置。
var DefaultContextConfig = seelectx.DefaultContextConfig

// ── 枚举常量 ────────────────────────────────────────────────────

// 上下文预算的默认阈值。
var (
	DefaultMaxTokens          = ctx_manager.DefaultConfig().MaxTokens
	DefaultMaxToolResultChars = ctx_manager.DefaultConfig().MaxToolResultChars
)

// ── 编译期检查 ──────────────────────────────────────────────────

var (
	_ = types.Message{} // 确保类型可用
)
