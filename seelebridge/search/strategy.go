// Package search 提供 web_search 工具的统一代理策略域：Strategy 接口、
// 策略装配器（Assembler）与标准 websearch 协议策略。
//
// 设计遵循「引擎无关 + 代理策略配置 + 装配器模式」：工具层不感知任何
// 具体搜索引擎；策略配置只需声明端点与密钥，请求/响应遵循标准协议
// （Tavily 兼容），Assemble 把配置装配为可执行的 Strategy。
package search

import (
	"context"
	"fmt"
	"strings"
)

// SearchItem 是单条搜索结果的规范模型。
type SearchItem struct {
	Title   string
	URL     string
	Content string
	Score   float64
}

// SearchResponse 是搜索结果的规范模型。
type SearchResponse struct {
	Answer string
	Items  []SearchItem
}

// Strategy 是代理策略接口：任何搜索端点（自建代理、Tavily 兼容网关等）
// 都以该接口被装配和执行，工具层与格式化只依赖它。
type Strategy interface {
	// Name 返回策略名称，即配置中 strategies[].name 字段。
	Name() string
	// Search 执行一次搜索并返回归一化结果；maxResults 小于等于 0 或超过
	// DefaultMaxResults 时使用配置的默认结果数。
	Search(ctx context.Context, query string, maxResults int) (SearchResponse, error)
}

// DefaultMaxResults 是 web_search 工具单次返回结果数的全局上限。
const DefaultMaxResults = 10

// Assemble 是 websearch 的装配器（Assembler）：根据配置选择并构建代理策略。
// 选择规则：
//  1. 优先使用配置中的 strategies 列表；
//  2. 未声明策略但存在旧字段（provider: tavily / api_key）时，自动翻译为
//     内置 tavily 兼容策略；
//  3. active（或旧 provider 名）用于在多个策略中选中一个，缺省取第一个；
//  4. 没有任何策略时返回错误，由工具层注册占位工具。
func Assemble(cfg WebSearchConfig) (Strategy, error) {
	strategies := cfg.Strategies
	if len(strategies) == 0 {
		if legacy, ok := legacyTavilyStrategy(cfg); ok {
			strategies = []StrategyConfig{legacy}
		}
	}
	if len(strategies) == 0 {
		return nil, fmt.Errorf("search: 未配置任何 websearch 代理策略（请在 websearch.strategies 声明，或沿用旧字段 provider/api_key）")
	}

	selected := strategies[0]
	name := strings.TrimSpace(cfg.Active)
	if name == "" {
		name = strings.TrimSpace(cfg.Provider)
	}
	if name != "" {
		found := false
		for _, s := range strategies {
			if s.Name == name {
				selected = s
				found = true
				break
			}
		}
		if !found {
			// 旧 provider 名未命中策略列表时明确报错并列出可用策略，
			// 避免多策略场景下静默选错。
			return nil, fmt.Errorf("search: 未找到代理策略 %q（可用: %s）", name, strategyNames(strategies))
		}
	}
	return newStandardStrategy(selected, cfg)
}

// legacyTavilyStrategy 把旧字段（provider: tavily 或仅 api_key）翻译为内置
// tavily 兼容策略，保证现有账号池配置无需修改即可继续使用。
func legacyTavilyStrategy(cfg WebSearchConfig) (StrategyConfig, bool) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	// 显式 provider: tavily 时即使未配 key 也翻译，让装配报「API Key 未配置」；
	// 未写 provider 但配了 api_key 时按历史行为兜底走 tavily。
	if provider == "tavily" || (provider == "" && cfg.APIKey != "") {
		return StrategyConfig{
			Name:     "tavily",
			Endpoint: cfg.Endpoint,
			APIKey:   cfg.APIKey,
		}, true
	}
	return StrategyConfig{}, false
}

// strategyNames 返回可用策略名列表，用于装配错误提示。
func strategyNames(strategies []StrategyConfig) string {
	names := make([]string, 0, len(strategies))
	for _, s := range strategies {
		names = append(names, s.Name)
	}
	return strings.Join(names, ", ")
}

// limitResults 归一化请求结果数：小于等于 0 或超过 DefaultMaxResults 时
// 回退配置默认值，避免异常大响应。
func limitResults(defaultMax, requested int) int {
	if requested <= 0 || requested > DefaultMaxResults {
		return defaultMax
	}
	return requested
}

// WebSearch 是保留的兼容入口：按 cfg 装配代理策略并返回格式化 Markdown。
// 新代码建议直接 Assemble 后调用 Search 与 FormatResponse。
func WebSearch(ctx context.Context, cfg WebSearchConfig, query string, maxResults int) (string, error) {
	strategy, err := Assemble(cfg)
	if err != nil {
		return "", err
	}
	resp, err := strategy.Search(ctx, query, maxResults)
	if err != nil {
		return "", err
	}
	return FormatResponse(resp), nil
}
