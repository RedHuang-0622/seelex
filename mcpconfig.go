package main

import (
	"context"
	"fmt"
	"os"

	"github.com/RedHuang-0622/seelex/seelebridge"
	"gopkg.in/yaml.v3"
)

// ── 配置结构 ──────────────────────────────────────────────────

// MCPServerConfig 对应 account-openai.yaml 中 mcp_servers 段的单条配置。
// 与 seelebridge.MCPServer 字段一一映射，但加了 yaml tag。
type MCPServerConfig struct {
	Name      string   `yaml:"name"`
	Transport string   `yaml:"transport"`
	Command   string   `yaml:"command"`
	Args      []string `yaml:"args"`
	Env       []string `yaml:"env"`
	URL       string   `yaml:"url"`
}

// mcpServersWrapper 用于解析 YAML 中的 mcp_servers 列表。
type mcpServersWrapper struct {
	MCPServers []MCPServerConfig `yaml:"mcp_servers"`
}

// ── 加载函数 ───────────────────────────────────────────────────

// loadMCPServersConfig 从账号池 YAML 中读取 mcp_servers 段。
// 完全复用 websearch.go 的加载模式。
func loadMCPServersConfig(accountsPath string) []MCPServerConfig {
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

// ── 注册函数 ──────────────────────────────────────────────────

// registerMCPServers 将 account-openai.yaml 中配置的 MCP 服务器
// 全部附加到 Runtime。跟在 plugin.md 中声明 mcp_servers 等效，
// 但这种方式不需要创建插件目录，适合快速配置通用 MCP 服务。
//
// 注册后的 MCP 工具自动通过 mcpstack 中间件记录调用 trace。
// 如需 CAD 级别的参数预验证，另配 freecad.PreValidate()。
func registerMCPServers(runtime *seelebridge.Runtime, accountsPath string) {
	servers := loadMCPServersConfig(accountsPath)
	if len(servers) == 0 {
		return
	}

	for _, s := range servers {
		ctx := context.Background()
		transport := s.Transport
		if transport == "" {
			if s.Command != "" {
				transport = "stdio"
			} else if s.URL != "" {
				transport = "sse"
			} else {
				fmt.Fprintf(os.Stderr, "⚠ MCP 服务器 %q：transport 未知（command 和 URL 均为空），跳过\n", s.Name)
				continue
			}
		}

		if err := runtime.AttachMCPServer(ctx, s.Name, transport, s.Command, s.Args, s.Env, s.URL); err != nil {
			fmt.Fprintf(os.Stderr, "⚠ 附加 MCP 服务器 %q 失败: %v\n", s.Name, err)
			continue
		}
		// 确保自动注册 mcpstack trace
		// （注：mcpstack 的集成需在 seelebridge.AttachMCP 中自动完成）
		fmt.Fprintf(os.Stderr, "✓ MCP 服务器 %q 已附加\n", s.Name)
	}
}
