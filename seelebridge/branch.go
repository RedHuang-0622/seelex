package seelebridge

import (
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/session"
)

// nodeSessionComponents 构造 Plan 节点子代理会话的公共组件
// （bridge.WithSessionComponents 输入，plan.md §3.1 步骤 5）。
// Agent 由 bridge 强制覆盖为 runtime 的 agent；每节点新建独立 Session
// （工作历史默认隔离）。节点级 PromptBlocks（目标/父证据/预算）由
// SeelexAgentNode.Run 注入 ctx，装配器 nodeScopeAssembler 在每次请求时
// 合并（agent_node.go），因此组件本身保持静态、可并发共享。
func (r *Runtime) nodeSessionComponents() session.SessionComponents {
	return session.SessionComponents{
		Context:   r.nodeContextComponents(),
		Config:    session.SessionConfig{MaxLoops: r.limits.PlanNodeMaxLoops},
		Telemetry: r.hook, // 节点会话与主会话共享遥测钩子（llm/tool intent-effect）
		ModelName: r.model,
	}
}

// nodeSessionID 派生节点会话 ID：以系统提示（节点目标）为种子做稳定 hash，
// 同一节点跨 plan_run 可复现（供未来 checkpoints 定位）；空提示返回空串
// 让 Session 自动生成不透明 ID。
func (r *Runtime) nodeSessionID(systemPrompt string) string {
	if systemPrompt == "" {
		return ""
	}
	return fmt.Sprintf("node-%x", stableHash(systemPrompt))
}

// stableHash 返回 seed 的 FNV-1a 32 位稳定哈希（与 stableIndex 同族）。
func stableHash(seed string) uint32 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(seed))
	return hash.Sum32()
}

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

// resolvePlanBranchAccount 按 binding 解析分支账号：显式 AccountID 直接 pin，
// 否则按 role + seed 走确定性 hash 选择（不占用主链路租约）。
func (r *Runtime) resolvePlanBranchAccount(binding PlanBranchBinding, role AccountRole, branchID string) (string, error) {
	if binding.AccountID != "" {
		if spec := accountByName(r.accountSpecList(), binding.AccountID); spec == nil {
			return "", fmt.Errorf("plan branch %q: selected account %q is unavailable", branchID, binding.AccountID)
		}
		return binding.AccountID, nil
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

func stableIndex(seed string, size int) int {
	if size <= 1 {
		return 0
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(seed))
	return int(hash.Sum32() % uint32(size))
}
