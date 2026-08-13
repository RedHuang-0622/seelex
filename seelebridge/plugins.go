package seelebridge

// 插件可见性配置域已迁入 seelebridge/plugin（Manager：定义/激活/停用 +
// include/exclude 过滤，作用于 bridge.WithVisibilityPolicy 输入）。
// 本文件只保留 Runtime 委托。

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
