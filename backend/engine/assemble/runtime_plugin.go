package assemble

// 这是插件域的宿主转发面（R11-S3，契约 §2.3）：插件生命周期在
// 域包 `backend/engine/plugin`（`r.plugins`），`WithPluginAssembly` 与 teammate 回合用同一个
// 原语（`toolreg.WithRolePlugins`）。内容从 `ports.go` 原样搬来，行为零变化。

import (
	"context"

	toolreg "github.com/RedHuang-0622/seelex/backend/tool/registry"
)

// ── plugin 端口 ───────────────────────────────────────────────────

// DefinePlugin 定义或替换一个插件的可见性快照。
func (r *Runtime) DefinePlugin(name, description string, include, exclude []string) error {
	if r == nil || r.plugins == nil {
		return nil
	}
	return r.plugins.Define(name, description, include, exclude)
}

// UndefinePlugin 删除插件定义；若其为当前激活插件则一并停用。
func (r *Runtime) UndefinePlugin(name string) {
	if r == nil || r.plugins == nil {
		return
	}
	r.plugins.Undefine(name)
}

// ActivatePlugin 激活插件（未定义返回显式错误）。
func (r *Runtime) ActivatePlugin(name string) error {
	if r == nil || r.plugins == nil {
		return nil
	}
	return r.plugins.Activate(name)
}

// DeactivatePlugin 停用当前插件。
func (r *Runtime) DeactivatePlugin() {
	if r == nil || r.plugins == nil {
		return
	}
	r.plugins.Deactivate()
}

// ActivePlugin 返回当前激活插件名。
func (r *Runtime) ActivePlugin() string {
	if r == nil || r.plugins == nil {
		return ""
	}
	return r.plugins.Active()
}

// SetPluginUnassembledReason 注入"未定义插件名怎么判 / 怎么说"的判决函数（产品侧在
// 启动期把**精选目录**的读数包成它；nil = 取消注入，回落既有"未定义"口径）。
//
// 它是插件域的端口（与 DefinePlugin 同族），不是插件面收窄逻辑：判决只影响
// "这个名字收不收"与拒绝文案，不改变任何已装插件的 include/exclude。
func (r *Runtime) SetPluginUnassembledReason(fn func(name string, installed []string) error) {
	if r == nil || r.plugins == nil {
		return
	}
	r.plugins.SetUnassembledReason(fn)
}

// WithPluginAssembly 返回把"这一轮装配哪些插件"带进执行 ctx 的派生 ctx。
//
// 与 teammate 回合用的是**同一个原语**（toolreg.WithRolePlugins）：工具可见性
// 每轮从 ctx 现算，不在任何句柄上缓存"当前装配"——缓存在并发回合里会被后写的那份
// 覆盖。空集合原样返回 ctx（= 不覆盖，继承宿主当前激活插件）。
//
// 边界（与 runtime_role_plugins.go 同一口径）：装配只回答"能用哪些能力包"，
// **不放宽权限面**（权限面先算完，插件面只在最后做减法）；root/主代理的**正文层**
// 仍来自宿主当前激活插件——ctx 装配管的是这一轮的工具面与技能目录。
func (r *Runtime) WithPluginAssembly(ctx context.Context, plugins []string) context.Context {
	if ctx == nil || r == nil || len(plugins) == 0 {
		return ctx
	}
	return toolreg.WithRolePlugins(ctx, plugins)
}
