package search

import "strings"

// KeyFallback 按**厂商名**取一把兜底密钥（如 "deepseek" → 账号池里那把 DeepSeek key）。
//
// 之所以做成回调而不是让本包去读账号档：搜索域与账号域是两份配置，
// 这里的职责只是"缺 key 时问一句"，取不回就返回空串、走既有的"密钥未配置"。
type KeyFallback func(vendor string) string

// ApplyKeyFallback 用 fallback 填掉**显式没配密钥**的厂商策略。
//
// 三级落点（只在这里定义一次）：`api_key`（显式写死，优先）→ `api_key_env`
// （Load 时已解析）→ 本函数的兜底。只补空，绝不覆盖已经写好的 key；`type` 为空
// 或 standard 的标准协议策略（自建网关/Tavily 兼容端点）不参与兜底——它们没有
// "厂商"可言，凭空塞一把 key 只会把错误提前成一次 401。
func ApplyKeyFallback(cfg WebSearchConfig, fallback KeyFallback) WebSearchConfig {
	if fallback == nil {
		return cfg
	}
	// 复制策略切片再改：配置值是按值传的，但不该改写调用方的底层数组。
	if len(cfg.Strategies) > 0 {
		strategies := make([]StrategyConfig, len(cfg.Strategies))
		copy(strategies, cfg.Strategies)
		for i := range strategies {
			if strings.TrimSpace(strategies[i].APIKey) != "" {
				continue
			}
			if vendor := builtinVendorOf(strategies[i]); vendor != "" {
				strategies[i].APIKey = strings.TrimSpace(fallback(vendor))
			}
		}
		cfg.Strategies = strategies
	}
	// 旧式简写（provider: deepseek + 无 api_key）同样适用。
	if len(cfg.Strategies) == 0 && strings.TrimSpace(cfg.APIKey) == "" {
		if vendor := strings.ToLower(strings.TrimSpace(cfg.Provider)); vendor != "" && isBuiltinVendor(vendor) {
			cfg.APIKey = strings.TrimSpace(fallback(vendor))
		}
	}
	return cfg
}

// builtinVendorOf 返回一条策略的内置厂商名（标准协议策略返回空）。
func builtinVendorOf(cfg StrategyConfig) string {
	vendor := strings.ToLower(strings.TrimSpace(cfg.Type))
	if vendor == "" || vendor == "standard" {
		return ""
	}
	return vendor
}
