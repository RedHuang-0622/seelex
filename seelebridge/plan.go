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
	fromIDs := make([]string, 0, len(adj))
	for from := range adj {
		fromIDs = append(fromIDs, from)
	}
	sort.Strings(fromIDs)
	for _, from := range fromIDs {
		targets := adj[from]
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

// TopoSort produces a stable topological order. The entry node is preferred
// among roots, and all other ties are resolved lexicographically.
func TopoSort(entry string, edges map[string][]string, allNodes map[string]struct{}) []string {
	inDegree := make(map[string]int, len(allNodes))
	adjacency := make(map[string][]string, len(allNodes))
	for id := range allNodes {
		inDegree[id] = 0
	}
	for from, targets := range edges {
		if _, ok := allNodes[from]; !ok {
			continue
		}
		for _, to := range targets {
			if _, ok := allNodes[to]; !ok {
				continue
			}
			adjacency[from] = append(adjacency[from], to)
			inDegree[to]++
		}
	}
	for from := range adjacency {
		sort.Strings(adjacency[from])
	}

	ready := make([]string, 0, len(allNodes))
	for id, degree := range inDegree {
		if degree == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)
	result := make([]string, 0, len(allNodes))
	emitted := make(map[string]bool, len(allNodes))
	for len(ready) > 0 {
		index := 0
		if len(result) == 0 {
			for i, id := range ready {
				if id == entry {
					index = i
					break
				}
			}
		}
		id := ready[index]
		ready = append(ready[:index], ready[index+1:]...)
		result = append(result, id)
		emitted[id] = true
		for _, next := range adjacency[id] {
			inDegree[next]--
			if inDegree[next] == 0 {
				ready = append(ready, next)
			}
		}
		sort.Strings(ready)
	}

	// A malformed cyclic graph should be rejected by the caller. Keep the
	// fallback deterministic so this helper remains total for defensive callers.
	remaining := make([]string, 0)
	for id := range allNodes {
		if !emitted[id] {
			remaining = append(remaining, id)
		}
	}
	sort.Strings(remaining)
	result = append(result, remaining...)

	return result
}
