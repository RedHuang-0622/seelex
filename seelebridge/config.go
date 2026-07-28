package seelebridge

import (
	"fmt"
	"log"
	"os"

	"github.com/RedHuang-0622/Seele/agent/core/api"
	"gopkg.in/yaml.v3"
)

// simplifiedAccount is a single entry in the role-based config format.
type simplifiedAccount struct {
	Model   string `yaml:"model"`
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
}

// simplifiedConfig represents the role-grouped accounts.yaml format:
//
//	roles:
//	  agent:
//	    - model: balanced-model
//	      base_url: https://api.openai.com/v1
//	      api_key: sk-xxx
//	  subagent:
//	    - model: fast-model
//	  goalplan:
//	    - model: deep-model
type simplifiedConfig struct {
	Roles struct {
		Agent    []simplifiedAccount `yaml:"agent"`
		SubAgent []simplifiedAccount `yaml:"subagent"`
		GoalPlan []simplifiedAccount `yaml:"goalplan"`
	} `yaml:"roles"`
}

// loadSimplifiedConfig reads the role-grouped format and builds an api.AccountPool.
// Missing roles fall back to the agent role; if agent is also missing the first
// available role is used by ResolveAccount.
func loadSimplifiedConfig(path string) (*api.AccountPool, []AccountRole, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		// 测试/无配置文件时回退到默认单账号池
		if os.IsNotExist(err) {
			return api.NewAccountPool(&api.Account{
				Name: "fallback", Provider: "openai",
				Model: "gpt-4o", BaseURL: "https://api.openai.com/v1",
				APIKey: os.Getenv("OPENAI_API_KEY"),
			}), []AccountRole{RoleAgent}, nil
		}
		return nil, nil, err
	}

	var cfg simplifiedConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, nil, fmt.Errorf("seelebridge: parse accounts: %w", err)
	}
	log.Printf("[config] parsed: agent=%d subagent=%d goalplan=%d",
		len(cfg.Roles.Agent), len(cfg.Roles.SubAgent), len(cfg.Roles.GoalPlan))

	roleMap := map[AccountRole][]simplifiedAccount{
		RoleAgent:    cfg.Roles.Agent,
		RoleSubAgent: cfg.Roles.SubAgent,
		RoleGoalPlan: cfg.Roles.GoalPlan,
	}

	var availableRoles []AccountRole
	for _, r := range []AccountRole{RoleAgent, RoleSubAgent, RoleGoalPlan} {
		if len(roleMap[r]) > 0 {
			availableRoles = append(availableRoles, r)
		}
	}
	if len(availableRoles) == 0 {
		return nil, nil, fmt.Errorf("seelebridge: no accounts configured in any role")
	}

	var accounts []*api.Account
	for role, entries := range roleMap {
		for i, entry := range entries {
			name := fmt.Sprintf("%s-%d", role, i+1)
			accounts = append(accounts, &api.Account{
				Name:     name,
				Provider: "openai",
				BaseURL:  entry.BaseURL,
				APIKey:   entry.APIKey,
				Model:    entry.Model,
			})
		}
	}

	pool := api.NewAccountPool(accounts...)
	return pool, availableRoles, nil
}

// AccountRole represents the task category an account is used for.
type AccountRole string

const (
	RoleAgent    AccountRole = "agent"
	RoleSubAgent AccountRole = "subagent"
	RoleGoalPlan AccountRole = "goalplan"
)

// ResolveAccount picks an account from the pool for the given role.
// Falls back: goalplan → agent, subagent → agent, agent → first available.
func ResolveAccount(pool *api.AccountPool, role AccountRole) (*api.Account, error) {
	return ResolveAccountForBranch(pool, role, "")
}

func accountRole(name string) AccountRole {
	for _, r := range []AccountRole{RoleAgent, RoleSubAgent, RoleGoalPlan} {
		if len(name) > len(r) && name[:len(r)] == string(r) {
			return r
		}
	}
	return RoleAgent
}
