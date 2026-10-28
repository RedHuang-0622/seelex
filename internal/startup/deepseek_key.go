// deepseek_key.go — 从模型账号档里取一把 DeepSeek 密钥，供 web_search 复用。
//
// 为什么需要这一格：DeepSeek 官方 Search & Fetch API 与模型 API 共用**同一把**
// Bearer key。搜索配置与账号档自 2026-10 拆开后，各自只该写自己的东西——
// 搜索侧写"用哪个引擎"，账号侧写"用哪个账号"。所以搜索配置通常**不必再写一遍
// key**：没写时从这里按厂商名取账号池里已有的那一把。
//
// 边界：本文件只读、只返回 key 字符串；账号档不因它承载任何搜索配置，
// 也不会把搜索策略写回账号档。
package startup

import (
	"net/url"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// deepSeekKeyRoleOrder 是取 key 的角色偏好顺序：先取"主对话"角色的账号，
// 再取子代理与规划角色，其余角色按名字字典序——同一份账号档每次都取到同一把 key。
var deepSeekKeyRoleOrder = []string{"agent", "subagent", "goalplan"}

// accountEntry 是账号档里取 key 所需的字段（其余一概不解析）。
type accountEntry struct {
	Model   string `yaml:"model"`
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
}

// accountsDoc 是账号档的最小结构。
type accountsDoc struct {
	Defaults struct {
		Provider string `yaml:"provider"`
	} `yaml:"defaults"`
	Roles map[string][]accountEntry `yaml:"roles"`
}

// DeepSeekSearchKey 返回账号档里第一把 DeepSeek 密钥；取不到返回空串。
func DeepSeekSearchKey() string {
	return deepSeekSearchKeyFrom(AccountsPath())
}

// deepSeekSearchKeyFrom 从指定账号档取 DeepSeek 密钥（测试经此注入路径）。
//
// 判据（唯一两格，命中任一且 key 非空即取）：`base_url` 指向 deepseek 域名，
// 或 `model` 名以 deepseek 开头；账号条目两者都没写时退回 `defaults.provider`。
// 读不到文件 / 解析失败 / 没有 DeepSeek 条目都返回空串——调用方据此走
// "密钥未配置"的既有回落，不在这里报错。
func deepSeekSearchKeyFrom(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var doc accountsDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	defaultsDeepSeek := strings.EqualFold(strings.TrimSpace(doc.Defaults.Provider), "deepseek")
	for _, role := range accountRoleOrder(doc.Roles) {
		for _, account := range doc.Roles[role] {
			key := strings.TrimSpace(account.APIKey)
			if key == "" {
				continue
			}
			if accountIsDeepSeek(account, defaultsDeepSeek) {
				return key
			}
		}
	}
	return ""
}

// accountRoleOrder 返回角色名的确定顺序：偏好角色在前，其余按字典序。
func accountRoleOrder(roles map[string][]accountEntry) []string {
	ordered := make([]string, 0, len(roles))
	seen := make(map[string]bool, len(roles))
	for _, role := range deepSeekKeyRoleOrder {
		if _, ok := roles[role]; ok {
			ordered = append(ordered, role)
			seen[role] = true
		}
	}
	rest := make([]string, 0, len(roles))
	for role := range roles {
		if !seen[role] {
			rest = append(rest, role)
		}
	}
	sort.Strings(rest)
	return append(ordered, rest...)
}

// accountIsDeepSeek 报告一条账号是不是 DeepSeek。
func accountIsDeepSeek(account accountEntry, defaultsDeepSeek bool) bool {
	baseURL := strings.ToLower(strings.TrimSpace(account.BaseURL))
	if baseURL != "" {
		if parsed, err := url.Parse(baseURL); err == nil && strings.Contains(parsed.Hostname(), "deepseek") {
			return true
		}
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(account.Model)), "deepseek") {
		return true
	}
	// 账号条目没写 base_url / model 时继承 defaults.provider。
	return defaultsDeepSeek && baseURL == "" && strings.TrimSpace(account.Model) == ""
}
