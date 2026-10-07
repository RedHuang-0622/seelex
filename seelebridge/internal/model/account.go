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

// DefaultTemperature 是账号未显式配置 temperature 时的采样温度。
//
// 口径（2026-10，据 DeepSeek 官方文档）：
//
//   - **temperature 只对"非思考模式"有意义**。deepseek-flash 默认开启思考模式，
//     而思考模式明确不支持 temperature / presence_penalty / frequency_penalty
//     ——官方原文：设置这些参数不会报错，但也不会有任何效果。此时真正决定
//     "想多深"的是 reasoning_effort。
//   - 官方按用途给的推荐值（非思考模式）：编码/数学 0.0、数据分析 1.0、
//     通用对话 1.3、翻译 1.3、创作/诗歌 1.5；不带就是服务端默认 1.0。
//
// 因此这里取 0.3：偏工程/确定性一侧，服务于需要 temperature 的非思考模型与
// 其它 provider。要按官方口径收得更紧，改这一行或在 accounts.yaml 里用
// defaults.temperature / 账号级 temperature 覆盖（每个账号可独立设）。
const DefaultTemperature = 0.3

// WireReasoningEffort 把账号配置里的思考强度翻成"能不能直接上线"的值。
//
// `session`（agent 角色的默认）意味着**跟随会话 effort 档位**：谁在构造客户端时
// 都还不知道用户选了哪一档，所以返回空串——空值不上线，等运行时按当前档位补上
// （ChatClient.SetReasoningEffort）。空值与 `session` 在这里是同一个结果。
//
// 空值时 provider 走自己的默认，我们**绝不猜一个值**填进去。
//
// 放在 model 而不是各个装配点：账号池（seelebridge/account）与规划预检
// （seelebridge/plan）都要构造 ChatClient，两处必须同一条口径——否则 "session"
// 会从其中一处原样漏到 wire 上，被 provider 判成非法值。
func WireReasoningEffort(configured string) string {
	if configured == ReasoningEffortSession {
		return ""
	}
	return configured
}

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
	Temperature     float64
	ReasoningEffort string
	Role            AccountRole
}

// ReasoningEffortSession 与角色默认思考强度统一登记在 application/contract/dto
// （跨层契约：seelebridge 与 application 都要用，而 application 依赖 seelebridge，
// 反向引用成环，故落在两边都引的叶子包）。这里按本包既有习惯（AccountRole 也是
// 这样）再导出一次，免得调用方同时引两个包。
const ReasoningEffortSession = dto.ReasoningEffortSession

// DefaultReasoningEffortForRole 见 dto.DefaultReasoningEffortForRole。
func DefaultReasoningEffortForRole(role AccountRole) string {
	return dto.DefaultReasoningEffortForRole(role)
}

// ValidReasoningEffort 见 dto.ValidReasoningEffort。
func ValidReasoningEffort(value string) bool { return dto.ValidReasoningEffort(value) }

// ReasoningEffortVocabulary 见 dto.ReasoningEffortVocabulary。
func ReasoningEffortVocabulary() []string { return dto.ReasoningEffortVocabulary() }

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
