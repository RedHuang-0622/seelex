package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/seelebridge"
)

// loadWebSearchConfig 从账号池配置文件中加载 websearch 段。
func loadWebSearchConfig(accountsPath string) application.WebSearchConfig {
	cfg := application.WebSearchConfig{
		Provider:      "tavily",
		MaxResults:    5,
		IncludeAnswer: true,
		SearchDepth:   "advanced",
	}
	b, err := os.ReadFile(accountsPath)
	if err != nil {
		return cfg
	}
	var wrapper struct {
		WebSearch application.WebSearchConfig `yaml:"websearch"`
	}
	if err := yaml.Unmarshal(b, &wrapper); err != nil {
		return cfg
	}
	if wrapper.WebSearch.Provider != "" {
		cfg.Provider = wrapper.WebSearch.Provider
	}
	if wrapper.WebSearch.APIKey != "" {
		cfg.APIKey = wrapper.WebSearch.APIKey
	}
	if wrapper.WebSearch.MaxResults > 0 {
		cfg.MaxResults = wrapper.WebSearch.MaxResults
	}
	if wrapper.WebSearch.SearchDepth != "" {
		cfg.SearchDepth = wrapper.WebSearch.SearchDepth
	}
	cfg.IncludeAnswer = wrapper.WebSearch.IncludeAnswer
	return cfg
}

// registerWebSearchTool 注册 web_search 工具到 Runtime。
// 配置从账号池 YAML 的 websearch 段加载。
func registerWebSearchTool(runtime *seelebridge.Runtime, accountsPath string) {
	cfg := loadWebSearchConfig(accountsPath)

	toolDesc := "搜索互联网获取最新信息。用于查找技术文档、论文、开源项目、最新资讯等。支持中英文搜索。"
	handler := func(ctx context.Context, argsJSON string) (string, error) {
		var input struct {
			Query      string `json:"query"`
			MaxResults int    `json:"max_results"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
			return "", fmt.Errorf("web_search: %w", err)
		}
		return application.WebSearch(ctx, cfg, input.Query, input.MaxResults)
	}

	if cfg.APIKey == "" {
		fmt.Fprintf(os.Stderr, "⚠ Web 搜索未配置 API Key (%s 中的 websearch.api_key)，注册占位工具\n", accountsPath)
		toolDesc = "搜索互联网获取最新信息。需要配置账号池 YAML 中的 websearch.api_key。"
		handler = func(ctx context.Context, argsJSON string) (string, error) {
			return `{"error":"web_search 未配置 API Key。请在账号池配置文件的 websearch 段填入 Tavily API Key。"}`, nil
		}
	}

	runtime.RegisterTool(
		"web_search",
		toolDesc,
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "搜索查询词。支持中英文，越具体越好。",
				},
				"max_results": map[string]any{
					"type":        "integer",
					"description": "最大返回结果数（默认5，最多10）",
				},
			},
			"required": []string{"query"},
		},
		handler,
	)
}
