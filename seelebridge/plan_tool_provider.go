package seelebridge

import (
	"context"
	"sync"

	"github.com/RedHuang-0622/Seele/agent/core/tool/builtin"
	"github.com/RedHuang-0622/Seele/agent/core/tool/interfaces"
)

const planLoadContractDescription = `

Strict JSON contract:
- Use only these top-level fields: entry, nodes, and edges. Do not use item.
- nodes MUST be an object keyed by node ID. Never send nodes as a JSON array.
- The object key is the node ID. Do not add a key field inside a node.
- edges MUST be an object keyed by source node ID. Each value is an array of target node ID strings, never edge objects.
- Every ID named by entry or edges must be a key in nodes.

Invalid nodes example (do not use):
{"entry":"search","nodes":[{"key":"search","input":"find files"}],"edges":{}}

Invalid edges examples (do not use):
{"entry":"search","nodes":{"search":{"input":"find files"},"summarize":{"input":"summarize"}},"edges":[{"to":"summarize"}]}
{"entry":"search","nodes":{"search":{"input":"find files"},"summarize":{"input":"summarize"}},"edges":{"search":[{"to":"summarize"}]}}

Valid complete example:
{"entry":"search","nodes":{"search":{"input":"find files"},"summarize":{"input":"summarize the file list"}},"edges":{"search":["summarize"]}}
`

// planToolProvider decorates Seele's WorkPlan handlers with Seelex's explicit
// plan_load contract. Handlers remain framework-owned; only LLM-facing schema
// and description are enriched.
type planToolProvider struct {
	tool   *builtin.WorkPlanTool
	policy func() PlanPolicy
}

func (provider *planToolProvider) ProviderName() string { return "seelex-workplan" }

func (provider *planToolProvider) Tools() []interfaces.ToolEntry {
	entries := provider.tool.Tools()
	for index := range entries {
		if entries[index].Definition.Function.Name == "plan_load" {
			entries[index] = enrichPlanLoadEntry(entries[index])
			entries[index].Handler = &planLoadPolicyHandler{
				delegate: entries[index].Handler,
				tool:     provider.tool,
				policy:   provider.policy,
			}
		}
	}
	return entries
}

type planLoadPolicyHandler struct {
	delegate interfaces.ToolHandler
	tool     *builtin.WorkPlanTool
	policy   func() PlanPolicy
	mu       sync.Mutex
}

func (handler *planLoadPolicyHandler) Execute(ctx context.Context, argsJSON string) (string, error) {
	handler.mu.Lock()
	defer handler.mu.Unlock()

	policy := PlanPolicy{}
	if handler.policy != nil {
		policy = handler.policy()
	}
	nodeCount, err := policy.validateLoad(argsJSON)
	if err != nil {
		return "", err
	}
	handler.tool.SetMaxForkConcurrency(policy.concurrency(nodeCount))
	return handler.delegate.Execute(ctx, argsJSON)
}

func enrichPlanLoadEntry(entry interfaces.ToolEntry) interfaces.ToolEntry {
	entry.Definition.Function.Description += planLoadContractDescription
	entry.Definition.Function.Parameters["additionalProperties"] = false
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
	edges, ok := properties["edges"].(map[string]interface{})
	if !ok {
		return entry
	}
	edges["description"] = "Object keyed by source node ID. Each value is an array of target node ID strings; never edge objects."
	edges["additionalProperties"] = map[string]interface{}{
		"type":  "array",
		"items": map[string]interface{}{"type": "string"},
	}
	return entry
}
