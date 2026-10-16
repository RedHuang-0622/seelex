package plan

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// loadSpecJSON 把节点表与边表编码成 plan_load 的参数 JSON（与调用方传入的
// 字符串形态一致：节点值是任意 JSON 对象，仅 `budget` 字段被策略读取）。
func loadSpecJSON(t *testing.T, entry string, nodes map[string]any, edges map[string][]string) string {
	t.Helper()
	if edges == nil {
		edges = map[string][]string{}
	}
	raw, err := json.Marshal(map[string]any{"entry": entry, "nodes": nodes, "edges": edges})
	if err != nil {
		t.Fatalf("marshal plan_load spec: %v", err)
	}
	return string(raw)
}

// budgetNode 造一个只带 budget 的节点载荷（其它字段不参与上限判据）。
func budgetNode(maxLoops, maxOutputTokens int) map[string]any {
	return map[string]any{
		"kind": "bash",
		"budget": map[string]any{
			"max_loops":         maxLoops,
			"max_output_tokens": maxOutputTokens,
		},
	}
}

// chainNodes 造 n 个成链的节点（id 为 n0..n(n-1)）与对应边表。
func chainNodes(n int) (map[string]any, map[string][]string) {
	nodes := make(map[string]any, n)
	edges := make(map[string][]string, n)
	for i := 0; i < n; i++ {
		nodes[fmt.Sprintf("n%d", i)] = map[string]any{"kind": "bash"}
		if i+1 < n {
			edges[fmt.Sprintf("n%d", i)] = []string{fmt.Sprintf("n%d", i+1)}
		}
	}
	return nodes, edges
}

// TestValidatePolicyLoadNodeBudgetGuard 钉住"节点级预算上限"这条分支（P11 的覆盖缺口）：
// 判据在 backend/tool/plan/policy.go:ValidatePolicyLoad 的 MaxNodeLoops / MaxNodeOutputTokens
// 两处比较。边界口径：**上限值本身放行**，越界一档拒绝；loops 先于 tokens 判定。
func TestValidatePolicyLoadNodeBudgetGuard(t *testing.T) {
	policy := PlanPolicy{Effort: "high", MaxNodeLoops: 5, MaxNodeOutputTokens: 1000}
	cases := []struct {
		name      string
		nodes     map[string]any
		edges     map[string][]string
		wantNodes int
		wantErr   string
	}{
		{
			name:      "无 budget 字段直接放行",
			nodes:     map[string]any{"n1": map[string]any{"kind": "bash"}},
			wantNodes: 1,
		},
		{
			name:      "上限值本身放行（边界内）",
			nodes:     map[string]any{"n1": budgetNode(5, 1000)},
			wantNodes: 1,
		},
		{
			name:    "max_loops 越界一档",
			nodes:   map[string]any{"n1": budgetNode(6, 1000)},
			wantErr: `node "n1" budget max_loops=6 exceeds limit 5`,
		},
		{
			name:    "max_output_tokens 越界一档",
			nodes:   map[string]any{"n1": budgetNode(5, 1001)},
			wantErr: `node "n1" budget max_output_tokens=1001 exceeds limit 1000`,
		},
		{
			name:    "两者都越界时 loops 先判",
			nodes:   map[string]any{"n1": budgetNode(6, 1001)},
			wantErr: `max_loops=6 exceeds limit 5`,
		},
		{
			name:    "多节点中任意一个越界即拒",
			nodes:   map[string]any{"n0": budgetNode(1, 1), "n1": budgetNode(5, 5000)},
			edges:   map[string][]string{"n0": {"n1"}},
			wantErr: `node "n1" budget max_output_tokens=5000 exceeds limit 1000`,
		},
		{
			name:    "节点载荷不是 JSON 对象",
			nodes:   map[string]any{"n1": "not-json"},
			wantErr: `node "n1"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := loadSpecJSON(t, "n1", tc.nodes, tc.edges)
			got, err := ValidatePolicyLoad(policy, spec)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidatePolicyLoad 返回错误 %v，期望放行", err)
				}
				if got != tc.wantNodes {
					t.Fatalf("节点数 = %d，期望 %d", got, tc.wantNodes)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidatePolicyLoad 放行了越界预算，期望错误含 %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("错误 = %q，期望含 %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// TestValidatePolicyLoadZeroBudgetLimitsDoNotEnforce 钉住"0 = 不限"的口径：
// 策略未设上限时，节点写多大的 budget 都不该被拒。
func TestValidatePolicyLoadZeroBudgetLimitsDoNotEnforce(t *testing.T) {
	policy := PlanPolicy{Effort: "low"} // MaxNodeLoops / MaxNodeOutputTokens 均为零值
	spec := loadSpecJSON(t, "n1", map[string]any{"n1": budgetNode(100000, 10000000)}, nil)
	got, err := ValidatePolicyLoad(policy, spec)
	if err != nil {
		t.Fatalf("零值上限应放行任意 budget，实际报错 %v", err)
	}
	if got != 1 {
		t.Fatalf("节点数 = %d，期望 1", got)
	}
}

// TestValidatePolicyLoadMaxNodesBoundary 钉住 MaxNodes 的边界（上限值放行、+1 拒绝）
// 与错误文案里的节点计数。
func TestValidatePolicyLoadMaxNodesBoundary(t *testing.T) {
	policy := PlanPolicy{Effort: "high", MaxNodes: 2}
	for _, tc := range []struct {
		nodeCount int
		wantErr   string
	}{
		{nodeCount: 1},
		{nodeCount: 2},
		{nodeCount: 3, wantErr: "plan has 3 nodes, maximum is 2"},
	} {
		t.Run(fmt.Sprintf("nodes=%d", tc.nodeCount), func(t *testing.T) {
			nodes, edges := chainNodes(tc.nodeCount)
			got, err := ValidatePolicyLoad(policy, loadSpecJSON(t, "n0", nodes, edges))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidatePolicyLoad 返回错误 %v，期望放行", err)
				}
				if got != tc.nodeCount {
					t.Fatalf("节点数 = %d，期望 %d", got, tc.nodeCount)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("错误 = %v，期望含 %q", err, tc.wantErr)
			}
		})
	}
}

// TestValidatePolicyLoadMalformedArgs 钉住非法 JSON 的拒绝面（不 panic、返回可读错误）。
func TestValidatePolicyLoadMalformedArgs(t *testing.T) {
	got, err := ValidatePolicyLoad(PlanPolicy{Effort: "high"}, "not-json")
	if err == nil {
		t.Fatal("非法 plan_load JSON 应被拒")
	}
	if !strings.Contains(err.Error(), "invalid plan_load JSON") {
		t.Fatalf("错误 = %q，期望含 invalid plan_load JSON", err.Error())
	}
	if got != 0 {
		t.Fatalf("节点数 = %d，期望 0", got)
	}
}

// TestValidatePolicyLoadRequireSerial 钉住 RequireSerial 的放行/拒绝面。
func TestValidatePolicyLoadRequireSerial(t *testing.T) {
	policy := PlanPolicy{Effort: "high", RequireSerial: true}

	serial := loadSpecJSON(t, "a",
		map[string]any{"a": map[string]any{"kind": "bash"}, "b": map[string]any{"kind": "bash"}},
		map[string][]string{"a": {"b"}},
	)
	if _, err := ValidatePolicyLoad(policy, serial); err != nil {
		t.Fatalf("串行链被拒：%v", err)
	}

	branch := loadSpecJSON(t, "a",
		map[string]any{"a": map[string]any{"kind": "bash"}, "b": map[string]any{"kind": "bash"}, "c": map[string]any{"kind": "bash"}},
		map[string][]string{"a": {"b", "c"}},
	)
	if _, err := ValidatePolicyLoad(policy, branch); err == nil ||
		!strings.Contains(err.Error(), "must be one serial chain from entry") {
		t.Fatalf("分叉计划错误 = %v，期望含 serial chain 拒绝", err)
	}
}
