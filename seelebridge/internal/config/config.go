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
	// agent/goalplan 保持 1：主会话与规划路径本来就受 Session 单锁约束，
	// 并发租约只是兜底；子代理角色另有 defaultSubagentMaxConcurrency。
	defaultMaxConcurrency = 1
	// defaultSubagentMaxConcurrency 是 subagent 角色账号的默认并发租约上限。
	// 2026-09-07：subagent 只是“角色 + 模型供应商”，不再由框架设实用上限
	// （高水位远高于任何真实 fork 扇出；实际在途请求受供应商/API 限流与
	// 429 重试约束）。可在账号条目里用 max_concurrency 显式覆盖。
	defaultSubagentMaxConcurrency = 1024
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
	Provider        string   `yaml:"provider"`
	ContextWindow   *int     `yaml:"context_window"`
	MaxTokens       *int     `yaml:"max_tokens"`
	Temperature     *float64 `yaml:"temperature"`
	ReasoningEffort string   `yaml:"reasoning_effort"`
}

// simplifiedAccount is a single entry in the role-based config format.
type simplifiedAccount struct {
	Provider        string   `yaml:"provider"`
	Model           string   `yaml:"model"`
	BaseURL         string   `yaml:"base_url"`
	APIKey          string   `yaml:"api_key"`
	ContextWindow   *int     `yaml:"context_window"`
	MaxTokens       *int     `yaml:"max_tokens"`
	MaxConcurrency  *int     `yaml:"max_concurrency"`
	Temperature     *float64 `yaml:"temperature"`
	ReasoningEffort string   `yaml:"reasoning_effort"`
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
			maxConcurrency := defaultMaxConcurrency
			if role == model.RoleSubAgent {
				maxConcurrency = defaultSubagentMaxConcurrency
			}
			if entry.MaxConcurrency != nil {
				if *entry.MaxConcurrency <= 0 {
					return Config{}, fmt.Errorf("seelebridge: account %q max_concurrency must be greater than zero", name)
				}
				maxConcurrency = *entry.MaxConcurrency
			}
			limits, err := resolveAccountLimits(cfg.Defaults, entry)
			if err != nil {
				return Config{}, fmt.Errorf("seelebridge: account %q: %w", name, err)
			}
			temperature, err := resolveTemperature(cfg.Defaults, entry)
			if err != nil {
				return Config{}, fmt.Errorf("seelebridge: account %q: %w", name, err)
			}
			reasoningEffort, err := resolveReasoningEffort(cfg.Defaults, entry, role)
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
				MaxConcurrency:  maxConcurrency,
				Temperature:     temperature,
				ReasoningEffort: reasoningEffort,
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

// LoadTolerant 与 Load 相同，但配置损坏（YAML 解析失败、账号字段非法）或
// 未配置任何角色时不返回致命错误：返回内置兜底账号配置，并把原始错误作为
// 非致命启动警告交给调用方展示。配置文件缺失仍视为正常回退（无警告）。
// 用途：配置写错时应用照常启动，用户能在界面里看到错误原因，而不是闪退。
func LoadTolerant(path string) (Config, error) {
	cfg, err := Load(path)
	if err != nil {
		return fallbackConfig(), err
	}
	return cfg, nil
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
		Temperature:     model.DefaultTemperature,
		ReasoningEffort: model.DefaultReasoningEffortForRole(model.RoleAgent),
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

// resolveTemperature 解析账号采样温度：账号级 > defaults 级 > 代码默认
// （model.DefaultTemperature）。**0 是合法值**（DeepSeek 官方对编码/数学的推荐
// 就是 0.0），所以用指针区分"没写"与"写了 0"——不拿 0 当哨兵。
//
// 提醒：DeepSeek 的思考模式不支持 temperature（设了不报错、也没效果）。这条
// 配置真正生效的对象是需要 temperature 的非思考模型与其它 provider；想让
// deepseek-flash "想得更深"，要看 reasoning_effort，不是这里。
func resolveTemperature(defaults simplifiedDefaults, account simplifiedAccount) (float64, error) {
	temperature := model.DefaultTemperature
	if defaults.Temperature != nil {
		temperature = *defaults.Temperature
	}
	if account.Temperature != nil {
		temperature = *account.Temperature
	}
	if temperature < 0 || temperature > 2 {
		return 0, fmt.Errorf("temperature must be between 0 and 2, got %v", temperature)
	}
	return temperature, nil
}

// resolveReasoningEffort 解析账号的思考强度（reasoning effort）：
// 账号级 > defaults 级 > 角色默认（model.DefaultReasoningEffortForRole）。
//
// 为什么有"角色默认"这一层：agent / subagent / goalplan 的定位不同——主会话
// 跟随用户选的会话档位、子代理只做窄任务、规划值得想但不该顶格。这不是一个
// 全局常数能表达的。显式配置永远优先：想给 goalplan 也配 max，写一行就够。
//
// 值域：dto 的 provider 词表，外加 "session"（跟随会话 effort 档位，agent 的
// 默认）。空串在这里就被补成角色默认，所以往下游（AccountSpec → ChatClient）
// 不会再出现"没配置"这种中间态。
//
// 注意这里**只定思考强度**：effort 档位携带的 loop 次数与执行预算不在这一层，
// 也不受这里的值影响——调强度不会动 loop。
func resolveReasoningEffort(defaults simplifiedDefaults, account simplifiedAccount, role model.AccountRole) (string, error) {
	effort := model.DefaultReasoningEffortForRole(role)
	if value := strings.TrimSpace(defaults.ReasoningEffort); value != "" {
		effort = value
	}
	if value := strings.TrimSpace(account.ReasoningEffort); value != "" {
		effort = value
	}
	if !model.ValidReasoningEffort(effort) {
		return "", fmt.Errorf("reasoning_effort %q is not valid, want one of %v", effort, model.ReasoningEffortVocabulary())
	}
	return effort, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
