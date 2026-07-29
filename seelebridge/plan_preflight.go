package seelebridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/RedHuang-0622/Seele/agent/core/api"
	"github.com/RedHuang-0622/Seele/types"
)

// PlanPreflight is the audited result of the mandatory planning turn that
// runs before a normal ReAct request.
type PlanPreflight struct {
	Arguments string
	Result    string
}

// PreparePlan makes an isolated planning request which may only call
// plan_load. The request transport forces that function at the provider API,
// so a prose reply cannot bypass the effort policy.
func (r *Runtime) PreparePlan(ctx context.Context, input string) (PlanPreflight, error) {
	policy := r.currentPlanPolicy()
	if !policy.RequirePlan {
		return PlanPreflight{}, nil
	}
	tool, ok := r.planLoadDefinition()
	if !ok {
		return PlanPreflight{}, fmt.Errorf("plan preflight: plan_load is not registered")
	}
	client := r.planPreflightClient()
	message, err := client.Complete(ctx, []types.Message{
		{Role: "system", Content: stringPointer(planPreflightPrompt(policy))},
		{Role: "user", Content: stringPointer(input)},
	}, []types.Tool{tool})
	if err != nil {
		return PlanPreflight{}, fmt.Errorf("plan preflight request: %w", err)
	}
	if len(message.ToolCalls) != 1 || message.ToolCalls[0].Function.Name != "plan_load" {
		return PlanPreflight{}, fmt.Errorf("plan preflight: provider did not return the required plan_load call")
	}
	arguments := message.ToolCalls[0].Function.Arguments
	result, err := r.agent.DirectDispatch(ctx, "plan_load", arguments)
	if err != nil {
		return PlanPreflight{Arguments: arguments}, fmt.Errorf("plan preflight load: %w", err)
	}
	return PlanPreflight{Arguments: arguments, Result: result}, nil
}

func (r *Runtime) planLoadDefinition() (types.Tool, bool) {
	for _, tool := range r.agent.Tools().Tools() {
		if tool.Function.Name == "plan_load" {
			return tool, true
		}
	}
	return types.Tool{}, false
}

func (r *Runtime) planPreflightClient() *api.ChatClient {
	client := api.NewChatClient(r.client.Cfg).WithAccountPool(r.pool)
	client.SetProvider(r.client.Provider())
	client.SetProviderFilter(r.client.ProviderFilter())
	httpClient := *r.client.Client
	base := httpClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	httpClient.Transport = forcePlanLoadTransport{base: base, provider: r.client.Provider()}
	client.Client = &httpClient
	return client
}

func planPreflightPrompt(policy PlanPolicy) string {
	constraint := ""
	if policy.RequireSerial {
		constraint = " Use no more than 4 nodes in one serial chain."
	}
	return "Compile the user request into one valid plan_load call. Return the function call only; do not return prose. " +
		"Use exactly this JSON shape: {\"entry\":\"inspect\",\"nodes\":{\"inspect\":{\"input\":\"inspect\"},\"report\":{\"input\":\"report\"}},\"edges\":{\"inspect\":[\"report\"]}}. " +
		"nodes and edges are objects, never arrays; edges values are arrays of target ID strings, never edge objects." + constraint
}

func stringPointer(value string) *string { return &value }

type forcePlanLoadTransport struct {
	base     http.RoundTripper
	provider api.ProviderType
}

func (transport forcePlanLoadTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	if err := request.Body.Close(); err != nil {
		return nil, err
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	choice := json.RawMessage(`{"type":"function","function":{"name":"plan_load"}}`)
	if transport.provider == api.ProviderAnthropic {
		choice = json.RawMessage(`{"type":"tool","name":"plan_load"}`)
	}
	payload["tool_choice"] = choice
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	clone := request.Clone(request.Context())
	clone.Body = io.NopCloser(bytes.NewReader(encoded))
	clone.ContentLength = int64(len(encoded))
	clone.Header = request.Header.Clone()
	clone.Header.Set("Content-Length", fmt.Sprint(len(encoded)))
	return transport.base.RoundTrip(clone)
}
