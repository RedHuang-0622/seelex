// Package plugin 承载 Runtime 的插件可见性执行面：插件不再控制 holder，
// 而是作为 bridge.WithVisibilityPolicy 的输入——激活插件时按
// include/exclude 过滤每次请求的可见工具集。域内不依赖 seelebridge 根包。
//
// 边界（解耦方案 §02.4 方案 A 轻量版）：本包只是可见性投影缓存，不是
// 插件定义的事实源——事实源在顶层 plugin.Manager（manifest/skills/MCP
// 全量契约），本包 defs 由 root 经 ToolBackend 单点推送（Define/Undefine/
// Activate），写路径只有 root 一个入口；多插件叠加属产品决策，当前为单选。
package plugin

import (
	"fmt"
	"path"
	"sync"

	"github.com/RedHuang-0622/Seele/types"
)

// Def 是插件可见性配置（include/exclude 快照）。
type Def struct {
	Name        string
	Description string
	Include     []string
	Exclude     []string
}

// Manager 是插件可见性状态的 actor 资源（自带锁）。defs 是 root
// plugin.Manager 定义的投影缓存，本包不解释 manifest/skills/MCP。
type Manager struct {
	mu     sync.RWMutex
	defs   map[string]Def
	active string
}

// NewManager 构造插件可见性管理器。
func NewManager() *Manager {
	return &Manager{defs: make(map[string]Def)}
}

// Define 定义或替换一个插件的可见性快照。
func (m *Manager) Define(name, description string, include, exclude []string) error {
	if name == "" {
		return fmt.Errorf("seelebridge: plugin name is empty")
	}
	m.mu.Lock()
	m.defs[name] = Def{
		Name: name, Description: description,
		Include: append([]string(nil), include...),
		Exclude: append([]string(nil), exclude...),
	}
	m.mu.Unlock()
	return nil
}

// Undefine 删除插件定义；若其为当前激活插件则一并停用。
func (m *Manager) Undefine(name string) {
	m.mu.Lock()
	delete(m.defs, name)
	if m.active == name {
		m.active = ""
	}
	m.mu.Unlock()
}

// Activate 激活插件（未定义返回显式错误）。
func (m *Manager) Activate(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.defs[name]; !ok {
		return fmt.Errorf("seelebridge: plugin %q is not defined", name)
	}
	m.active = name
	return nil
}

// Deactivate 停用当前插件。
func (m *Manager) Deactivate() {
	m.mu.Lock()
	m.active = ""
	m.mu.Unlock()
}

// Active 返回当前激活插件名。
func (m *Manager) Active() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active
}

// Filter 按激活插件的 include/exclude 过滤可见工具集；未激活/未知插件
// 原样返回（插件过滤不生效）。
func (m *Manager) Filter(tools []types.Tool) []types.Tool {
	m.mu.RLock()
	active := m.active
	def, ok := m.defs[active]
	m.mu.RUnlock()
	if !ok || active == "" {
		return tools
	}
	filtered := make([]types.Tool, 0, len(tools))
	for _, tool := range tools {
		name := tool.Function.Name
		if len(def.Include) > 0 && !matchesAnyPattern(name, def.Include) {
			continue
		}
		if matchesAnyPattern(name, def.Exclude) {
			continue
		}
		filtered = append(filtered, tool)
	}
	return filtered
}

func matchesAnyPattern(name string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchToolPattern(pattern, name) {
			return true
		}
	}
	return false
}

// matchToolPattern 支持 "*" 通配的简单模式匹配（与旧 holder 插件过滤语义一致）。
func matchToolPattern(pattern, name string) bool {
	if pattern == "" {
		return name == ""
	}
	ok, err := path.Match(pattern, name)
	if err != nil {
		return pattern == name
	}
	return ok
}
