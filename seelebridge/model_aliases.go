package seelebridge

import (
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// ── 账号域共享类型别名（internal/model 下沉，根包保持既有符号名）──────

// accountSpec 是账号的非敏感配置（凭据在 APIKey 内，仅用于组装客户端）。
type accountSpec = model.AccountSpec

// AccountRole represents the task category an account is used for.
type AccountRole = model.AccountRole

const (
	RoleAgent    = model.RoleAgent
	RoleSubAgent = model.RoleSubAgent
	RoleGoalPlan = model.RoleGoalPlan
)

// ResolveAccountSpec picks an account spec from the loaded config for the
// given role. Roles fall back to the primary agent role when they are not
// configured, preserving single-account installations.
func ResolveAccountSpec(specs []accountSpec, role AccountRole) (accountSpec, error) {
	return model.ResolveAccountSpec(specs, role)
}

// fallbackRoles 返回 role 的回退角色（未配置时回退主 agent 角色）。
func fallbackRoles(role AccountRole) []AccountRole { return model.FallbackRoles(role) }
