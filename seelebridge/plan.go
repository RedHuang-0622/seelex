// Package seelebridge — Plan graph bridge types reused from the Seele framework.
package seelebridge

import (
	"sort"

	"github.com/RedHuang-0622/Seele/workplan/runtime/serialize"
)

// PlanEdge directly reuses the framework's serializable edge type.
// JSON format: {"from","to","label","condition"}.
type PlanEdge = serialize.PlanEdgeSpec

// AdjacencyToEdges converts an adjacency list (plan_load input format) to a
// PlanEdge slice suitable for PlanState.Edges.
func AdjacencyToEdges(adj map[string][]string) []PlanEdge {
	edges := make([]PlanEdge, 0)
	for from, targets := range adj {
		for _, to := range targets {
			edges = append(edges, PlanEdge{From: from, To: to})
		}
	}
	return edges
}

// TopoSort produces a stable topological order of node IDs starting from the
// entry node. Nodes not reachable from entry are appended in sorted order.
func TopoSort(entry string, edges map[string][]string, allNodes map[string]struct{}) []string {
	if entry == "" {
		result := make([]string, 0, len(allNodes))
		for id := range allNodes {
			result = append(result, id)
		}
		sort.Strings(result)
		return result
	}

	visited := make(map[string]bool)
	result := make([]string, 0, len(allNodes))

	// BFS from entry
	queue := []string{entry}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if visited[id] {
			continue
		}
		visited[id] = true
		result = append(result, id)
		for _, next := range edges[id] {
			if !visited[next] {
				queue = append(queue, next)
			}
		}
	}

	// Append any nodes not reachable from entry (disconnected subgraphs)
	remaining := make([]string, 0)
	for id := range allNodes {
		if !visited[id] {
			remaining = append(remaining, id)
		}
	}
	sort.Strings(remaining)
	result = append(result, remaining...)

	return result
}
