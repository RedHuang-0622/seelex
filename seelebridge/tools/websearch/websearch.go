// Package websearch 提供 web_search 工具注册。
//
// 职责只保留两件事：从账号池 YAML 加载 websearch 配置、把配置装配出的
// 代理策略注册为 web_search 工具；策略装配本身由 seelebridge/search 提供。
package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/RedHuang-0622/seelex/seelebridge/search"
)

// ToolRegistrar 是 websearch 注册所需的运行时窄接口。
type ToolRegistrar interface {
	RegisterTool(name, description string, inputSchema map[string]interface{}, handler func(context.Context, string) (string, error))
}

// Register 是 web_search 的装配点（Assembler）：配置从账号池 YAML 的
// websearch 段加载，由 search.Assemble 装配为代理策略后注册工具；
// 没有可用策略时注册占位工具并给出修复指引。
func Register(registrar ToolRegistrar, accountsPath string) {
	cfg := search.LoadConfig(accountsPath)
	strategy, err := search.Assemble(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠ Web 搜索未装配可用代理策略 (%s 的 websearch 段)：%v\n", accountsPath, err)
		registrar.RegisterTool(
			"web_search",
			"搜索互联网获取最新信息。需要配置账号池 YAML 的 websearch 段。",
			toolSchema(),
			func(context.Context, string) (string, error) {
				return `{"error":"web_search 未装配可用代理策略。请在账号池配置文件的 websearch.strategies 声明搜索 API 端点与密钥（旧字段 provider/api_key 仍兼容）。"}`, nil
			},
		)
		return
	}

	handler := func(ctx context.Context, argsJSON string) (string, error) {
		var input struct {
			Query      string `json:"query"`
			MaxResults int    `json:"max_results"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
			return "", fmt.Errorf("web_search: %w", err)
		}
		if strings.TrimSpace(input.Query) == "" {
			return "", fmt.Errorf("web_search: query 不能为空")
		}
		resp, err := strategy.Search(ctx, input.Query, input.MaxResults)
		if err != nil {
			return "", err
		}
		return search.FormatResponse(resp), nil
	}

	registrar.RegisterTool("web_search", "搜索互联网获取最新信息。用于查找技术文档、论文、开源项目、最新资讯等。支持中英文搜索。", toolSchema(), handler)
}

// toolSchema 返回 web_search 工具的 JSON Schema（参数校验由运行时负责）。
func toolSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "搜索查询词。支持中英文，越具体越好。",
			},
			"max_results": map[string]interface{}{
				"type":        "integer",
				"description": "最大返回结果数（默认 5，最大 10）",
			},
		},
		"required": []string{"query"},
	}
}
