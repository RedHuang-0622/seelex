package seelebridge

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/agent/core/api"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/internal/promptassets"
)

// PlanPreflight is the audited result of an isolated optional planning turn.
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
	if !explicit {
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
	for attempt := 0; attempt < r.limits.PreflightRetry; attempt++ {
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
		// 规划永远靠 prompt 引导（replan/recovery 模板含 "exactly one valid
		// plan_load" 强指令），不做 tool_choice 强制：OpenAI thinking 模型
		// （o 系/GPT-5）平台拒绝 tool_choice 强制（"Thinking mode does not
		// support this tool_choice"），强制 transport 已于 2026-08-01 移除。
		requestContext := ctx
		cancelDecision := func() {}
		if !explicit && attempt == 0 {
			requestContext, cancelDecision = context.WithTimeout(ctx, r.planDecisionTimeout)
		}
		message, err, decisionTimedOut := r.completePlanPreflight(
			ctx,
			requestContext,
			explicit,
			attempt,
			prompt(policy),
			attemptInput,
			tool,
		)
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
			result, dispatchErr := r.agt.DirectDispatch(ctx, "plan_load", canonicalArgs)
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

// completePlanPreflight enforces the optional decision deadline independently
// of provider behavior. A provider may return an invalid or empty response
// after its request context has expired; that response must not bypass the
// prompt-directed plan_load fallback.
func (r *Runtime) completePlanPreflight(
	parentCtx, requestCtx context.Context,
	explicit bool,
	attempt int,
	systemPrompt, input string,
	tool types.Tool,
) (types.Message, error, bool) {
	complete := func() (types.Message, error) {
		return r.planPreflightClient().Complete(requestCtx, []types.Message{
			{Role: "system", Content: stringPointer(systemPrompt)},
			{Role: "user", Content: stringPointer(input)},
		}, []types.Tool{tool})
	}
	if explicit || attempt != 0 {
		message, err := complete()
		return message, err, false
	}

	type completion struct {
		message types.Message
		err     error
	}
	completed := make(chan completion, 1)
	go func() {
		message, err := complete()
		completed <- completion{message: message, err: err}
	}()

	select {
	case result := <-completed:
		if requestCtx.Err() != nil && parentCtx.Err() == nil {
			return types.Message{}, requestCtx.Err(), true
		}
		return result.message, result.err, false
	case <-requestCtx.Done():
		if parentCtx.Err() != nil {
			return types.Message{}, parentCtx.Err(), false
		}
		return types.Message{}, requestCtx.Err(), true
	}
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
	if r.registry == nil || r.registry.registry == nil {
		return types.Tool{}, false
	}
	for _, tool := range r.registry.registry.Tools() {
		if tool.Function.Name == "plan_load" {
			return tool, true
		}
	}
	return types.Tool{}, false
}

// planPreflightClient 构造隔离的规划回合 Completer 实例（plan.md §3.6）：
// 每次调用创建独立 api.ChatClient，不参与共享账号池与选择器（无账号选择
// 副作用、不消耗主链路租约），规划会话不暴露任何工具（只传 plan_load 定义）。
// 不做 tool_choice 强制（thinking 模型平台拒绝；强制 transport 已移除）。
func (r *Runtime) planPreflightClient() *api.ChatClient {
	spec := r.resolvePreflightAccountSpec()
	client := api.NewChatClient(types.LLMConfig{
		BaseURL: spec.BaseURL, APIKey: spec.APIKey, Model: spec.Model,
		MaxTokens: spec.MaxTokens, Timeout: 300, Temperature: 0.7,
	})
	provider := api.ProviderType(spec.Provider)
	client.SetProvider(provider)
	return client
}

// resolvePreflightAccountSpec 选择规划回合的账号：优先 subagent 角色，
// 未配置时回退主 agent 角色（与旧 ResolveAccount 的 fallbackRoles 一致）。
func (r *Runtime) resolvePreflightAccountSpec() accountSpec {
	specs := r.accountSpecList()
	if len(specs) == 0 {
		return accountSpec{}
	}
	if spec, err := ResolveAccountSpec(specs, RoleSubAgent); err == nil {
		return spec
	}
	if spec, err := ResolveAccountSpec(specs, RoleAgent); err == nil {
		return spec
	}
	return specs[0]
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
	concurrency := "plan_run executes every currently runnable node concurrently (agent nodes as subagents); without plan_run the primary Agent executes the DAG serially as a tasklist"
	if policy.MaxForkConcurrency > 0 {
		concurrency = fmt.Sprintf("plan_run executes at most %d independent nodes concurrently (agent nodes as subagents); without plan_run the primary Agent executes the DAG serially as a tasklist", policy.MaxForkConcurrency)
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
