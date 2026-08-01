package seelebridge

import (
	"encoding/json"
	"fmt"
)

// PlanPolicy defines the runtime constraints applied to a plan_load request.
// MaxForkConcurrency is resolved per loaded plan: zero means every node in the
// plan may run concurrently, rather than Seele's default concurrency of three.
// 规划始终是模型的自愿决策，不设置聊天入口强制门槛（RequirePlan 已于 2026-08-01
// 移除：强制规划为失败设计，preflight 仅由显式 PrepareReplan 触发）。
type PlanPolicy struct {
	Effort             string
	MaxNodes           int
	RequireSerial      bool
	MaxForkConcurrency int
}

type planLoadSpec struct {
	Entry string                     `json:"entry"`
	Nodes map[string]json.RawMessage `json:"nodes"`
	Edges map[string][]string        `json:"edges"`
}

func (policy PlanPolicy) validateLoad(argsJSON string) (int, error) {
	var input planLoadSpec
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return 0, fmt.Errorf("plan policy %q: invalid plan_load JSON: %w", policy.Effort, err)
	}
	if policy.MaxNodes > 0 && len(input.Nodes) > policy.MaxNodes {
		return 0, fmt.Errorf("plan policy %q: plan has %d nodes, maximum is %d", policy.Effort, len(input.Nodes), policy.MaxNodes)
	}
	if policy.RequireSerial && !isSerialPlan(input) {
		return 0, fmt.Errorf("plan policy %q: plan must be one serial chain from entry", policy.Effort)
	}
	return len(input.Nodes), nil
}

func (policy PlanPolicy) concurrency(nodeCount int) int {
	if policy.MaxForkConcurrency > 0 {
		return policy.MaxForkConcurrency
	}
	if nodeCount > 0 {
		return nodeCount
	}
	return 1
}

func isSerialPlan(input planLoadSpec) bool {
	if len(input.Nodes) == 0 || input.Entry == "" {
		return false
	}
	if _, ok := input.Nodes[input.Entry]; !ok {
		return false
	}

	inDegree := make(map[string]int, len(input.Nodes))
	edgeCount := 0
	for from, targets := range input.Edges {
		if _, ok := input.Nodes[from]; !ok || len(targets) > 1 {
			return false
		}
		for _, to := range targets {
			if _, ok := input.Nodes[to]; !ok {
				return false
			}
			inDegree[to]++
			if inDegree[to] > 1 {
				return false
			}
			edgeCount++
		}
	}
	if edgeCount != len(input.Nodes)-1 || inDegree[input.Entry] != 0 {
		return false
	}
	for id := range input.Nodes {
		if id != input.Entry && inDegree[id] != 1 {
			return false
		}
	}

	visited := make(map[string]struct{}, len(input.Nodes))
	for current := input.Entry; ; {
		if _, seen := visited[current]; seen {
			return false
		}
		visited[current] = struct{}{}
		next := input.Edges[current]
		if len(next) == 0 {
			break
		}
		current = next[0]
	}
	return len(visited) == len(input.Nodes)
}
