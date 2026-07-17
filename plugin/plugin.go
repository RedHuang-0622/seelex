// Package plugin loads and activates Seelex product plugins.
package plugin

import "github.com/RedHuang-0622/seelex/skill"

const CurrentSchemaVersion = 1

// MCPServer describes one framework MCP connection owned by a plugin.
type MCPServer struct {
	Name      string   `yaml:"name"`
	Transport string   `yaml:"transport"`
	Command   string   `yaml:"command"`
	Args      []string `yaml:"args"`
	Env       []string `yaml:"env"`
	URL       string   `yaml:"url"`
}

// Plugin is the parsed application-level plugin definition.
type Plugin struct {
	SchemaVersion int
	Name          string
	Description   string
	Include       []string
	Exclude       []string
	Prompt        string
	RootDir       string
	FilePath      string
	MCPServers    []MCPServer
	Skills        []skill.Skill
}
