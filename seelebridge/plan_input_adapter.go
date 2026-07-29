package seelebridge

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// NormalizePlanLoadArguments converts the LLM-friendly list forms accepted by
// plan_load into Seele's canonical object-keyed WorkPlan JSON. Canonical input
// remains supported unchanged in meaning.
func NormalizePlanLoadArguments(argsJSON string) (string, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(argsJSON), &root); err != nil {
		return "", fmt.Errorf("invalid JSON: %w", err)
	}
	for key := range root {
		if key != "entry" && key != "nodes" && key != "edges" {
			return "", fmt.Errorf("unexpected top-level field %q", key)
		}
	}
	entry, err := requiredString(root, "entry")
	if err != nil {
		return "", err
	}
	nodes, err := normalizePlanNodes(root["nodes"])
	if err != nil {
		return "", err
	}
	edges, err := normalizePlanEdges(root["edges"])
	if err != nil {
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

func normalizePlanEdges(raw json.RawMessage) (map[string][]string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("edges is required")
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
