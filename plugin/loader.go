package plugin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/RedHuang-0622/seelex/internal/frontmatter"
	"github.com/RedHuang-0622/seelex/skill"
)

const manifestFile = "plugin.md"

var validPluginName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

type manifest struct {
	SchemaVersion int         `yaml:"schema_version"`
	Name          string      `yaml:"name"`
	Description   string      `yaml:"description"`
	Include       []string    `yaml:"include"`
	Exclude       []string    `yaml:"exclude"`
	MCPServers    []MCPServer `yaml:"mcp_servers"`
}

type Loader struct{ roots []string }

func NewLoader(roots ...string) *Loader {
	cleaned := make([]string, 0, len(roots))
	for _, root := range roots {
		if root = strings.TrimSpace(root); root != "" {
			cleaned = append(cleaned, filepath.Clean(root))
		}
	}
	return &Loader{roots: cleaned}
}

func (l *Loader) LoadAll() ([]Plugin, error) {
	seen := make(map[string]Plugin)
	for _, root := range l.roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("plugin load root %s: %w", root, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() || !validPluginName.MatchString(entry.Name()) {
				continue
			}
			if _, exists := seen[entry.Name()]; exists {
				continue
			}
			loaded, err := loadPlugin(root, entry.Name())
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			seen[loaded.Name] = loaded
		}
	}
	result := make([]Plugin, 0, len(seen))
	for _, p := range seen {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (l *Loader) Load(name string) (*Plugin, error) {
	if !validPluginName.MatchString(name) {
		return nil, fmt.Errorf("invalid plugin name %q", name)
	}
	plugins, err := l.LoadAll()
	if err != nil {
		return nil, err
	}
	for i := range plugins {
		if plugins[i].Name == name {
			return &plugins[i], nil
		}
	}
	return nil, fmt.Errorf("plugin %q not found", name)
}

func loadPlugin(root, directoryName string) (Plugin, error) {
	pluginRoot := filepath.Join(root, directoryName)
	path := filepath.Join(pluginRoot, manifestFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return Plugin{}, err
	}
	var meta manifest
	body, err := frontmatter.Parse(data, &meta)
	if err != nil {
		return Plugin{}, fmt.Errorf("plugin %q: %w", directoryName, err)
	}
	if meta.SchemaVersion == 0 {
		meta.SchemaVersion = CurrentSchemaVersion
	}
	if meta.SchemaVersion != CurrentSchemaVersion {
		return Plugin{}, fmt.Errorf("plugin %q: unsupported schema version %d", directoryName, meta.SchemaVersion)
	}
	if meta.Name == "" {
		meta.Name = directoryName
	}
	if meta.Name != directoryName || !validPluginName.MatchString(meta.Name) {
		return Plugin{}, fmt.Errorf("plugin directory %q does not match manifest name %q", directoryName, meta.Name)
	}
	servers, err := resolveMCPServers(pluginRoot, meta.MCPServers)
	if err != nil {
		return Plugin{}, fmt.Errorf("plugin %q: %w", directoryName, err)
	}
	skills, err := skill.LoadPluginDir(pluginRoot)
	if err != nil {
		return Plugin{}, fmt.Errorf("plugin %q skills: %w", directoryName, err)
	}
	description := strings.TrimSpace(meta.Description)
	if description == "" {
		description = firstHeading(body, directoryName)
	}
	return Plugin{
		SchemaVersion: meta.SchemaVersion, Name: meta.Name,
		Description: description, Include: append([]string(nil), meta.Include...),
		Exclude: append([]string(nil), meta.Exclude...), Prompt: strings.TrimSpace(body),
		RootDir: pluginRoot, FilePath: path, MCPServers: servers, Skills: skills,
	}, nil
}

func resolveMCPServers(root string, servers []MCPServer) ([]MCPServer, error) {
	result := make([]MCPServer, 0, len(servers))
	seen := make(map[string]struct{})
	for _, server := range servers {
		server.Name = strings.TrimSpace(server.Name)
		if !validPluginName.MatchString(server.Name) {
			return nil, fmt.Errorf("invalid MCP server name %q", server.Name)
		}
		if _, exists := seen[server.Name]; exists {
			return nil, fmt.Errorf("duplicate MCP server %q", server.Name)
		}
		seen[server.Name] = struct{}{}
		var err error
		server.Command, err = resolveManifestPath(root, server.Command)
		if err != nil {
			return nil, err
		}
		for i, arg := range server.Args {
			server.Args[i], err = resolveManifestPath(root, arg)
			if err != nil {
				return nil, err
			}
		}
		result = append(result, server)
	}
	return result, nil
}

func resolveManifestPath(root, value string) (string, error) {
	if !strings.HasPrefix(value, "./") &&
		!strings.HasPrefix(value, ".\\") &&
		!strings.HasPrefix(value, "../") &&
		!strings.HasPrefix(value, "..\\") {
		return value, nil
	}
	value = strings.ReplaceAll(value, "\\", string(filepath.Separator))
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(rootAbs, filepath.Clean(value)))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("manifest path %q escapes plugin root", value)
	}
	return target, nil
}

func firstHeading(body, fallback string) string {
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(raw), "#"))
		if line != "" {
			return line
		}
	}
	return fallback
}
