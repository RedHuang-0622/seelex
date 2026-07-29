package seelebridge

import (
	"context"
	"fmt"
	"sync"

	"github.com/RedHuang-0622/Seele/agent/core/tool/builtin"
	"github.com/RedHuang-0622/Seele/agent/core/tool/interfaces"
)

const planLoadContractDescription = `

Strict JSON contract:
- Use only these top-level fields: entry, nodes, and edges. Do not use item.
- Canonical nodes is an object keyed by node ID; canonical edges is an object keyed by source node ID with arrays of target ID strings.
- LLM-friendly adapter form is also accepted: nodes may be an array of {id|key,input,kind?}, and edges may be an array of {from|source,to|target}.
- An edges object may also use [{"to":"target-id"}] values and will be normalized.
- Every array edge MUST name both its source and target. Do not send {"to":"target"} without from/source.
- Every ID named by entry or edges must be a key in nodes.

Invalid nodes example (do not use):
{"entry":"search","nodes":[{"input":"find files"}],"edges":{}}

Invalid edges examples (do not use):
{"entry":"search","nodes":{"search":{"input":"find files"},"summarize":{"input":"summarize"}},"edges":[{"to":"summarize"}]}

Invalid top-level node example (do not use):
{"entry":"inspect","inspect":{"input":"inspect"},"verify":{"input":"verify"},"edges":{}}

Valid adapter example:
{"entry":"search","nodes":[{"id":"search","input":"find files"},{"key":"summarize","input":"summarize the file list"}],"edges":[{"from":"search","to":"summarize"}]}

Valid complete example:
{"entry":"search","nodes":{"search":{"input":"find files"},"summarize":{"input":"summarize the file list"}},"edges":{"search":["summarize"]}}
`

// planToolProvider decorates Seele's WorkPlan handlers with Seelex's explicit
// plan_load contract. Handlers remain framework-owned; only LLM-facing schema
// and description are enriched.
type planToolProvider struct {
	tool          *builtin.WorkPlanTool
	policy        func() PlanPolicy
	authoritative func() bool
	authorize     func(context.Context, string) error
}

func (provider *planToolProvider) ProviderName() string { return "seelex-workplan" }

func (provider *planToolProvider) Tools() []interfaces.ToolEntry {
	entries := provider.tool.Tools()
	for index := range entries {
		switch entries[index].Definition.Function.Name {
		case "plan_load":
			entries[index] = enrichPlanLoadEntry(entries[index])
			entries[index].Handler = &planLoadPolicyHandler{
				delegate:  entries[index].Handler,
				tool:      provider.tool,
				policy:    provider.policy,
				authorize: provider.authorize,
			}
		case "plan_clear":
			entries[index].Handler = &planMutationGuardHandler{
				delegate:  entries[index].Handler,
				toolName:  "plan_clear",
				authorize: provider.authorize,
			}
		}
	}
	if provider.authoritative != nil && provider.authoritative() {
		return withoutAuthoritativePlanMutationTools(entries)
	}
	return entries
}

// withoutAuthoritativePlanMutationTools leaves execution and read-only Plan
// tools visible, but omits operations that can replace or clear the loaded DAG.
func withoutAuthoritativePlanMutationTools(entries []interfaces.ToolEntry) []interfaces.ToolEntry {
	filtered := make([]interfaces.ToolEntry, 0, len(entries))
	for _, entry := range entries {
		switch entry.Definition.Function.Name {
		case "plan_load", "plan_clear":
			continue
		default:
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

type planLoadPolicyHandler struct {
	delegate  interfaces.ToolHandler
	tool      *builtin.WorkPlanTool
	policy    func() PlanPolicy
	authorize func(context.Context, string) error
	mu        sync.Mutex
}

func (handler *planLoadPolicyHandler) Execute(ctx context.Context, argsJSON string) (string, error) {
	handler.mu.Lock()
	defer handler.mu.Unlock()
	if handler.authorize != nil {
		if err := handler.authorize(ctx, "plan_load"); err != nil {
			return "", err
		}
	}
	canonicalArgs, err := NormalizePlanLoadArguments(argsJSON)
	if err != nil {
		return "", fmt.Errorf("plan_load: normalize DAG input: %w", err)
	}

	policy := PlanPolicy{}
	if handler.policy != nil {
		policy = handler.policy()
	}
	nodeCount, err := policy.validateLoad(canonicalArgs)
	if err != nil {
		return "", err
	}
	handler.tool.SetMaxForkConcurrency(policy.concurrency(nodeCount))
	return handler.delegate.Execute(ctx, canonicalArgs)
}

type planMutationGuardHandler struct {
	delegate  interfaces.ToolHandler
	toolName  string
	authorize func(context.Context, string) error
}

func (handler *planMutationGuardHandler) Execute(ctx context.Context, argsJSON string) (string, error) {
	if handler.authorize != nil {
		if err := handler.authorize(ctx, handler.toolName); err != nil {
			return "", err
		}
	}
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
	nodes["description"] = "Canonical object keyed by node ID, or adapter array entries with id/key and input."
	nodes["oneOf"] = []interface{}{
		map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"input": map[string]interface{}{"type": "string"},
				"kind":  map[string]interface{}{"type": "string", "enum": []string{"auto", "manual"}},
			},
			"required": []string{"input"},
		}},
		map[string]interface{}{"type": "array", "items": map[string]interface{}{
			"type": "object", "properties": map[string]interface{}{
				"id": map[string]interface{}{"type": "string"}, "key": map[string]interface{}{"type": "string"},
				"input": map[string]interface{}{"type": "string"}, "kind": map[string]interface{}{"type": "string", "enum": []string{"auto", "manual"}},
			}, "required": []string{"input"},
		}},
	}
	delete(nodes, "type")
	delete(nodes, "additionalProperties")
	edges, ok := properties["edges"].(map[string]interface{})
	if !ok {
		return entry
	}
	edges["description"] = "Canonical source-to-target adjacency object, or adapter edge array with from/source and to/target."
	edges["oneOf"] = []interface{}{
		map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{
			"type":  "array",
			"items": map[string]interface{}{"type": "string"},
		}},
		map[string]interface{}{"type": "array", "items": map[string]interface{}{
			"type": "object", "properties": map[string]interface{}{
				"from": map[string]interface{}{"type": "string"}, "source": map[string]interface{}{"type": "string"},
				"to": map[string]interface{}{"type": "string"}, "target": map[string]interface{}{"type": "string"},
			},
		}},
	}
	delete(edges, "type")
	delete(edges, "additionalProperties")
	return entry
}
