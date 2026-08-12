package seelebridge

import (
	"github.com/RedHuang-0622/seelex/seelebridge/plan"
)

// Plan graph bridge types, re-exported from the plan subpackage for API
// compatibility with existing callers (application/model, plan_factory, etc.).
type PlanEdge = plan.PlanEdge

// AdjacencyToEdges converts an adjacency list to a PlanEdge slice.
func AdjacencyToEdges(adj map[string][]string) []PlanEdge {
	return plan.AdjacencyToEdges(adj)
}

// DetectCycle checks if the directed graph contains a cycle.
func DetectCycle(edges map[string][]string) error {
	return plan.DetectCycle(edges)
}

// TopoSort produces a stable topological order.
func TopoSort(entry string, edges map[string][]string, allNodes map[string]struct{}) []string {
	return plan.TopoSort(entry, edges, allNodes)
}
