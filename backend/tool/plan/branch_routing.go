package plan

// branch_routing.go 收纳 Plan **分支路由**的三条判据（P21-30「域方法随类型走」：从
// `backend/engine/assemble/runtime_plan.go` 搬进 Plan 域；plan §2.6 把 `seelebridge/runtime_plan.go`
// 列为 `backend/tool/plan/` 的目标）。
//
// 三条都是"由绑定与分支标识推出一个稳定标识/账号"的换算：追踪 ID、节点会话号、分支账号。
// 宿主读数（账号规格表、账号池）以**值/端口**传入——本包不认识 Runtime。

import (
	"fmt"
	"hash/fnv"

	"github.com/RedHuang-0622/Seele/accountpool"
	"github.com/RedHuang-0622/Seele/agent"

	"github.com/RedHuang-0622/seelex/backend/engine/account"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/model"
)

// BranchTraceID 返回分支追踪 ID（`traceID:branchID`；绑定没给 traceID 时就是 branchID）。
func BranchTraceID(binding dto.PlanBranchBinding, branchID string) string {
	if binding.TraceID == "" {
		return branchID
	}
	return binding.TraceID + ":" + branchID
}

// StableHash 返回 seed 的 FNV-1a 32 位稳定哈希（同 seed 恒等）。
//
// 它服务"由提示词派生节点会话号"这一条路（见 NodeSessionID）：稳定性是契约——换算法就换 nodeID，
// 从而换掉运行期复用与后续定位的键。
func StableHash(seed string) uint32 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(seed))
	return hash.Sum32()
}

// NodeSessionID 派生节点会话 ID：以系统提示（节点目标）为种子做稳定 hash；同一节点路径
// `plan_run` 可复现（供 checkpoints 定位）；空提示返回空串，让 Session 自动生成不透明 ID。
func NodeSessionID(systemPrompt string) string {
	if systemPrompt == "" {
		return ""
	}
	return fmt.Sprintf("node-%x", StableHash(systemPrompt))
}

// ResolveBranchAccount 解析分支账号：绑定**钉住了账号**时校验它在册（未知名显式报错，不静默回退），
// 否则按角色 + `planID:branchID` 种子交给账号域解析。
//
// 两个宿主读数都是值/端口：`specs` 是账号规格表（校验钉住的名字用），`pool` 是账号池
// （按角色选账号用）。返回的是**账号名**，不是句柄。
func ResolveBranchAccount(
	specs []model.AccountSpec,
	pool *accountpool.P2CPool[agent.Completer],
	binding dto.PlanBranchBinding,
	role model.AccountRole,
	branchID string,
) (string, error) {
	if binding.AccountID != "" {
		if spec := account.ByName(specs, binding.AccountID); spec == nil {
			return "", fmt.Errorf("branch %q pins unknown account %q", branchID, binding.AccountID)
		}
		return binding.AccountID, nil
	}
	return account.ResolveForBranch(pool, role, binding.PlanID+":"+branchID)
}
