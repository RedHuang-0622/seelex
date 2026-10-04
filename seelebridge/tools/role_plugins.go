package tools

import (
	"context"
	"strings"
)

// role_plugins.go — 「按会话装配的插件集合」在 ctx 上的载体（能力轴）。
//
// 与 WithEmployeeGrant 同一姿势（见 runtime_role_turn.go 的"按构造授权"说明）：
// 装配面在**起手那一刻**把这一轮的插件集合放进 ctx，工具可见性与技能目录都按它
// 现算——不靠反查角色注册表，也不在 Policy 上缓存"当前装配"（缓存就是并发丢失
// 更新的另一面：两个 teammate 并发回合时，后写的那份会覆盖先跑的那份）。
//
// 它**不是权限**：权限面按主体（emp_ro/emp_rw/root）算，插件只收窄插件面，两者
// 在 Policy.Filter 里相交（见 PolicyDeps.PluginFace）。空集 = 不覆盖：调用方不
// 装配时闭包直接早退到宿主的插件过滤（与"插件缺失 = 今天的行为"逐字一致）。

// rolePluginsKey 携带"本轮按会话装配的插件集合"。
type rolePluginsKey struct{}

// WithRolePlugins 把按会话装配的插件名清单放进 ctx：去首尾空白、丢弃空项、保序
// 去重；清完为空（或输入为空）时原样返回 ctx（不塞一个空集合进 ctx——"没装配"与
// "装配了零个"在读取侧必须同一口径，见契约 3 的空集语义）。
//
// 这里的去重是**传输侧的规范化**，不是"声明侧的口径"：声明侧（team_plan 的成员条目 /
// RoleSpec.Plugins）的重复在入口就被**显式拒绝**了（见 dto.NormalizePlugins），所以走到
// 本函数的都是已经过校验的事实——它不会把 leader 写的重复名单悄悄合并掉（那种输入根本
// 到不了这里）。
func WithRolePlugins(ctx context.Context, names []string) context.Context {
	if ctx == nil || len(names) == 0 {
		return ctx
	}
	cleaned := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}
		cleaned = append(cleaned, name)
	}
	if len(cleaned) == 0 {
		return ctx
	}
	return context.WithValue(ctx, rolePluginsKey{}, cleaned)
}

// RolePluginsFromContext 取本轮装配的插件集合；没装配（含空集、含 ctx 为 nil）
// 时返回 nil。调用方据此早退到宿主面。
func RolePluginsFromContext(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	names, _ := ctx.Value(rolePluginsKey{}).([]string)
	return names
}
