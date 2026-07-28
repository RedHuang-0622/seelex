package seelebridge

import (
	"github.com/RedHuang-0622/Seele/agent/core/tool/builtin"
	"github.com/RedHuang-0622/Seele/agent/core/tool/interfaces"
)

const planLoadContractDescription = `

Strict JSON contract:
- nodes MUST be an object keyed by node ID. Never send nodes as a JSON array.
- The object key is the node ID. Do not add a key field inside a node.
- edges is an adjacency object: each source node maps to an array of successor node IDs.

Invalid example (do not use):
{"entry":"search","nodes":[{"key":"search","input":"find files"}],"edges":{}}

Valid example:
{"entry":"search","nodes":{"search":{"input":"find files"},"summarize":{"input":"summarize the file list"}},"edges":{"search":["summarize"]}}
`

// planToolProvider decorates Seele's WorkPlan handlers with Seelex's explicit
// plan_load contract. Handlers remain framework-owned; only LLM-facing schema
// and description are enriched.
type planToolProvider struct {
	tool *builtin.WorkPlanTool
}

func (provider *planToolProvider) ProviderName() string { return "seelex-workplan" }

func (provider *planToolProvider) Tools() []interfaces.ToolEntry {
	entries := provider.tool.Tools()
	for index := range entries {
		if entries[index].Definition.Function.Name == "plan_load" {
			entries[index] = enrichPlanLoadEntry(entries[index])
		}
	}
	return entries
}

func enrichPlanLoadEntry(entry interfaces.ToolEntry) interfaces.ToolEntry {
	entry.Definition.Function.Description += planLoadContractDescription
	properties, ok := entry.Definition.Function.Parameters["properties"].(map[string]interface{})
	if !ok {
		return entry
	}
	nodes, ok := properties["nodes"].(map[string]interface{})
	if !ok {
		return entry
	}
	nodes["description"] = "Object keyed by node ID; never a JSON array. Each value has required string input and optional kind."
	nodes["additionalProperties"] = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"input": map[string]interface{}{"type": "string"},
			"kind":  map[string]interface{}{"type": "string", "enum": []string{"auto", "manual"}},
		},
		"required": []string{"input"},
	}
	return entry
}
