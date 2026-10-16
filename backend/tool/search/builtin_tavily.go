package search

import "strings"

// init 注册 "tavily" 内置厂商：Tavily 原生 API 请求体与响应就是标准
// websearch 协议，直接复用 standardStrategy；未声明 endpoint 时使用
// Tavily 官方默认端点，未声明密钥时装配期报「API Key 未配置」。
func init() {
	registerBuiltin("tavily", func(cfg StrategyConfig, opts WebSearchConfig) (Strategy, error) {
		if strings.TrimSpace(cfg.Name) == "" {
			cfg.Name = "tavily"
		}
		if strings.TrimSpace(cfg.Endpoint) == "" {
			cfg.Endpoint = defaultTavilyEndpoint
		}
		return newStandardStrategy(cfg, opts)
	})
}
