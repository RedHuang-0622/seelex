// Package config 提供账号池 YAML 中 mcp_servers 段的加载与校验。
// 只负责配置解析，不依赖运行时；注册由调用方（composition root）完成，
// 以保持 mcpstack → seelebridge 单向依赖。
package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// MCPServerConfig 对应 account-openai.yaml 中 mcp_servers 段的单条配置。
// 与 seelebridge.MCPServer 字段一一映射，但加了 yaml tag。
type MCPServerConfig struct {
	Name      string   `yaml:"name"`
	Transport string   `yaml:"transport"`
	Command   string   `yaml:"command"`
	Args      []string `yaml:"args"`
	Env       []string `yaml:"env"`
	URL       string   `yaml:"url"`
	// ToolNotes 是宿主侧追加到该 server 每个工具描述末尾的「使用须知」
	// （模型可见）。MCP 工具的描述本来完全由 server 自己给出，而「在本机
	// 实测出来的调用纪律」——哪种入参形态能拿回结果、失败长什么样——只有
	// 宿主知道。它写在工具定义里，模型在第一次调用之前就看得见。
	ToolNotes []string `yaml:"tool_notes"`
}

// mcpServersWrapper 用于解析 YAML 中的 mcp_servers 列表。
type mcpServersWrapper struct {
	MCPServers []MCPServerConfig `yaml:"mcp_servers"`
}

// Load 从账号池 YAML 中读取 mcp_servers 段。
func Load(accountsPath string) []MCPServerConfig {
	b, err := os.ReadFile(accountsPath)
	if err != nil {
		return nil
	}
	var wrapper mcpServersWrapper
	if err := yaml.Unmarshal(b, &wrapper); err != nil {
		return nil
	}
	// 过滤掉空 name 的无效配置
	valid := make([]MCPServerConfig, 0, len(wrapper.MCPServers))
	for _, s := range wrapper.MCPServers {
		if s.Name != "" {
			valid = append(valid, s)
		}
	}
	return valid
}
