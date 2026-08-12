// Package config 加载简化账号 YAML（accounts*.yaml 角色分组格式），
// 产出账号规格与 Seelex 侧上下文预算。属于根 facade 的装配细节
// （仅 runtime.go 使用），因此置于 internal/，不对外暴露。
package config

import (
	"fmt"
	"log"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

const (
	// DefaultContextWindow 是未配置时的默认上下文窗口。
	DefaultContextWindow = 200_000
	// DefaultMaxOutputTokens 是未配置时的默认输出 token 上限。
	DefaultMaxOutputTokens = 8_192
	// defaultMaxConcurrency 是每个账号默认的并发租约上限。
	// 主会话串行（Session 单锁），默认 1 即可；后续切片为 Plan 子代理并行时按角色上调。
	defaultMaxConcurrency = 1
)

// AccountLimits 是账号的上下文/输出预算。
type AccountLimits struct {
	ContextWindow   int
	MaxOutputTokens int
}

// Config 是账号配置加载结果：规格、可用角色与按账号的预算表。
type Config struct {
	Specs          []model.AccountSpec
	AvailableRoles []model.AccountRole
	Limits         map[string]AccountLimits
}

type simplifiedDefaults struct {
	Provider      string `yaml:"provider"`
	ContextWindow *int   `yaml:"context_window"`
	MaxTokens     *int   `yaml:"max_tokens"`
}

// simplifiedAccount is a single entry in the role-based config format.
type simplifiedAccount struct {
	Provider      string `yaml:"provider"`
	Model         string `yaml:"model"`
	BaseURL       string `yaml:"base_url"`
	APIKey        string `yaml:"api_key"`
	ContextWindow *int   `yaml:"context_window"`
	MaxTokens     *int   `yaml:"max_tokens"`
}

// simplifiedConfig represents the role-grouped accounts.yaml format.
type simplifiedConfig struct {
	Defaults simplifiedDefaults `yaml:"defaults"`
	Roles    struct {
		Agent    []simplifiedAccount `yaml:"agent"`
		SubAgent []simplifiedAccount `yaml:"subagent"`
		GoalPlan []simplifiedAccount `yaml:"goalplan"`
	} `yaml:"roles"`
}

// Load reads the role-grouped accounts.yaml format and builds the account
// specs plus Seelex-owned context limits. Missing roles are resolved later by
// model.ResolveAccountSpec.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fallbackConfig(), nil
		}
		return Config{}, err
	}

	var cfg simplifiedConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("seelebridge: parse accounts: %w", err)
	}
	log.Printf("[config] parsed: agent=%d subagent=%d goalplan=%d",
		len(cfg.Roles.Agent), len(cfg.Roles.SubAgent), len(cfg.Roles.GoalPlan))

	roleMap := map[model.AccountRole][]simplifiedAccount{
		model.RoleAgent:    cfg.Roles.Agent,
		model.RoleSubAgent: cfg.Roles.SubAgent,
		model.RoleGoalPlan: cfg.Roles.GoalPlan,
	}
	roleOrder := []model.AccountRole{model.RoleAgent, model.RoleSubAgent, model.RoleGoalPlan}
	availableRoles := make([]model.AccountRole, 0, len(roleOrder))
	for _, role := range roleOrder {
		if len(roleMap[role]) > 0 {
			availableRoles = append(availableRoles, role)
		}
	}
	if len(availableRoles) == 0 {
		return Config{}, fmt.Errorf("seelebridge: no accounts configured in any role")
	}

	specs := make([]model.AccountSpec, 0)
	limitsByAccount := make(map[string]AccountLimits)
	for _, role := range roleOrder {
		for index, entry := range roleMap[role] {
			name := fmt.Sprintf("%s-%d", role, index+1)
			limits, err := resolveAccountLimits(cfg.Defaults, entry)
			if err != nil {
				return Config{}, fmt.Errorf("seelebridge: account %q: %w", name, err)
			}
			provider := firstNonEmpty(entry.Provider, cfg.Defaults.Provider, "openai")
			specs = append(specs, model.AccountSpec{
				Name:            name,
				Provider:        provider,
				BaseURL:         entry.BaseURL,
				APIKey:          entry.APIKey,
				Model:           entry.Model,
				MaxTokens:       limits.MaxOutputTokens,
				ContextWindow:   limits.ContextWindow,
				MaxOutputTokens: limits.MaxOutputTokens,
				MaxConcurrency:  defaultMaxConcurrency,
				Role:            role,
			})
			limitsByAccount[name] = limits
		}
	}

	return Config{
		Specs:          specs,
		AvailableRoles: availableRoles,
		Limits:         limitsByAccount,
	}, nil
}

func fallbackConfig() Config {
	limits := AccountLimits{ContextWindow: DefaultContextWindow, MaxOutputTokens: DefaultMaxOutputTokens}
	spec := model.AccountSpec{
		Name:            "fallback",
		Provider:        "openai",
		Model:           "gpt-4o",
		BaseURL:         "https://api.openai.com/v1",
		APIKey:          os.Getenv("OPENAI_API_KEY"),
		MaxTokens:       limits.MaxOutputTokens,
		ContextWindow:   limits.ContextWindow,
		MaxOutputTokens: limits.MaxOutputTokens,
		MaxConcurrency:  defaultMaxConcurrency,
		Role:            model.RoleAgent,
	}
	return Config{
		Specs:          []model.AccountSpec{spec},
		AvailableRoles: []model.AccountRole{model.RoleAgent},
		Limits:         map[string]AccountLimits{spec.Name: limits},
	}
}

func resolveAccountLimits(defaults simplifiedDefaults, account simplifiedAccount) (AccountLimits, error) {
	limits := AccountLimits{ContextWindow: DefaultContextWindow, MaxOutputTokens: DefaultMaxOutputTokens}
	if defaults.ContextWindow != nil {
		limits.ContextWindow = *defaults.ContextWindow
	}
	if defaults.MaxTokens != nil {
		limits.MaxOutputTokens = *defaults.MaxTokens
	}
	if account.ContextWindow != nil {
		limits.ContextWindow = *account.ContextWindow
	}
	if account.MaxTokens != nil {
		limits.MaxOutputTokens = *account.MaxTokens
	}
	if limits.ContextWindow <= 0 {
		return AccountLimits{}, fmt.Errorf("context_window must be greater than zero")
	}
	if limits.MaxOutputTokens <= 0 {
		return AccountLimits{}, fmt.Errorf("max_tokens must be greater than zero")
	}
	if limits.MaxOutputTokens+limits.ContextWindow/8 >= limits.ContextWindow {
		return AccountLimits{}, fmt.Errorf(
			"max_tokens (%d) plus safety reserve must be smaller than context_window (%d)",
			limits.MaxOutputTokens, limits.ContextWindow,
		)
	}
	return limits, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
