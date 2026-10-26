package dto

// WebSearchConfig is the web_search tool configuration.
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

// StrategyConfig describes one web-search proxy strategy.
type StrategyConfig struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Endpoint    string `yaml:"endpoint"`
	APIKey      string `yaml:"api_key"`
	APIKeyAlias string `yaml:"apikey"`
	APIKeyEnv   string `yaml:"api_key_env"`
}
