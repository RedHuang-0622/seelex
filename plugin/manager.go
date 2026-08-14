package plugin

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"

	"github.com/RedHuang-0622/seelex/skill"
)

type ToolBackend interface {
	DefinePlugin(name, description string, include, exclude []string) error
	UndefinePlugin(name string)
	ActivatePlugin(name string) error
	DeactivatePlugin()
	ActivePlugin() string
}

type MCPBackend interface {
	AttachMCPServer(ctx context.Context, name, transport, command string, args, env []string, url string) error
	DetachMCP(name string) error
}

type SkillBackend interface {
	PublishPluginSkills(pluginName string, skills []skill.Skill) error
	ClearPluginSkills(pluginName string)
	ActivatePluginSkills(pluginName string) error
	DeactivatePluginSkills() error
}

// Manager coordinates product plugin definitions with framework primitives.
type Manager struct {
	mu       sync.Mutex
	loader   *Loader
	tools    ToolBackend
	mcp      MCPBackend
	skills   SkillBackend
	plugins  map[string]Plugin
	current  string
	attached map[string][]string
}

func NewManager(loader *Loader, tools ToolBackend, mcp MCPBackend, skills SkillBackend) *Manager {
	return &Manager{
		loader: loader, tools: tools, mcp: mcp, skills: skills,
		plugins: make(map[string]Plugin), attached: make(map[string][]string),
	}
}

func (m *Manager) Load() error {
	plugins, err := m.loader.LoadAll()
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.plugins) != 0 {
		return fmt.Errorf("plugins already loaded")
	}

	loaded := make(map[string]Plugin, len(plugins))
	registered := make([]string, 0, len(plugins))
	rollback := func() {
		for i := len(registered) - 1; i >= 0; i-- {
			name := registered[i]
			m.skills.ClearPluginSkills(name)
			m.tools.UndefinePlugin(name)
		}
	}
	for _, p := range plugins {
		if err := m.tools.DefinePlugin(p.Name, p.Description, p.Include, p.Exclude); err != nil {
			rollback()
			return fmt.Errorf("plugin define %q: %w", p.Name, err)
		}
		registered = append(registered, p.Name)
		if err := m.skills.PublishPluginSkills(p.Name, p.Skills); err != nil {
			rollback()
			return fmt.Errorf("plugin skills %q: %w", p.Name, err)
		}
		loaded[p.Name] = p
	}
	m.plugins = loaded
	return nil
}

func (m *Manager) Activate(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	target, ok := m.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}
	if m.current == name {
		return nil
	}
	previous := m.plugins[m.current]

	// Runtime MCP names are plugin-qualified, so the target can be prepared
	// while the previous plugin remains fully usable. This gives rollback a
	// stable state instead of tearing down the old plugin first.
	if err := m.attachLocked(ctx, target); err != nil {
		return err
	}
	if err := m.tools.ActivatePlugin(name); err != nil {
		cleanupErr := m.detachLocked(ctx, name)
		m.restoreToolPluginLocked(previous)
		return errors.Join(fmt.Errorf("plugin activate tools %q: %w", name, err), cleanupErr)
	}
	if err := m.skills.ActivatePluginSkills(name); err != nil {
		cleanupErr := m.detachLocked(ctx, name)
		m.restoreToolPluginLocked(previous)
		return errors.Join(fmt.Errorf("plugin activate skills %q: %w", name, err), cleanupErr)
	}
	if err := m.detachLocked(ctx, m.current); err != nil {
		m.restoreToolPluginLocked(previous)
		cleanupErr := m.detachLocked(ctx, name)
		return errors.Join(err, cleanupErr)
	}
	m.current = name
	return nil
}

func (m *Manager) Deactivate(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	previous := m.plugins[m.current]
	if err := m.detachLocked(ctx, m.current); err != nil {
		return err
	}
	m.tools.DeactivatePlugin()
	if err := m.skills.DeactivatePluginSkills(); err != nil {
		restoreErr := m.attachLocked(ctx, previous)
		m.restoreToolPluginLocked(previous)
		return errors.Join(fmt.Errorf("plugin deactivate skills: %w", err), restoreErr)
	}
	m.current = ""
	return nil
}

func (m *Manager) Current() (Plugin, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.plugins[m.current]
	return p, ok
}

func (m *Manager) All() []Plugin {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]Plugin, 0, len(m.plugins))
	for _, p := range m.plugins {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// ReloadReport 是一次 plugins_reload 的差异摘要（自迭代审计面）。
type ReloadReport struct {
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
	Updated []string `json:"updated"`
	Active  string   `json:"active"`
}

// Reload 重新扫描磁盘插件目录并与当前状态 diff 后**事务式**应用：
//   - 新增 → DefinePlugin + PublishPluginSkills（不激活，保持现状语义）；
//   - 删除 → 若为 active 先 Deactivate，再 Undefine + ClearPluginSkills；
//   - 修改 → 若为 active：Deactivate → 替换 → 重新 Activate；否则直接替换；
//
// 任一步失败按快照回滚到上一个可用状态。
func (m *Manager) Reload(ctx context.Context) (ReloadReport, error) {
	if m == nil || m.loader == nil {
		return ReloadReport{}, fmt.Errorf("plugins_reload: loader is not configured")
	}
	next, err := m.loader.LoadAll()
	if err != nil {
		return ReloadReport{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	previous := make(map[string]Plugin, len(m.plugins))
	for name, p := range m.plugins {
		previous[name] = p
	}
	previousCurrent := m.current
	previousAttached := make(map[string][]string, len(m.attached))
	for name, servers := range m.attached {
		previousAttached[name] = append([]string(nil), servers...)
	}

	nextMap := make(map[string]Plugin, len(next))
	for index := range next {
		nextMap[next[index].Name] = next[index]
	}
	report := ReloadReport{}
	rollback := func(cause error) (ReloadReport, error) {
		// best-effort 恢复快照：先拆当前态，再重建旧定义并恢复旧 active。
		if m.current != "" {
			_ = m.detachLocked(ctx, m.current)
			m.tools.DeactivatePlugin()
			_ = m.skills.DeactivatePluginSkills()
		}
		m.plugins = previous
		m.current = previousCurrent
		m.attached = previousAttached
		for name, p := range previous {
			_ = m.tools.DefinePlugin(name, p.Description, p.Include, p.Exclude)
			_ = m.skills.PublishPluginSkills(name, p.Skills)
		}
		if previousCurrent != "" {
			if p, ok := previous[previousCurrent]; ok {
				_ = m.attachLocked(ctx, p)
				_ = m.tools.ActivatePlugin(previousCurrent)
				_ = m.skills.ActivatePluginSkills(previousCurrent)
			}
		}
		return report, fmt.Errorf("plugins_reload: %w", cause)
	}

	// 1) 删除
	for name := range m.plugins {
		if _, ok := nextMap[name]; ok {
			continue
		}
		report.Removed = append(report.Removed, name)
		if m.current == name {
			if err := m.detachLocked(ctx, name); err != nil {
				return rollback(err)
			}
			m.tools.DeactivatePlugin()
			_ = m.skills.DeactivatePluginSkills()
			m.current = ""
		}
		m.skills.ClearPluginSkills(name)
		m.tools.UndefinePlugin(name)
		delete(m.plugins, name)
		delete(m.attached, name)
	}

	// 2) 新增 / 修改（保持 next 的稳定顺序）
	for _, p := range next {
		old, existed := m.plugins[p.Name]
		if existed && reflect.DeepEqual(old, p) {
			continue
		}
		wasActive := m.current == p.Name
		if existed {
			report.Updated = append(report.Updated, p.Name)
			if wasActive {
				if err := m.detachLocked(ctx, p.Name); err != nil {
					return rollback(err)
				}
				m.tools.DeactivatePlugin()
				_ = m.skills.DeactivatePluginSkills()
				m.current = ""
			}
			m.skills.ClearPluginSkills(p.Name)
			m.tools.UndefinePlugin(p.Name)
		} else {
			report.Added = append(report.Added, p.Name)
		}
		if err := m.tools.DefinePlugin(p.Name, p.Description, p.Include, p.Exclude); err != nil {
			return rollback(err)
		}
		if err := m.skills.PublishPluginSkills(p.Name, p.Skills); err != nil {
			return rollback(err)
		}
		m.plugins[p.Name] = p
		if wasActive {
			if err := m.attachLocked(ctx, p); err != nil {
				return rollback(err)
			}
			if err := m.tools.ActivatePlugin(p.Name); err != nil {
				return rollback(err)
			}
			if err := m.skills.ActivatePluginSkills(p.Name); err != nil {
				return rollback(err)
			}
			m.current = p.Name
		}
	}
	report.Active = m.current
	return report, nil
}

func (m *Manager) attachLocked(ctx context.Context, p Plugin) error {
	if p.Name == "" {
		return nil
	}
	attached := make([]string, 0, len(p.MCPServers))
	for _, server := range p.MCPServers {
		runtimeName := p.Name + "__" + server.Name
		if err := m.attachServerLocked(ctx, p, server); err != nil {
			var cleanupErr error
			for i := len(attached) - 1; i >= 0; i-- {
				cleanupErr = errors.Join(cleanupErr, m.mcp.DetachMCP(attached[i]))
			}
			return errors.Join(
				fmt.Errorf("plugin %q attach MCP %q: %w", p.Name, server.Name, err),
				cleanupErr,
			)
		}
		attached = append(attached, runtimeName)
	}
	m.attached[p.Name] = attached
	return nil
}

func (m *Manager) attachServerLocked(ctx context.Context, p Plugin, server MCPServer) error {
	runtimeName := p.Name + "__" + server.Name
	return m.mcp.AttachMCPServer(
		ctx, runtimeName, server.Transport, server.Command,
		server.Args, server.Env, server.URL,
	)
}

func (m *Manager) detachLocked(ctx context.Context, pluginName string) error {
	if pluginName == "" {
		return nil
	}
	names := m.attached[pluginName]
	detached := make([]string, 0, len(names))
	for i := len(names) - 1; i >= 0; i-- {
		if err := m.mcp.DetachMCP(names[i]); err != nil {
			var restoreErr error
			p := m.plugins[pluginName]
			for j := len(detached) - 1; j >= 0; j-- {
				server, ok := serverByRuntimeName(p, detached[j])
				if !ok {
					continue
				}
				restoreErr = errors.Join(restoreErr, m.attachServerLocked(ctx, p, server))
			}
			return errors.Join(
				fmt.Errorf("plugin %q detach MCP %q: %w", pluginName, names[i], err),
				restoreErr,
			)
		}
		detached = append(detached, names[i])
	}
	delete(m.attached, pluginName)
	return nil
}

func serverByRuntimeName(p Plugin, runtimeName string) (MCPServer, bool) {
	for _, server := range p.MCPServers {
		if p.Name+"__"+server.Name == runtimeName {
			return server, true
		}
	}
	return MCPServer{}, false
}

func (m *Manager) restoreToolPluginLocked(previous Plugin) {
	if previous.Name == "" {
		m.tools.DeactivatePlugin()
		_ = m.skills.DeactivatePluginSkills()
		return
	}
	_ = m.tools.ActivatePlugin(previous.Name)
	_ = m.skills.ActivatePluginSkills(previous.Name)
}
