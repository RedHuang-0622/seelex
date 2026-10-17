package plan

// branch_routing_test.go 是 Plan 分支路由三条判据的域级用例（P21-30：随实现从
// `backend/engine/assemble/runtime_plan.go` 搬进本域）。

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/model"
)

// TestBranchTraceIDCarriesTraceWhenPresent：追踪 ID = `traceID:branchID`；绑定没给 traceID 时
// 退化成 branchID 本身（不凭空造前缀）。
func TestBranchTraceIDCarriesTraceWhenPresent(t *testing.T) {
	if got := BranchTraceID(dto.PlanBranchBinding{TraceID: "trace-1"}, "left"); got != "trace-1:left" {
		t.Fatalf("追踪 ID = %q, want trace-1:left", got)
	}
	if got := BranchTraceID(dto.PlanBranchBinding{}, "left"); got != "left" {
		t.Fatalf("没有 traceID 时就是分支名：%q", got)
	}
}

// TestStableHashIsStable：同 seed 恒等、异 seed 一般不同（稳定性是契约：换算法就换 nodeID）。
func TestStableHashIsStable(t *testing.T) {
	if StableHash("节点目标") != StableHash("节点目标") {
		t.Fatal("同 seed 必须同值")
	}
	// FNV-1a 32 位在 1000 个不同种子上几乎不可能碰撞：抽两条明确不同的输入做哨兵。
	if StableHash("a") == StableHash("b") {
		t.Fatal("不同 seed 不该同值")
	}
}

// TestNodeSessionIDDerivesFromPrompt：节点会话号由提示词派生（`node-<hex>`）；空提示**返回空串**
// （让 Session 自动生成不透明 ID，而不是造一个所有空提示共用的会话号）。
func TestNodeSessionIDDerivesFromPrompt(t *testing.T) {
	first := NodeSessionID("实现渲染件")
	if !strings.HasPrefix(first, "node-") || len(first) <= len("node-") {
		t.Fatalf("节点会话号形状 = %q", first)
	}
	if NodeSessionID("实现渲染件") != first {
		t.Fatal("同一提示必须派生出同一个会话号（plan_run 可复现）")
	}
	if NodeSessionID("另一件事") == first {
		t.Fatal("不同提示该派生出不同会话号")
	}
	if got := NodeSessionID(""); got != "" {
		t.Fatalf("空提示应当返回空串：%q", got)
	}
}

// TestResolveBranchAccountPinnedMustExist：绑定钉住账号时**校验它在册**——未知名显式报错
// （文案里带分支名与账号名），在册则原样返回钉住的那个。
func TestResolveBranchAccountPinnedMustExist(t *testing.T) {
	specs := []model.AccountSpec{{Name: "agent-1"}}
	got, err := ResolveBranchAccount(specs, nil, dto.PlanBranchBinding{AccountID: "agent-1"}, model.RoleAgent, "left")
	if err != nil || got != "agent-1" {
		t.Fatalf("在册的钉住账号应当原样返回：%q %v", got, err)
	}
	_, err = ResolveBranchAccount(specs, nil, dto.PlanBranchBinding{AccountID: "ghost"}, model.RoleAgent, "left")
	if err == nil || !strings.Contains(err.Error(), `branch "left" pins unknown account "ghost"`) {
		t.Fatalf("未知名必须显式报错且点名两边：%v", err)
	}
}

// TestResolveBranchAccountWithoutPinFallsBackToRoleRouter：没钉账号时交给账号域按**角色 + 计划分支种子**
// 解析；没有可用账号时把账号域的报错原样上抛（不吞、不静默换成空账号）。
func TestResolveBranchAccountWithoutPinFallsBackToRoleRouter(t *testing.T) {
	_, err := ResolveBranchAccount(nil, nil, dto.PlanBranchBinding{PlanID: "plan-1"}, model.RoleAgent, "left")
	if err == nil || !strings.Contains(err.Error(), "no accounts available") {
		t.Fatalf("没有账号池时应当把账号侧的报错上抛：%v", err)
	}
}
