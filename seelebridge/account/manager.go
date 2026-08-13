package account

import (
	"context"
	"sort"
	"sync"

	"github.com/RedHuang-0622/Seele/accountpool"
	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/config"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// Account 是账号路由摘要（GUI/快照消费）。
type Account struct {
	Name     string
	Provider string
	Model    string
	Disabled bool
}

// Manager 收拢 Runtime 的账号路由状态：账号规格、限额、选中账号与 provider
// 过滤、P2C 池引用。自带锁，跨域协作经 Selector 闭包注入。
type Manager struct {
	mu             sync.RWMutex
	specs          map[string]model.AccountSpec
	limits         map[string]config.AccountLimits
	defaultID      string
	selectedID     string
	providerFilter string
	pool           *accountpool.P2CPool[agent.Completer]
}

// NewManager 构造账号管理器。
func NewManager(specs []model.AccountSpec, limits map[string]config.AccountLimits, defaultID string, pool *accountpool.P2CPool[agent.Completer]) *Manager {
	m := &Manager{
		specs:     make(map[string]model.AccountSpec, len(specs)),
		limits:    limits,
		defaultID: defaultID,
		pool:      pool,
	}
	for _, spec := range specs {
		m.specs[spec.Name] = spec
	}
	return m
}

// Select 切换主链路选中账号（provider 过滤跟随账号规格）；未知账号返回 false。
func (m *Manager) Select(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	spec, ok := m.specs[name]
	if !ok {
		return false
	}
	m.selectedID = spec.Name
	m.providerFilter = spec.Provider
	return true
}

// SetSelected 设置选中账号（不校验规格；plan 分支冻结路径用）。
func (m *Manager) SetSelected(name string) {
	m.mu.Lock()
	m.selectedID = name
	m.mu.Unlock()
}

// Selected 返回当前选中账号。
func (m *Manager) Selected() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.selectedID
}

// SetProvider 切换 provider 过滤；非空时清除固定账号，让 P2C 在过滤集内选择。
func (m *Manager) SetProvider(provider string) {
	m.mu.Lock()
	m.providerFilter = provider
	if provider != "" {
		m.selectedID = ""
	}
	m.mu.Unlock()
}

// Provider 返回当前 provider 过滤；未过滤时回退默认账号的 provider。
func (m *Manager) Provider() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.providerFilter != "" {
		return m.providerFilter
	}
	if spec, ok := m.specs[m.defaultID]; ok {
		return spec.Provider
	}
	return ""
}

// Accounts 返回账号池条目的路由摘要（按名称排序）。
func (m *Manager) Accounts() []Account {
	if m.pool == nil {
		return nil
	}
	entries := m.pool.Entries()
	result := make([]Account, 0, len(entries))
	for _, entry := range entries {
		result = append(result, Account{
			Name:     entry.Snapshot.ID,
			Provider: entry.Snapshot.Metadata["provider"],
			Model:    entry.Snapshot.Metadata["model"],
			Disabled: entry.Snapshot.Disabled,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// Specs 返回账号规格列表（只读拷贝）。
func (m *Manager) Specs() []model.AccountSpec {
	m.mu.RLock()
	defer m.mu.RUnlock()
	specs := make([]model.AccountSpec, 0, len(m.specs))
	for _, spec := range m.specs {
		specs = append(specs, spec)
	}
	return specs
}

// Limits 返回当前账号（选中或默认）的限额；未知回退默认值。
func (m *Manager) Limits() config.AccountLimits {
	m.mu.RLock()
	accountID := m.selectedID
	if accountID == "" {
		accountID = m.defaultID
	}
	limits, ok := m.limits[accountID]
	m.mu.RUnlock()
	if ok {
		return limits
	}
	return config.AccountLimits{ContextWindow: config.DefaultContextWindow, MaxOutputTokens: config.DefaultMaxOutputTokens}
}

// SelectorDeps 是 Selector 需要的跨域闭包（plan 分支绑定读取）。
type SelectorDeps struct {
	BranchBinding func() dto.PlanBranchBinding
}

// Selector 返回主链路共享的账号选择器闭包：把请求条件转换为
// accountpool.AcquireRequest。P2C 池负责实际选择与租赁。
// Plan 子代理请求（ctx 含 NodeScope）优先走节点账号解析：显式 pin 优先，
// 否则按角色 + branchID 走确定性 hash（ResolveForBranch 逻辑）。
func (m *Manager) Selector(deps SelectorDeps) func(ctx context.Context, messages []types.Message, tools []types.Tool) accountpool.AcquireRequest {
	return func(ctx context.Context, _ []types.Message, _ []types.Tool) accountpool.AcquireRequest {
		if scope := model.NodeScopeFromContextOrEmpty(ctx); scope.NodeID != "" {
			return m.nodeRequest(deps, scope)
		}
		m.mu.RLock()
		selected := m.selectedID
		provider := m.providerFilter
		m.mu.RUnlock()
		request := accountpool.AcquireRequest{}
		if selected != "" {
			request.AccountID = selected
		}
		if provider != "" {
			if request.Metadata == nil {
				request.Metadata = make(map[string]string)
			}
			request.Metadata["provider"] = provider
		}
		return request
	}
}

// nodeRequest 按节点作用域解析账号租赁请求：binding 显式 AccountID 直接
// pin；否则按 role + planID:branchID 确定性 hash 选择。
func (m *Manager) nodeRequest(deps SelectorDeps, scope model.NodeScope) accountpool.AcquireRequest {
	request := accountpool.AcquireRequest{}
	if deps.BranchBinding != nil {
		binding := deps.BranchBinding()
		if binding.AccountID != "" {
			request.AccountID = binding.AccountID
			return request
		}
		seed := scope.BranchID
		if seed == "" {
			seed = scope.NodeID
		}
		accountID, err := ResolveForBranch(m.pool, scope.Role, binding.PlanID+":"+seed)
		if err == nil {
			request.AccountID = accountID
		}
	}
	return request
}
