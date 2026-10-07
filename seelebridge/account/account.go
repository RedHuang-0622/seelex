// Package account 承载 Seelex 的账号装配与选择：从账号配置构造同步
// Completer、注册进 P2C 账号池、按 role+seed 稳定哈希解析节点账号。
// 域内不依赖 seelebridge 根包。
package account

import (
	"fmt"
	"hash/fnv"
	"net/http"

	"github.com/RedHuang-0622/Seele/accountpool"
	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/agent/core/api"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// ClientFor 从账号配置构造一个同步 Completer（agent.Completer）。
// 每个账号一个独立 client，账号选择统一走 accountpool 租赁，不做类型断言。
//
// 参数纪律（2026-10）：采样参数**不再硬编码在本文件**——temperature 与
// max_tokens 都来自账号配置（accounts.yaml 的 defaults 段 + 每个角色的每条账号
// 各自可覆盖，见 seelebridge/internal/config）。本文件只保留一条不可配置的
// 纪律：Timeout 清零。
//
// 超时纪律（2026-09-29 事故）：api.NewChatClient 把 LLMConfig.Timeout 变成
// http.Client.Timeout——**整请求 wall-clock 上限，含 SSE body 读**。长流因此会被
// 「总时长」而不是「停滞」判死，报错措辞 `…(Client.Timeout or context cancellation
// while reading body)` 还区分不了两者。这里显式清零该字段（NewChatClient 对
// Timeout<=0 会回落到 60s，所以必须清在构造之后——见 seelebridge/account/README.md），
// 改用 Transport 级看门狗：响应头超时 + body 空闲看门狗（transport.go）。
func ClientFor(spec model.AccountSpec) agent.Completer {
	client := api.NewChatClient(types.LLMConfig{
		BaseURL: spec.BaseURL, APIKey: spec.APIKey, Model: spec.Model,
		MaxTokens: spec.MaxTokens, Temperature: spec.Temperature,
		ReasoningEffort: model.WireReasoningEffort(spec.ReasoningEffort),
	})
	client.Client.Timeout = 0
	client.Client.Transport = newStreamTransport(http.DefaultTransport)
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

// leastBusyForRole 返回指定角色下当前还有并发余量的账号（余量最大者）；
// 全部占满或无账号时返回 false。并发 fork/plan 节点用它避免被确定性哈希
// 全部钉到同一账号而排队（真实双 subagent 阻塞根因）。
func leastBusyForRole(pool *accountpool.P2CPool[agent.Completer], role model.AccountRole) (string, bool) {
	if pool == nil {
		return "", false
	}
	bestID := ""
	bestAvailable := 0
	for _, entry := range ForRole(pool, role) {
		snapshot := entry.Snapshot
		if snapshot.Disabled || snapshot.Available <= 0 {
			continue
		}
		if bestID == "" || snapshot.Available > bestAvailable {
			bestID = snapshot.ID
			bestAvailable = snapshot.Available
		}
	}
	if bestID == "" {
		return "", false
	}
	return bestID, true
}

// SetSessionReasoningEffort 把**配置为跟随会话**的账号调到 effort 指定的思考强度，
// 返回实际被改动的账号数（0 = 没有账号跟随会话，或池是空的）。
//
// 只碰 spec.ReasoningEffort == model.ReasoningEffortSession 的账号：写死了强度的
// 角色（subagent 默认 low、goalplan 默认 high，或用户在账号条目里显式配置的）
// 不受会话档位影响——"跟随会话"是显式选择，不是"所有账号跟着一起变"。
//
// effort 是 provider 词表的 wire 值（low/medium/high/max）；空串 = 不下发。
// 线程安全由 *api.ChatClient 自己的读锁保证，可在请求在途时调用。
//
// 为什么类型断言收在这里：账号池存的是 agent.Completer 接口，而"改思考强度"只有
// Seele 的 *api.ChatClient 支持。按既有纪律"账号选择不做类型断言"，断言被收在
// 这**一个**函数里（装配层），不散到调用点。
func SetSessionReasoningEffort(pool *accountpool.P2CPool[agent.Completer], specs []model.AccountSpec, effort string) int {
	if pool == nil {
		return 0
	}
	changed := 0
	for _, entry := range pool.Entries() {
		spec := ByName(specs, entry.Snapshot.ID)
		if spec == nil || spec.ReasoningEffort != model.ReasoningEffortSession {
			continue
		}
		client, ok := entry.Value.(*api.ChatClient)
		if !ok {
			continue
		}
		client.SetReasoningEffort(effort)
		changed++
	}
	return changed
}
