// Package seelebridge — Plan graph bridge types reused from the Seele framework.
package seelebridge

import (
	"fmt"
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

// DetectCycle checks if the directed graph defined by edges contains a cycle.
// Returns an error with a cycle path when found, nil if the DAG is valid.
func DetectCycle(edges map[string][]string) error {
	// Build in-degree map
	inDegree := make(map[string]int)
	for from := range edges {
		if _, ok := inDegree[from]; !ok {
			inDegree[from] = 0
		}
		for _, to := range edges[from] {
			inDegree[to]++
		}
	}

	// Kahn's algorithm
	queue := make([]string, 0)
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}
	visited := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		visited++
		for _, next := range edges[id] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if visited != len(inDegree) {
		return fmt.Errorf("cycle detected: %d of %d nodes reachable (the rest form a cycle)", visited, len(inDegree))
	}
	return nil
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
