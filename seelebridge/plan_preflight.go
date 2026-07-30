package seelebridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/agent/core/api"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/internal/promptassets"
)

const defaultPlanDecisionTimeout = 10 * time.Second

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

// PreparePlan runs an isolated planning-gate request before normal ReAct. The
// gate may decide that a request is reply-only and return no tool call; in that
// case normal ReAct proceeds without a WorkPlan. When it chooses plan_load,
// runtime validates and loads the resulting DAG before execution starts.
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
	var (
		arguments                 string
		lastErr                   error
		planningRequiredByTimeout bool
	)
	for attempt := 0; attempt < 2; attempt++ {
		if onProviderRequest != nil {
			if err := onProviderRequest(); err != nil {
				return PlanPreflight{}, fmt.Errorf("%s request: %w", stage, err)
			}
		}
		attemptInput := input
		if lastErr != nil {
			if planningRequiredByTimeout {
				attemptInput += "\n\nThe planning gate exceeded its decision allowance. Planning is now required; issue exactly one valid plan_load call for this request."
			} else {
				attemptInput += "\n\nThe previous plan_load was rejected before execution: " + lastErr.Error() +
					". Correct the JSON and issue exactly one valid plan_load call now."
			}
		}
		// The first ordinary preflight request is an isolated planning decision:
		// it may reply without a tool for a conversational turn. A corrective
		// retry, and every explicit replan, force plan_load because the model has
		// already committed to planning and only its DAG needs correction.
		forcePlanLoad := explicit || attempt > 0
		requestContext := ctx
		cancelDecision := func() {}
		decisionStartedAt := time.Now()
		if !explicit && attempt == 0 {
			requestContext, cancelDecision = context.WithTimeout(ctx, r.planDecisionTimeout)
		}
		message, err := r.planPreflightClient(forcePlanLoad).Complete(requestContext, []types.Message{
			{Role: "system", Content: stringPointer(prompt(policy))},
			{Role: "user", Content: stringPointer(attemptInput)},
		}, []types.Tool{tool})
		decisionTimedOut := !explicit && attempt == 0 && ctx.Err() == nil &&
			(errors.Is(requestContext.Err(), context.DeadlineExceeded) || time.Since(decisionStartedAt) >= r.planDecisionTimeout)
		cancelDecision()
		if decisionTimedOut {
			planningRequiredByTimeout = true
			lastErr = fmt.Errorf("planning decision exceeded %s", r.planDecisionTimeout)
			continue
		}
		if err != nil {
			return PlanPreflight{}, fmt.Errorf("%s request: %w", stage, err)
		}
		if len(message.ToolCalls) == 1 && message.ToolCalls[0].Function.Name == "plan_load" {
			arguments = message.ToolCalls[0].Function.Arguments
			canonicalArgs, normalizeErr := NormalizePlanLoadArguments(arguments)
			if normalizeErr != nil {
				lastErr = fmt.Errorf("plan_load: normalize DAG input: %w", normalizeErr)
				continue
			}
			result, dispatchErr := r.agent.DirectDispatch(ctx, "plan_load", canonicalArgs)
			if dispatchErr == nil {
				return PlanPreflight{Arguments: canonicalArgs, Result: result}, nil
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
		if !explicit && attempt == 0 {
			return PlanPreflight{}, nil
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

func (r *Runtime) planPreflightClient(forcePlanLoad bool) *api.ChatClient {
	client := api.NewChatClient(r.client.Cfg).WithAccountPool(r.pool)
	provider := r.client.Provider()
	// Planning is an isolated subagent role when that role is configured. The
	// resolver falls back to the primary agent account, so installations without
	// a subagent remain fully functional.
	if account, err := ResolveAccount(r.pool, RoleSubAgent); err == nil && client.SelectAccount(account.Name) {
		provider = account.Provider
	}
	client.SetProvider(provider)
	client.SetProviderFilter(provider)
	httpClient := *r.client.Client
	base := httpClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	if forcePlanLoad {
		httpClient.Transport = forcePlanLoadTransport{base: base, provider: provider}
	}
	client.Client = &httpClient
	return client
}

func planPreflightPrompt(policy PlanPolicy) string {
	return promptassets.PlanPreflight(planPromptData(policy))
}

func replanPrompt(policy PlanPolicy) string {
	return promptassets.PlanReplan(planPromptData(policy))
}

func planPromptData(policy PlanPolicy) promptassets.PlanData {
	nodeLimit := "no fixed node-count limit"
	if policy.MaxNodes > 0 {
		nodeLimit = fmt.Sprintf("at most %d nodes", policy.MaxNodes)
	}
	topology := "a valid DAG; use edges only for real dependencies"
	if policy.RequireSerial {
		topology = "one serial chain from entry; no fan-out or fan-in"
	}
	concurrency := "DAG dependencies may be expressed, but the primary ReAct agent executes the authoritative checklist serially"
	if policy.MaxForkConcurrency > 0 {
		concurrency = fmt.Sprintf("DAG branches are future candidates for at most %d concurrent node runners; do not claim they execute concurrently today", policy.MaxForkConcurrency)
	}
	verification := "include verification for material claims and observable changes"
	if policy.Effort == "lite" {
		verification = "use the smallest observable check needed for recovery"
	}
	return promptassets.PlanData{
		Effort:       policy.Effort,
		NodeLimit:    nodeLimit,
		Topology:     topology,
		Concurrency:  concurrency,
		Verification: verification,
	}
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
