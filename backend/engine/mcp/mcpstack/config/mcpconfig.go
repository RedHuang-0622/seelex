// Package config 提供独立 MCP 配置文件（mcp.yaml）的加载与校验。
// 只负责配置解析，不依赖运行时；注册由调用方（composition root）完成，
// 以保持 mcpstack → seelebridge 单向依赖。
package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// FileName 是 MCP 服务器清单的独立文件名（2026-10 从 accounts.yaml 的
// mcp_servers 段拆出来）。
//
// 为什么单独一份：MCP 是**工具接线**（拉起哪个 server、怎么跟它说话），与
// "用哪个模型账号/哪把密钥"无关。混在账号档里让两件事绑死——接一个新 MCP
// 要动凭据文件，而 accounts.yaml 因含模型密钥被 gitignore，MCP 配置也就
// 进不了版本库（而它恰恰是最该进版本库、最该被评审的那份）。
//
// 段结构保持不变：根级 `mcp_servers:` 列表——独立文件里没必要再套一层。
const FileName = "mcp.yaml"

// MCPServerConfig 对应 mcp.yaml 根级 mcp_servers 列表的单条配置。
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

// mcpServersWrapper 用于解析 YAML 根级的 mcp_servers 列表。
type mcpServersWrapper struct {
	MCPServers []MCPServerConfig `yaml:"mcp_servers"`
}

// Load 从独立的 mcp.yaml 读取 mcp_servers 列表；文件缺失或解析失败返回 nil。
//
// 参数语义（2026-10 起）：path 指向 mcp.yaml **本身**，不再是账号档的路径。
// 调用方经 bootseed 责任链解析出路径（缺失时用内嵌默认档就地初始化），
// 本包不参与路径决策。
func Load(path string) []MCPServerConfig {
	b, err := os.ReadFile(path)
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
