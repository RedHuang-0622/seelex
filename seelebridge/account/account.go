// Package account 承载 Seelex 的账号装配与选择：从账号配置构造同步
// Completer、注册进 P2C 账号池、按 role+seed 稳定哈希解析节点账号。
// 域内不依赖 seelebridge 根包。
package account

import (
	"fmt"
	"hash/fnv"

	"github.com/RedHuang-0622/Seele/accountpool"
	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/agent/core/api"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// ClientFor 从账号配置构造一个同步 Completer（agent.Completer）。
// 每个账号一个独立 client，账号选择统一走 accountpool 租赁，不做类型断言。
func ClientFor(spec model.AccountSpec) agent.Completer {
	client := api.NewChatClient(types.LLMConfig{
		BaseURL: spec.BaseURL, APIKey: spec.APIKey, Model: spec.Model,
		MaxTokens: spec.MaxTokens, Timeout: 300, Temperature: 0.7,
	})
	client.SetProvider(api.ProviderType(spec.Provider))
	return client
}

// RegisterAccounts 把加载的账号配置注册进 P2C 账号池。
// Metadata 只放非敏感路由属性（provider/model），凭据留在 Value 内部。
func RegisterAccounts(pool *accountpool.P2CPool[agent.Completer], specs []model.AccountSpec) error {
	for _, spec := range specs {
		if err := pool.Register(accountpool.Account[agent.Completer]{
			ID:             spec.Name,
			Value:          ClientFor(spec),
			MaxConcurrency: spec.MaxConcurrency,
			Metadata: map[string]string{
				"provider": spec.Provider,
				"model":    spec.Model,
			},
		}); err != nil {
			return fmt.Errorf("seelebridge: register account %q: %w", spec.Name, err)
		}
	}
	return nil
}

// ResolveForBranch selects an account without mutating the shared pool
// cursor. The same role and seed always resolve to the same configured account.
func ResolveForBranch(pool *accountpool.P2CPool[agent.Completer], role model.AccountRole, seed string) (string, error) {
	entries := ForRole(pool, role)
	if len(entries) == 0 {
		return "", fmt.Errorf("seelebridge: no accounts available")
	}
	return entries[StableIndex(seed, len(entries))].Snapshot.ID, nil
}

// ForRole 按角色（含回退角色链）筛选可用账号；无匹配时回退任意启用账号。
func ForRole(pool *accountpool.P2CPool[agent.Completer], role model.AccountRole) []accountpool.Entry[agent.Completer] {
	if pool == nil {
		return nil
	}
	all := pool.Entries()
	roles := append([]model.AccountRole{role}, model.FallbackRoles(role)...)
	for _, candidate := range roles {
		matched := make([]accountpool.Entry[agent.Completer], 0)
		for _, entry := range all {
			if !entry.Snapshot.Disabled && model.AccountRoleFromName(entry.Snapshot.ID) == candidate {
				matched = append(matched, entry)
			}
		}
		if len(matched) > 0 {
			return matched
		}
	}
	for _, entry := range all {
		if !entry.Snapshot.Disabled {
			return []accountpool.Entry[agent.Completer]{entry}
		}
	}
	return nil
}

// ByName 在账号规格列表中按名称查找（返回内部指针；nil = 未找到）。
func ByName(specs []model.AccountSpec, name string) *model.AccountSpec {
	for index := range specs {
		if specs[index].Name == name {
			return &specs[index]
		}
	}
	return nil
}

// StableIndex 返回 seed 的 FNV-1a 32 位稳定哈希索引（同 seed 同 size 恒等）。
func StableIndex(seed string, size int) int {
	if size <= 1 {
		return 0
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(seed))
	return int(hash.Sum32() % uint32(size))
}
