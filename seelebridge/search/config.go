package search

import (
	"os"

	"gopkg.in/yaml.v3"
)

// WebSearchConfig 是 web_search 工具的搜索配置，对应账号池 YAML 的
// websearch 段。Strategies 声明代理策略（proxy strategy）列表；每个策略
// 只需声明 name / endpoint / 密钥，请求与响应遵循标准 websearch 协议
// （Tavily 兼容），工具本身不绑定任何具体搜索引擎。
type WebSearchConfig struct {
	Provider       string           `yaml:"provider"`
	APIKey         string           `yaml:"api_key"`
	Endpoint       string           `yaml:"endpoint"`
	MaxResults     int              `yaml:"max_results"`
	IncludeAnswer  bool             `yaml:"include_answer"`
	SearchDepth    string           `yaml:"search_depth"`
	TimeoutSeconds int              `yaml:"timeout"`
	Active         string           `yaml:"active"`
	Strategies     []StrategyConfig `yaml:"strategies"`
}

// StrategyConfig 描述一个代理策略：内置厂商类型（可选）+ 搜索端点与密钥。
//
// Type 为空或 "standard" 时使用标准 websearch 协议（Tavily 兼容：
// POST JSON + Bearer 鉴权，响应含 results 数组），无需声明 method、header
// 或字段映射；Type 为内置厂商名（tavily / bochaai / searxng，见 builtin.go）
// 时使用该厂商的专有协议适配器。无论哪种 Type，装配出的 Strategy 对外
// 入参 (ctx, query, maxResults) 与出参 (SearchResponse) 完全一致。
type StrategyConfig struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Endpoint    string `yaml:"endpoint"`
	APIKey      string `yaml:"api_key"`
	APIKeyAlias string `yaml:"apikey"`
}

// DefaultConfig 返回 websearch 的默认配置：引擎无关（Provider 为空，
// 不默认选择任何搜索引擎），只保留结果数与选项默认值。
func DefaultConfig() WebSearchConfig {
	return WebSearchConfig{
		MaxResults:    5,
		IncludeAnswer: true,
		SearchDepth:   "advanced",
	}
}

// LoadConfig 从账号池 YAML 的 websearch 段加载配置；文件缺失或解析失败时
// 返回默认配置。include_answer 只在显式配置时覆盖，避免历史无条件置 false。
func LoadConfig(accountsPath string) WebSearchConfig {
	cfg := DefaultConfig()
	b, err := os.ReadFile(accountsPath)
	if err != nil {
		return cfg
	}
	var wrapper struct {
		WebSearch WebSearchConfig `yaml:"websearch"`
	}
	if err := yaml.Unmarshal(b, &wrapper); err != nil {
		return cfg
	}
	loaded := wrapper.WebSearch
	mergeConfig(&cfg, loaded)

	var presence struct {
		WebSearch map[string]yaml.Node `yaml:"websearch"`
	}
	if err := yaml.Unmarshal(b, &presence); err == nil {
		if _, ok := presence.WebSearch["include_answer"]; ok {
			cfg.IncludeAnswer = loaded.IncludeAnswer
		}
	}
	return cfg
}

// mergeConfig 用加载值覆盖非零字段，保留默认值；兼容 apikey 别名。
func mergeConfig(dst *WebSearchConfig, src WebSearchConfig) {
	if src.Provider != "" {
		dst.Provider = src.Provider
	}
	if src.APIKey != "" {
		dst.APIKey = src.APIKey
	}
	if src.Endpoint != "" {
		dst.Endpoint = src.Endpoint
	}
	if src.MaxResults > 0 {
		dst.MaxResults = src.MaxResults
	}
	if src.SearchDepth != "" {
		dst.SearchDepth = src.SearchDepth
	}
	if src.TimeoutSeconds > 0 {
		dst.TimeoutSeconds = src.TimeoutSeconds
	}
	if src.Active != "" {
		dst.Active = src.Active
	}
	for i := range src.Strategies {
		if src.Strategies[i].APIKey == "" {
			src.Strategies[i].APIKey = src.Strategies[i].APIKeyAlias
		}
	}
	dst.Strategies = src.Strategies
}
