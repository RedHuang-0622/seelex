package seelebridge

import (
	"fmt"
	"log"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	defaultContextWindow   = 200_000
	defaultMaxOutputTokens = 8_192
	// defaultMaxConcurrency 是每个账号默认的并发租约上限。
	// 主会话串行（Session 单锁），默认 1 即可；后续切片为 Plan 子代理并行时按角色上调。
	defaultMaxConcurrency = 1
)

type accountLimits struct {
	ContextWindow   int
	MaxOutputTokens int
}

// accountSpec 是账号的非敏感配置（凭据在 APIKey 内，仅用于组装客户端）。
type accountSpec struct {
	Name            string
	Provider        string
	BaseURL         string
	APIKey          string
	Model           string
	MaxTokens       int
	ContextWindow   int
	MaxOutputTokens int
	MaxConcurrency  int
	Role            AccountRole
}

type loadedAccountConfig struct {
	Specs          []accountSpec
	AvailableRoles []AccountRole
	Limits         map[string]accountLimits
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

// loadSimplifiedConfig reads the role-grouped format and builds the account
// specs plus Seelex-owned context limits. Missing roles are resolved later by
// ResolveAccountSpec.
func loadSimplifiedConfig(path string) (loadedAccountConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fallbackAccountConfig(), nil
		}
		return loadedAccountConfig{}, err
	}

	var cfg simplifiedConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return loadedAccountConfig{}, fmt.Errorf("seelebridge: parse accounts: %w", err)
	}
	log.Printf("[config] parsed: agent=%d subagent=%d goalplan=%d",
		len(cfg.Roles.Agent), len(cfg.Roles.SubAgent), len(cfg.Roles.GoalPlan))

	roleMap := map[AccountRole][]simplifiedAccount{
		RoleAgent:    cfg.Roles.Agent,
		RoleSubAgent: cfg.Roles.SubAgent,
		RoleGoalPlan: cfg.Roles.GoalPlan,
	}
	roleOrder := []AccountRole{RoleAgent, RoleSubAgent, RoleGoalPlan}
	availableRoles := make([]AccountRole, 0, len(roleOrder))
	for _, role := range roleOrder {
		if len(roleMap[role]) > 0 {
			availableRoles = append(availableRoles, role)
		}
	}
	if len(availableRoles) == 0 {
		return loadedAccountConfig{}, fmt.Errorf("seelebridge: no accounts configured in any role")
	}

	specs := make([]accountSpec, 0)
	limitsByAccount := make(map[string]accountLimits)
	for _, role := range roleOrder {
		for index, entry := range roleMap[role] {
			name := fmt.Sprintf("%s-%d", role, index+1)
			limits, err := resolveAccountLimits(cfg.Defaults, entry)
			if err != nil {
				return loadedAccountConfig{}, fmt.Errorf("seelebridge: account %q: %w", name, err)
			}
			provider := firstNonEmpty(entry.Provider, cfg.Defaults.Provider, "openai")
			specs = append(specs, accountSpec{
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

	return loadedAccountConfig{
		Specs:          specs,
		AvailableRoles: availableRoles,
		Limits:         limitsByAccount,
	}, nil
}

func fallbackAccountConfig() loadedAccountConfig {
	limits := accountLimits{ContextWindow: defaultContextWindow, MaxOutputTokens: defaultMaxOutputTokens}
	spec := accountSpec{
		Name:            "fallback",
		Provider:        "openai",
		Model:           "gpt-4o",
		BaseURL:         "https://api.openai.com/v1",
		APIKey:          os.Getenv("OPENAI_API_KEY"),
		MaxTokens:       limits.MaxOutputTokens,
		ContextWindow:   limits.ContextWindow,
		MaxOutputTokens: limits.MaxOutputTokens,
		MaxConcurrency:  defaultMaxConcurrency,
		Role:            RoleAgent,
	}
	return loadedAccountConfig{
		Specs:          []accountSpec{spec},
		AvailableRoles: []AccountRole{RoleAgent},
		Limits:         map[string]accountLimits{spec.Name: limits},
	}
}

func resolveAccountLimits(defaults simplifiedDefaults, account simplifiedAccount) (accountLimits, error) {
	limits := accountLimits{ContextWindow: defaultContextWindow, MaxOutputTokens: defaultMaxOutputTokens}
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
		return accountLimits{}, fmt.Errorf("context_window must be greater than zero")
	}
	if limits.MaxOutputTokens <= 0 {
		return accountLimits{}, fmt.Errorf("max_tokens must be greater than zero")
	}
	if limits.MaxOutputTokens+limits.ContextWindow/8 >= limits.ContextWindow {
		return accountLimits{}, fmt.Errorf(
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

// AccountRole represents the task category an account is used for.
type AccountRole string

const (
	RoleAgent    AccountRole = "agent"
	RoleSubAgent AccountRole = "subagent"
	RoleGoalPlan AccountRole = "goalplan"
)

// ResolveAccountSpec picks an account spec from the loaded config for the
// given role. Roles fall back to the primary agent role when they are not
// configured, preserving single-account installations.
func ResolveAccountSpec(specs []accountSpec, role AccountRole) (accountSpec, error) {
	roles := append([]AccountRole{role}, fallbackRoles(role)...)
	for _, candidate := range roles {
		for _, spec := range specs {
			if spec.Role == candidate && spec.APIKey != "" {
				return spec, nil
			}
		}
	}
	for _, spec := range specs {
		if spec.APIKey != "" {
			return spec, nil
		}
	}
	return accountSpec{}, fmt.Errorf("seelebridge: no accounts available")
}

func accountRole(name string) AccountRole {
	for _, role := range []AccountRole{RoleAgent, RoleSubAgent, RoleGoalPlan} {
		if len(name) > len(role) && name[:len(role)] == string(role) {
			return role
		}
	}
	return RoleAgent
}
