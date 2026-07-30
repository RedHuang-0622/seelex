package seelebridge

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/RedHuang-0622/Seele/agent/core/api"
	"gopkg.in/yaml.v3"
)

const (
	defaultContextWindow   = 200_000
	defaultMaxOutputTokens = 8_192
)

type accountLimits struct {
	ContextWindow   int
	MaxOutputTokens int
}

type loadedAccountConfig struct {
	Pool           *api.AccountPool
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
// pool plus Seelex-owned context limits. Missing roles are resolved later by
// ResolveAccount.
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

	accounts := make([]*api.Account, 0)
	limitsByAccount := make(map[string]accountLimits)
	for _, role := range roleOrder {
		for index, entry := range roleMap[role] {
			name := fmt.Sprintf("%s-%d", role, index+1)
			limits, err := resolveAccountLimits(cfg.Defaults, entry)
			if err != nil {
				return loadedAccountConfig{}, fmt.Errorf("seelebridge: account %q: %w", name, err)
			}
			provider := firstNonEmpty(entry.Provider, cfg.Defaults.Provider, string(api.ProviderOpenAI))
			accounts = append(accounts, &api.Account{
				Name:      name,
				Provider:  api.ProviderType(provider),
				BaseURL:   entry.BaseURL,
				APIKey:    entry.APIKey,
				Model:     entry.Model,
				MaxTokens: limits.MaxOutputTokens,
			})
			limitsByAccount[name] = limits
		}
	}

	return loadedAccountConfig{
		Pool:           api.NewAccountPool(accounts...),
		AvailableRoles: availableRoles,
		Limits:         limitsByAccount,
	}, nil
}

func fallbackAccountConfig() loadedAccountConfig {
	limits := accountLimits{ContextWindow: defaultContextWindow, MaxOutputTokens: defaultMaxOutputTokens}
	account := &api.Account{
		Name:      "fallback",
		Provider:  api.ProviderOpenAI,
		Model:     "gpt-4o",
		BaseURL:   "https://api.openai.com/v1",
		APIKey:    os.Getenv("OPENAI_API_KEY"),
		MaxTokens: limits.MaxOutputTokens,
	}
	return loadedAccountConfig{
		Pool:           api.NewAccountPool(account),
		AvailableRoles: []AccountRole{RoleAgent},
		Limits:         map[string]accountLimits{account.Name: limits},
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

// ResolveAccount picks an account from the pool for the given role.
func ResolveAccount(pool *api.AccountPool, role AccountRole) (*api.Account, error) {
	return ResolveAccountForBranch(pool, role, "")
}

func accountRole(name string) AccountRole {
	for _, role := range []AccountRole{RoleAgent, RoleSubAgent, RoleGoalPlan} {
		if len(name) > len(role) && name[:len(role)] == string(role) {
			return role
		}
	}
	return RoleAgent
}
