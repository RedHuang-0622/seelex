package seelebridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/RedHuang-0622/Seele/agent/core/api"
	"github.com/RedHuang-0622/Seele/types"
)

// PlanPreflight is the audited result of the mandatory planning turn that
// runs before a normal ReAct request.
type PlanPreflight struct {
	Arguments string
	Result    string
}

// ReplanRequest is the bounded, auditable context supplied to an isolated
// recovery-planning turn. It deliberately contains execution facts rather
// than an unbounded chat transcript.
type ReplanRequest struct {
	IdempotencyKey string
	Objective      string
	PreviousPlan   string
	Failure        string
	Evidence       string
}

// PreparePlan makes an isolated planning request which may only call
// plan_load. The request transport forces that function at the provider API,
// so a prose reply cannot bypass the effort policy.
func (r *Runtime) PreparePlan(ctx context.Context, input string) (PlanPreflight, error) {
	return r.preparePlan(ctx, planPreflightPrompt, input, "plan preflight", false, nil)
}

// PrepareReplan atomically replaces a failed WorkPlan with a recovery plan.
// It only plans; it never invokes plan_run or retries side effects. The
// existing plan_load handler still applies the current effort policy.
func (r *Runtime) PrepareReplan(ctx context.Context, request ReplanRequest) (PlanPreflight, error) {
	if request.Objective == "" {
		return PlanPreflight{}, fmt.Errorf("plan replan: objective is required")
	}
	context := "Objective:\n" + request.Objective + "\n\nPrevious plan:\n" + request.PreviousPlan +
		"\n\nObserved failure:\n" + request.Failure
	if request.Evidence != "" {
		context += "\n\nCompleted-node evidence:\n" + request.Evidence
	}
	idempotencyKey := request.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = replanOperationKey(request)
	}
	finish, err := r.replanGuard.acquire(idempotencyKey)
	if err != nil {
		return PlanPreflight{}, err
	}
	result, err := r.preparePlan(ctx, replanPrompt, context, "plan replan", true, r.replanGuard.acquireProviderRequest)
	finish(err)
	return result, err
}

func replanOperationKey(request ReplanRequest) string {
	value := request.Objective + "\x00" + request.PreviousPlan + "\x00" + request.Failure + "\x00" + request.Evidence
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("replan-%x", sum[:])
}

func (r *Runtime) preparePlan(ctx context.Context, prompt func(PlanPolicy) string, input, stage string, explicit bool, onProviderRequest func() error) (PlanPreflight, error) {
	policy := r.currentPlanPolicy()
	if !policy.RequirePlan && !explicit {
		return PlanPreflight{}, nil
	}
	tool, ok := r.planLoadDefinition()
	if !ok {
		return PlanPreflight{}, fmt.Errorf("plan preflight: plan_load is not registered")
	}
	client := r.planPreflightClient()
	var (
		arguments string
		lastErr   error
	)
	for attempt := 0; attempt < 2; attempt++ {
		if onProviderRequest != nil {
			if err := onProviderRequest(); err != nil {
				return PlanPreflight{}, fmt.Errorf("%s request: %w", stage, err)
			}
		}
		attemptInput := input
		if lastErr != nil {
			attemptInput += "\n\nThe previous plan_load was rejected before execution: " + lastErr.Error() +
				". Correct the JSON and issue exactly one valid plan_load call now."
		}
		message, err := client.Complete(ctx, []types.Message{
			{Role: "system", Content: stringPointer(prompt(policy))},
			{Role: "user", Content: stringPointer(attemptInput)},
		}, []types.Tool{tool})
		if err != nil {
			return PlanPreflight{}, fmt.Errorf("%s request: %w", stage, err)
		}
		if len(message.ToolCalls) == 1 && message.ToolCalls[0].Function.Name == "plan_load" {
			arguments = message.ToolCalls[0].Function.Arguments
			result, dispatchErr := r.agent.DirectDispatch(ctx, "plan_load", arguments)
			if dispatchErr == nil {
				return PlanPreflight{Arguments: arguments, Result: result}, nil
			}
			if !retryablePlanLoadError(dispatchErr) {
				return PlanPreflight{Arguments: arguments}, fmt.Errorf("%s load failed; retry is unsafe: %w", stage, dispatchErr)
			}
			lastErr = dispatchErr
			continue
		}
		if len(message.ToolCalls) != 0 {
			return PlanPreflight{}, fmt.Errorf("%s: provider returned an unexpected tool call; refusing retry", stage)
		}
		lastErr = fmt.Errorf("provider returned no tool call")
	}
	if lastErr != nil {
		return PlanPreflight{Arguments: arguments}, fmt.Errorf("%s: no valid plan_load after one idempotent retry: %w", stage, lastErr)
	}
	return PlanPreflight{}, fmt.Errorf("%s: no valid plan_load after one idempotent retry", stage)
}

// retryablePlanLoadError accepts only failures emitted before the delegated
// WorkPlan handler executes. Both the plan policy validation layer and the
// plan_load handler reject these before a WorkPlan is replaced, so a
// corrective provider request cannot duplicate an executed recovery plan.
func retryablePlanLoadError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.HasPrefix(message, "plan_load:") || strings.HasPrefix(message, "plan policy ")
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

func replanPrompt(policy PlanPolicy) string {
	return planPreflightPrompt(policy) + " The prior plan failed. Produce a replacement recovery plan for the remaining work only. " +
		"Do not repeat completed work or automatically retry the failed side effect. Use the supplied failure and evidence to add diagnosis or a safe alternative before any retry. " +
		"The replacement must contain at least one node and its entry must be a nodes key. If no automatic recovery is safe, create one manual decision node instead of returning an empty plan."
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
