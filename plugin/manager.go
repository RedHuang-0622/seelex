package plugin

import (
	"context"
	"errors"
	"fmt"
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
