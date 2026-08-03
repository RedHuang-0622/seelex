package seelebridge

import "fmt"

// pluginDef 是插件可见性配置（include/exclude 快照）。
// 新装配模型下插件不再控制 holder，而是作为 bridge.WithVisibilityPolicy
// 的输入：激活插件时按 include/exclude 过滤每次请求的可见工具集。
type pluginDef struct {
	Name        string
	Description string
	Include     []string
	Exclude     []string
}

// DefinePlugin 定义或替换一个插件的可见性快照。
func (r *Runtime) DefinePlugin(name, description string, include, exclude []string) error {
	if name == "" {
		return fmt.Errorf("seelebridge: plugin name is empty")
	}
	r.pluginMu.Lock()
	r.pluginDefs[name] = pluginDef{
		Name: name, Description: description,
		Include: append([]string(nil), include...),
		Exclude: append([]string(nil), exclude...),
	}
	r.pluginMu.Unlock()
	return nil
}

func (r *Runtime) UndefinePlugin(name string) {
	r.pluginMu.Lock()
	delete(r.pluginDefs, name)
	if r.activePlugin == name {
		r.activePlugin = ""
	}
	r.pluginMu.Unlock()
}

func (r *Runtime) ActivatePlugin(name string) error {
	r.pluginMu.Lock()
	defer r.pluginMu.Unlock()
	if _, ok := r.pluginDefs[name]; !ok {
		return fmt.Errorf("seelebridge: plugin %q is not defined", name)
	}
	r.activePlugin = name
	return nil
}

func (r *Runtime) DeactivatePlugin() {
	r.pluginMu.Lock()
	r.activePlugin = ""
	r.pluginMu.Unlock()
}

func (r *Runtime) ActivePlugin() string {
	r.pluginMu.RLock()
	defer r.pluginMu.RUnlock()
	return r.activePlugin
}
