package seelebridge

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/agent/core/api"
	"github.com/RedHuang-0622/Seele/agent/core/tool/builtin"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/Seele/workplan/core/node"
	"github.com/RedHuang-0622/Seele/workplan/runtime/forkexec"
)

// PlanBranchEvent is the Seelex-owned representation of a branch lifecycle
// event. It intentionally contains no Seele runtime types.
type PlanBranchEvent struct {
	Type     string
	BranchID string
	NodeID   string
	Error    string
	At       time.Time
}

// PlanBranchBinding freezes the request-scoped values used to construct
// branch runtimes. Empty AccountID delegates selection to the role router.
type PlanBranchBinding struct {
	SessionID   string
	WorkspaceID string
	PlanID      string
	EntryNodeID string
	AccountID   string
	PrimaryRole AccountRole
	TraceID     string
}

func (r *Runtime) setPlanBranchBinding(binding PlanBranchBinding) {
	r.branchMu.Lock()
	defer r.branchMu.Unlock()
	if binding.AccountID == "" {
		binding.AccountID = r.selectedAccountID
	}
	if binding.PrimaryRole == "" {
		binding.PrimaryRole = RoleAgent
	}
	if binding.PlanID == "" {
		binding.PlanID = binding.EntryNodeID
	}
	r.branchBinding = binding
}

func (r *Runtime) currentPlanBranchBinding() PlanBranchBinding {
	r.branchMu.RLock()
	defer r.branchMu.RUnlock()
	return r.branchBinding
}

func (r *Runtime) setSelectedAccount(name string) {
	r.branchMu.Lock()
	r.selectedAccountID = name
	r.branchMu.Unlock()
}

func (r *Runtime) resolvePlanBranchRuntime(branchID string) forkexec.BranchRuntime {
	binding := r.currentPlanBranchBinding()
	if binding.SessionID == "" && binding.PlanID == "" && binding.AccountID == "" {
		return forkexec.BranchRuntime{}
	}
	role := roleForPlanBranch(binding, branchID)
	account, err := r.resolvePlanBranchAccount(binding, role, branchID)
	runtime := forkexec.BranchRuntime{
		SessionID:   binding.SessionID,
		WorkspaceID: binding.WorkspaceID,
		Role:        string(role),
		TraceID:     branchTraceID(binding, branchID),
	}
	if err != nil {
		runtime.AgentFactory = unavailableAgentFactory{err: err}
		return runtime
	}
	runtime.AccountID = account.Name
	runtime.Provider = string(account.Provider)
	runtime.AgentFactory = newBranchAgentFactory(account, r.client.Cfg)
	return runtime
}

func (r *Runtime) resolvePlanBranchAccount(binding PlanBranchBinding, role AccountRole, branchID string) (*api.Account, error) {
	if binding.AccountID != "" {
		account := accountByName(r.pool, binding.AccountID)
		if account == nil {
			return nil, fmt.Errorf("plan branch %q: selected account %q is unavailable", branchID, binding.AccountID)
		}
		return account, nil
	}
	return ResolveAccountForBranch(r.pool, role, binding.PlanID+":"+branchID)
}

func roleForPlanBranch(binding PlanBranchBinding, branchID string) AccountRole {
	if branchID == binding.EntryNodeID {
		return binding.PrimaryRole
	}
	if strings.HasPrefix(branchID, "_") {
		return RoleGoalPlan
	}
	return RoleSubAgent
}

func branchTraceID(binding PlanBranchBinding, branchID string) string {
	if binding.TraceID == "" {
		return branchID
	}
	return binding.TraceID + ":" + branchID
}

func newBranchAgentFactory(account *api.Account, defaults types.LLMConfig) node.AgentFactory {
	config := defaults
	config.BaseURL = account.BaseURL
	config.APIKey = account.APIKey
	config.Model = account.Model
	client := api.NewChatClient(config).
		WithAccountPool(api.NewAccountPool(account)).
		SetProvider(account.Provider)
	return builtin.NewChatAgentFactory(client)
}

type unavailableAgentFactory struct{ err error }

func (f unavailableAgentFactory) NewAgent(string) node.Agent {
	return unavailableAgent{err: f.err}
}

type unavailableAgent struct{ err error }

func (a unavailableAgent) Chat(context.Context, string) (string, error) {
	return "", a.err
}

// ResolveAccountForBranch selects an account without mutating the shared pool
// cursor. The same role and seed always resolve to the same configured account.
func ResolveAccountForBranch(pool *api.AccountPool, role AccountRole, seed string) (*api.Account, error) {
	accounts := accountsForRole(pool, role)
	if len(accounts) == 0 {
		return nil, fmt.Errorf("seelebridge: no accounts available")
	}
	return accounts[stableIndex(seed, len(accounts))], nil
}

func accountsForRole(pool *api.AccountPool, role AccountRole) []*api.Account {
	if pool == nil {
		return nil
	}
	all := pool.All()
	roles := append([]AccountRole{role}, fallbackRoles(role)...)
	for _, candidate := range roles {
		matched := make([]*api.Account, 0)
		for _, account := range all {
			if !account.Disabled && accountRole(account.Name) == candidate {
				matched = append(matched, account)
			}
		}
		if len(matched) > 0 {
			return matched
		}
	}
	for _, account := range all {
		if !account.Disabled {
			return []*api.Account{account}
		}
	}
	return nil
}

func fallbackRoles(role AccountRole) []AccountRole {
	switch role {
	case RoleGoalPlan, RoleSubAgent:
		return []AccountRole{RoleAgent}
	default:
		return nil
	}
}

func accountByName(pool *api.AccountPool, name string) *api.Account {
	if pool == nil {
		return nil
	}
	for _, account := range pool.All() {
		if account.Name == name && !account.Disabled {
			return account
		}
	}
	return nil
}

func stableIndex(seed string, size int) int {
	if size <= 1 {
		return 0
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(seed))
	return int(hash.Sum32() % uint32(size))
}
