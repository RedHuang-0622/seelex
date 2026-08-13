// Package model 承载 seelebridge 各域共享的纯类型（无运行时依赖），
// 供根包 facade 与 plan/worktree/task/session 等子包共同引用。
package model

import (
	"fmt"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// AccountRole represents the task category an account is used for
// （类型本体下沉 application/contract/dto）。
type AccountRole = dto.AccountRole

const (
	RoleAgent    = dto.RoleAgent
	RoleSubAgent = dto.RoleSubAgent
	RoleGoalPlan = dto.RoleGoalPlan
)

// AccountSpec 是账号的非敏感配置（凭据在 APIKey 内，仅用于组装客户端）。
type AccountSpec struct {
	Name            string
	Provider        string
	BaseURL         string
	APIKey          string
	Model           string
	MaxTokens       int
	ContextWindow   int
	MaxOutputTokens int
	MaxConcurrency  int
	Role            AccountRole
}

// FallbackRoles 返回 role 的回退角色（未配置时回退主 agent 角色，
// 保留单账号安装的可用性）。
func FallbackRoles(role AccountRole) []AccountRole {
	switch role {
	case RoleGoalPlan, RoleSubAgent:
		return []AccountRole{RoleAgent}
	default:
		return nil
	}
}

// AccountRoleFromName 从账号 ID 推断角色（按角色前缀匹配，回退 agent）。
func AccountRoleFromName(name string) AccountRole {
	for _, role := range []AccountRole{RoleAgent, RoleSubAgent, RoleGoalPlan} {
		if len(name) > len(role) && name[:len(role)] == string(role) {
			return role
		}
	}
	return RoleAgent
}

// ResolveAccountSpec picks an account spec from the loaded config for the
// given role. Roles fall back to the primary agent role when they are not
// configured, preserving single-account installations.
func ResolveAccountSpec(specs []AccountSpec, role AccountRole) (AccountSpec, error) {
	roles := append([]AccountRole{role}, FallbackRoles(role)...)
	for _, candidate := range roles {
		for _, spec := range specs {
			if spec.Role == candidate && spec.APIKey != "" {
				return spec, nil
			}
		}
	}
	for _, spec := range specs {
		if spec.APIKey != "" {
			return spec, nil
		}
	}
	return AccountSpec{}, fmt.Errorf("seelebridge: no accounts available")
}
