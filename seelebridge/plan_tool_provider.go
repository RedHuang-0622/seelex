package seelebridge

import (
	"context"
	"fmt"
	"sync"

	"github.com/RedHuang-0622/Seele/agent/core/tool/builtin"
	"github.com/RedHuang-0622/Seele/agent/core/tool/interfaces"
)

const planLoadContractDescription = `

When to use plan_load:
- The user explicitly asks for a plan or checklist.
- A code/file change has real inspect -> implement -> verify dependencies.
- Research or a multi-deliverable task needs visible evidence and reporting stages.

When not to use plan_load:
- Greetings, acknowledgements, simple clarifications, or a self-contained answer.
- One obvious read-only check or a task whose next safe action is already clear.
- After work is already complete merely to make the process appear structured.

If uncertain, take the smallest safe direct step first. A Plan is optional;
do not create a one-node reply plan for casual conversation.

Strict JSON contract:
- Emit exactly three top-level fields: entry, nodes, and edges. Do not use item.
- Nodes MUST be an object keyed by node ID. Edges MUST be an object keyed by source node ID with arrays of target ID strings.
- Every ID named by entry or edges must be a key in nodes.

Valid simple example:
{"entry":"reply","nodes":{"reply":{"input":"answer the user"}},"edges":{}}

Valid audit example:
{"entry":"inspect","nodes":{"inspect":{"input":"read source"},"verify":{"input":"verify claims"},"report":{"input":"report findings"}},"edges":{"inspect":["verify"],"verify":["report"]}}

Valid code-change example:
{"entry":"inspect","nodes":{"inspect":{"input":"inspect scope"},"implement":{"input":"make change"},"verify":{"input":"run tests"},"report":{"input":"summarize result"}},"edges":{"inspect":["implement"],"implement":["verify"],"verify":["report"]}}

- Do not use node or edge arrays in preflight.
- Do not encode edges as a string such as "inspect -> verify -> report".

Invalid unrelated top-level field example (do not use):
{"entry":"inspect","nodes":{"inspect":{"input":"inspect"}},"item":{"input":"metadata"},"edges":{}}
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

// withoutAuthoritativePlanMutationTools exposes the loaded DAG as a control
// context, not as unscoped subagent work. plan_run creates framework child
// chats without Seelex's project-scoped tool set or upstream evidence envelope,
// so it remains unavailable until a real NodeExecutionEnvelope exists.
func withoutAuthoritativePlanMutationTools(entries []interfaces.ToolEntry) []interfaces.ToolEntry {
	filtered := make([]interfaces.ToolEntry, 0, len(entries))
	for _, entry := range entries {
		switch entry.Definition.Function.Name {
		case "plan_load", "plan_clear", "plan_run":
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
	nodes["description"] = "Required preflight shape: an object keyed by node ID. Do not use an array."
	nodes["type"] = "object"
	nodes["additionalProperties"] = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"input": map[string]interface{}{"type": "string"},
			"kind":  map[string]interface{}{"type": "string", "enum": []string{"auto", "manual"}},
		},
		"required": []string{"input"},
	}
	delete(nodes, "oneOf")
	edges, ok := properties["edges"].(map[string]interface{})
	if !ok {
		return entry
	}
	edges["description"] = "Required preflight shape: a source-to-target adjacency object. Do not use an array."
	edges["type"] = "object"
	edges["additionalProperties"] = map[string]interface{}{
		"type":  "array",
		"items": map[string]interface{}{"type": "string"},
	}
	delete(edges, "oneOf")
	return entry
}
