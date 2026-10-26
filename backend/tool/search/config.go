package search

import (
	"os"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"gopkg.in/yaml.v3"
)

// FileName 是搜索引擎配置的独立文件名（2026-10 从 accounts.yaml 的 websearch
// 段拆出来）。
//
// 为什么单独一份：搜索是**工具接线**（用哪个引擎、哪把密钥、多深、几条结果），
// 与"用哪个模型账号"无关。混在账号档里让两件事绑死——换引擎要去动凭据文件，
// 而 accounts.yaml 因含模型密钥被 gitignore，搜索配置也就进不了版本库。
//
// 文件格式是**根级字段**（不再套 `websearch:` 段）：文件本身就叫 search_engine，
// 再套一层同名段名只是把每一行都埋深两格。
const FileName = "search_engine.yaml"

// WebSearchConfig 是跨层稳定的 web_search 配置形状。
type WebSearchConfig = dto.WebSearchConfig

// StrategyConfig 是跨层稳定的代理策略配置形状。
type StrategyConfig = dto.StrategyConfig

// DefaultConfig 返回 websearch 的默认配置：引擎无关（Provider 为空，
// 不默认选择任何搜索引擎），只保留结果数与选项默认值。
func DefaultConfig() WebSearchConfig {
	return WebSearchConfig{
		MaxResults:    5,
		IncludeAnswer: true,
		SearchDepth:   "advanced",
	}
}

// Load 从独立的 search_engine.yaml 加载配置；文件缺失或解析失败时返回默认配置。
// include_answer 只在显式配置时覆盖，避免历史无条件置 false。
func Load(path string) WebSearchConfig {
	cfg := DefaultConfig()
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	var loaded WebSearchConfig
	if err := yaml.Unmarshal(b, &loaded); err != nil {
		return cfg
	}
	mergeConfig(&cfg, loaded)

	var presence map[string]yaml.Node
	if err := yaml.Unmarshal(b, &presence); err == nil {
		if _, ok := presence["include_answer"]; ok {
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
		// 密钥三级落点：api_key / apikey 别名 / api_key_env 指向的环境变量。
		if src.Strategies[i].APIKey == "" && src.Strategies[i].APIKeyEnv != "" {
			src.Strategies[i].APIKey = os.Getenv(src.Strategies[i].APIKeyEnv)
		}
	}
	dst.Strategies = src.Strategies
}
