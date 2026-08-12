package plan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// NormalizePlanLoadArguments converts the LLM-friendly list forms accepted by
// plan_load into Seele's canonical object-keyed WorkPlan JSON. Canonical input
// remains supported unchanged in meaning.
func NormalizePlanLoadArguments(argsJSON string) (string, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(argsJSON), &root); err != nil {
		return "", fmt.Errorf("invalid JSON: %w", err)
	}
	entry, err := requiredString(root, "entry")
	if err != nil {
		return "", err
	}
	nodes, err := normalizePlanNodes(root["nodes"])
	if err != nil {
		return "", err
	}
	edges, err := normalizePlanEdges(root["edges"], entry)
	if err != nil {
		return "", err
	}
	if err := mergeReferencedTopLevelNodes(root, entry, nodes, edges); err != nil {
		return "", err
	}
	if _, ok := nodes[entry]; !ok {
		return "", fmt.Errorf("entry %q is not a nodes key", entry)
	}
	for from, targets := range edges {
		if _, ok := nodes[from]; !ok {
			return "", fmt.Errorf("edge source %q is not a nodes key", from)
		}
		for _, target := range targets {
			if _, ok := nodes[target]; !ok {
				return "", fmt.Errorf("edge target %q is not a nodes key", target)
			}
		}
	}
	canonical, err := json.Marshal(struct {
		Entry string                     `json:"entry"`
		Nodes map[string]json.RawMessage `json:"nodes"`
		Edges map[string][]string        `json:"edges"`
	}{Entry: entry, Nodes: nodes, Edges: edges})
	if err != nil {
		return "", fmt.Errorf("encode canonical plan: %w", err)
	}
	return string(canonical), nil
}

// mergeReferencedTopLevelNodes accepts one constrained legacy model shape:
// node specifications accidentally placed beside nodes. It accepts only an
// object with input/kind fields whose key is already named by entry or edges.
// Every other top-level field remains an error, so metadata such as item
// cannot silently become an executable node.
func mergeReferencedTopLevelNodes(root map[string]json.RawMessage, entry string, nodes map[string]json.RawMessage, edges map[string][]string) error {
	referenced := map[string]struct{}{entry: {}}
	for from, targets := range edges {
		referenced[from] = struct{}{}
		for _, target := range targets {
			referenced[target] = struct{}{}
		}
	}
	for key, raw := range root {
		if key == "entry" || key == "nodes" || key == "edges" {
			continue
		}
		if _, ok := referenced[key]; !ok {
			return fmt.Errorf("unexpected top-level field %q", key)
		}
		if _, exists := nodes[key]; exists {
			return fmt.Errorf("node %q is defined both in nodes and at the top level", key)
		}
		node, err := normalizeTopLevelNodeSpec(key, raw)
		if err != nil {
			return err
		}
		nodes[key] = node
	}
	return nil
}

func normalizeTopLevelNodeSpec(id string, raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("top-level node %q must be an object: %w", id, err)
	}
	for field := range fields {
		if field != "input" && field != "kind" {
			return nil, fmt.Errorf("top-level node %q has unexpected field %q", id, field)
		}
	}
	var node struct {
		Input string `json:"input"`
		Kind  string `json:"kind,omitempty"`
	}
	if err := json.Unmarshal(raw, &node); err != nil || node.Input == "" {
		return nil, fmt.Errorf("top-level node %q must include a non-empty input", id)
	}
	if node.Kind != "" && node.Kind != "auto" && node.Kind != "manual" {
		return nil, fmt.Errorf("top-level node %q has invalid kind %q", id, node.Kind)
	}
	canonical, err := json.Marshal(node)
	if err != nil {
		return nil, fmt.Errorf("encode top-level node %q: %w", id, err)
	}
	return canonical, nil
}

func requiredString(root map[string]json.RawMessage, field string) (string, error) {
	raw, ok := root[field]
	if !ok {
		return "", fmt.Errorf("%s is required", field)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || value == "" {
		return "", fmt.Errorf("%s must be a non-empty string", field)
	}
	return value, nil
}

func normalizePlanNodes(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("nodes is required")
	}
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		var nodes map[string]json.RawMessage
		if err := json.Unmarshal(raw, &nodes); err != nil {
			return nil, fmt.Errorf("nodes object: %w", err)
		}
		return nodes, nil
	}
	var items []struct {
		ID    string `json:"id"`
		Key   string `json:"key"`
		Input string `json:"input"`
		Kind  string `json:"kind,omitempty"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("nodes must be an object or an array: %w", err)
	}
	nodes := make(map[string]json.RawMessage, len(items))
	for index, item := range items {
		id := item.ID
		if id == "" {
			id = item.Key
		}
		if id == "" || item.Input == "" {
			return nil, fmt.Errorf("nodes[%d] must include id (or key) and input", index)
		}
		if _, exists := nodes[id]; exists {
			return nil, fmt.Errorf("nodes contains duplicate ID %q", id)
		}
		node, err := json.Marshal(struct {
			Input string `json:"input"`
			Kind  string `json:"kind,omitempty"`
		}{Input: item.Input, Kind: item.Kind})
		if err != nil {
			return nil, fmt.Errorf("encode node %q: %w", id, err)
		}
		nodes[id] = node
	}
	return nodes, nil
}

func normalizePlanEdges(raw json.RawMessage, entry string) (map[string][]string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("edges is required")
	}
	var chain string
	if err := json.Unmarshal(raw, &chain); err == nil {
		return normalizeEdgeChain(chain)
	}
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		var sources map[string]json.RawMessage
		if err := json.Unmarshal(raw, &sources); err != nil {
			return nil, fmt.Errorf("edges object: %w", err)
		}
		edges := make(map[string][]string, len(sources))
		for from, targets := range sources {
			normalized, err := normalizeEdgeTargets(targets)
			if err != nil {
				return nil, fmt.Errorf("edges[%q]: %w", from, err)
			}
			edges[from] = normalized
		}
		return edges, nil
	}
	var orderedTargets []string
	if err := json.Unmarshal(raw, &orderedTargets); err == nil {
		return normalizeOrderedEdgeTargets(entry, orderedTargets)
	}
	var items []struct {
		From   string `json:"from"`
		Source string `json:"source"`
		To     string `json:"to"`
		Target string `json:"target"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("edges must be an object or an array: %w", err)
	}
	edges := make(map[string][]string)
	for index, item := range items {
		from, target := item.From, item.To
		if from == "" {
			from = item.Source
		}
		if target == "" {
			target = item.Target
		}
		if from == "" || target == "" {
			return nil, fmt.Errorf("edges[%d] must include from (or source) and to (or target)", index)
		}
		edges[from] = append(edges[from], target)
	}
	return edges, nil
}

// normalizeOrderedEdgeTargets recovers a common LLM shorthand such as
// "edges": ["implement", "verify"]. The entry supplies the omitted first
// source, so the only accepted interpretation is the serial chain
// entry -> implement -> verify. It intentionally does not infer a branching
// graph from an array of names.
func normalizeOrderedEdgeTargets(entry string, targets []string) (map[string][]string, error) {
	edges := make(map[string][]string, len(targets))
	seen := map[string]struct{}{entry: {}}
	previous := entry
	for index, target := range targets {
		target = strings.TrimSpace(target)
		if target == "" {
			return nil, fmt.Errorf("edges[%d] must be a non-empty node ID", index)
		}
		if target == entry && index == 0 {
			continue
		}
		if _, exists := seen[target]; exists {
			return nil, fmt.Errorf("edges ordered target %q is repeated", target)
		}
		seen[target] = struct{}{}
		edges[previous] = append(edges[previous], target)
		previous = target
	}
	return edges, nil
}

// normalizeEdgeChain accepts only an explicit, ordered recovery chain such as
// "inspect -> verify -> report". Unlike a partial edge object, every source
// and target is present in the string, so converting adjacent IDs does not
// invent dependencies or execution order.
func normalizeEdgeChain(chain string) (map[string][]string, error) {
	parts := strings.Split(chain, "->")
	if len(parts) < 2 {
		return nil, fmt.Errorf("edges string must be an explicit chain such as \"inspect -> verify\"")
	}
	edges := make(map[string][]string, len(parts)-1)
	previous := strings.TrimSpace(parts[0])
	if previous == "" {
		return nil, fmt.Errorf("edges string contains an empty node ID")
	}
	for _, part := range parts[1:] {
		target := strings.TrimSpace(part)
		if target == "" {
			return nil, fmt.Errorf("edges string contains an empty node ID")
		}
		edges[previous] = append(edges[previous], target)
		previous = target
	}
	return edges, nil
}

func normalizeEdgeTargets(raw json.RawMessage) ([]string, error) {
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("targets must be an array: %w", err)
	}
	targets := make([]string, 0, len(values))
	for index, value := range values {
		var target string
		if err := json.Unmarshal(value, &target); err == nil && target != "" {
			targets = append(targets, target)
			continue
		}
		var item struct {
			To     string `json:"to"`
			Target string `json:"target"`
		}
		if err := json.Unmarshal(value, &item); err != nil {
			return nil, fmt.Errorf("target %d must be a string or object: %w", index, err)
		}
		target = item.To
		if target == "" {
			target = item.Target
		}
		if target == "" {
			return nil, fmt.Errorf("target %d must include to (or target)", index)
		}
		targets = append(targets, target)
	}
	return targets, nil
}
