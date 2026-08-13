package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/RedHuang-0622/Seele/agent"
	frameworkevent "github.com/RedHuang-0622/Seele/event"
	"github.com/RedHuang-0622/Seele/tools"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/Seele/workplan"
	"github.com/RedHuang-0622/Seele/workplan/codec"
	coreplan "github.com/RedHuang-0622/Seele/workplan/core/plan"
	workplanTypes "github.com/RedHuang-0622/Seele/workplan/core/types"
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

// LoadedPlanDoc 是当前加载的权威 Plan 的规范化存储：Canonical 供
// plan_export 原样输出，Plan 是 codec.Import 产物（可执行内核）。
type LoadedPlanDoc struct {
	Canonical   string
	Entry       string
	NodeCount   int
	EdgeCount   int
	MaxForkConc int
	Plan        *coreplan.Plan
}

// ToolProvider decorates Seele's WorkPlan handlers with Seelex's explicit
// plan_load contract. 新装配模型下它实现 tools.ToolProvider，直接注册进
// tools.Registry；plan 状态保存在 Runtime 内存中（slice 4 之前）。
type ToolProvider struct {
	executor  *Executor
	policy    func() PlanPolicy
	authorize func(context.Context, string) error

	mu                 sync.Mutex
	loaded             *LoadedPlanDoc
	maxForkConcurrency int
}

func NewToolProvider(executor *Executor) *ToolProvider {
	return &ToolProvider{
		executor:  executor,
		policy:    executor.Policy,
		authorize: AuthorizePlanMutation,
	}
}

func (provider *ToolProvider) ProviderName() string { return "seelex-workplan" }

// Tools 恒返回全部 plan 工具：plan_run 在 authoritative 模式恢复可见
// （子代理继承项目作用域工具与父证据，DAG 可真并行，不再需要隐藏；
// plan_load/plan_clear 的权威期准入由 authorizePlanMutation 拦截）。
func (provider *ToolProvider) Tools() []tools.ToolEntry {
	return []tools.ToolEntry{
		provider.planLoadEntry(),
		provider.planClearEntry(),
		provider.planRunEntry(),
		provider.planStatusEntry(),
		provider.planExportEntry(),
		provider.planValidateEntry(),
	}
}

// ── 工具定义 ─────────────────────────────────────────────────────────

func (provider *ToolProvider) planLoadEntry() tools.ToolEntry {
	entry := tools.ToolEntry{
		Definition: types.Tool{
			Type: "function",
			Function: types.ToolFunction{
				Name:        "plan_load",
				Description: "Load a validated DAG plan as the authoritative task structure for the current task." + planLoadContractDescription,
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"entry": map[string]interface{}{"type": "string"},
						"nodes": map[string]interface{}{
							"type":        "object",
							"description": "Required preflight shape: an object keyed by node ID. Do not use an array.",
							"additionalProperties": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"input": map[string]interface{}{"type": "string"},
									"kind":  map[string]interface{}{"type": "string", "enum": []string{"auto", "manual", "agent", "function", "approve", "verify", "deliver"}},
									"budget": map[string]interface{}{
										"type":        "object",
										"description": "Node-level execution budget (optional; falls back to seele.yaml limits).",
										"properties": map[string]interface{}{
											"max_loops":         map[string]interface{}{"type": "integer", "minimum": 1},
											"max_output_tokens": map[string]interface{}{"type": "integer", "minimum": 1},
										},
									},
								},
								"required": []string{"input"},
							},
						},
						"edges": map[string]interface{}{
							"type":        "object",
							"description": "Required preflight shape: a source-to-target adjacency object. Do not use an array.",
							"additionalProperties": map[string]interface{}{
								"type":  "array",
								"items": map[string]interface{}{"type": "string"},
							},
						},
					},
					"required":             []string{"entry", "nodes", "edges"},
					"additionalProperties": false,
				},
			},
		},
		Handler: &planLoadPolicyHandler{
			provider: provider,
		},
	}
	return entry
}

func (provider *ToolProvider) planClearEntry() tools.ToolEntry {
	return tools.ToolEntry{
		Definition: types.Tool{
			Type: "function",
			Function: types.ToolFunction{
				Name:        "plan_clear",
				Description: "Clear the currently loaded plan. The Plan becomes a normal checklist again.",
				Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
			},
		},
		Handler: &planMutationGuardHandler{
			provider: provider,
			toolName: "plan_clear",
			delegate: tools.HandlerFunc(provider.clearPlan),
		},
	}
}

func (provider *ToolProvider) planRunEntry() tools.ToolEntry {
	return tools.ToolEntry{
		Definition: types.Tool{
			Type: "function",
			Function: types.ToolFunction{
				Name:        "plan_run",
				Description: "Execute the loaded plan DAG with the workplan kernel. Node completion and plan lifecycle facts are projected through the plan event sink.",
				Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
			},
		},
		Handler: &planMutationGuardHandler{
			provider: provider,
			toolName: "plan_run",
			delegate: &planRunHandler{provider: provider},
		},
	}
}

func (provider *ToolProvider) planStatusEntry() tools.ToolEntry {
	return tools.ToolEntry{
		Definition: types.Tool{
			Type: "function",
			Function: types.ToolFunction{
				Name:        "plan_status",
				Description: "Report the status of the currently loaded plan.",
				Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
			},
		},
		Handler: tools.HandlerFunc(provider.planStatus),
	}
}

func (provider *ToolProvider) planExportEntry() tools.ToolEntry {
	return tools.ToolEntry{
		Definition: types.Tool{
			Type: "function",
			Function: types.ToolFunction{
				Name:        "plan_export",
				Description: "Export the currently loaded canonical plan JSON.",
				Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
			},
		},
		Handler: tools.HandlerFunc(provider.planExport),
	}
}

func (provider *ToolProvider) planValidateEntry() tools.ToolEntry {
	return tools.ToolEntry{
		Definition: types.Tool{
			Type: "function",
			Function: types.ToolFunction{
				Name:        "plan_validate",
				Description: "Validate a plan DAG payload without loading it as the authoritative plan.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"plan": map[string]interface{}{"type": "string", "description": "Canonical plan JSON to validate."},
					},
					"required": []string{"plan"},
				},
			},
		},
		Handler: tools.HandlerFunc(provider.planValidate),
	}
}

// ── 处理器实现 ───────────────────────────────────────────────────────

type planLoadPolicyHandler struct {
	provider *ToolProvider
	mu       sync.Mutex
}

func (handler *planLoadPolicyHandler) Execute(ctx context.Context, argsJSON string) (string, error) {
	handler.mu.Lock()
	defer handler.mu.Unlock()
	provider := handler.provider
	if provider.authorize != nil {
		if err := provider.authorize(ctx, "plan_load"); err != nil {
			return "", err
		}
	}
	canonicalArgs, err := NormalizePlanLoadArguments(argsJSON)
	if err != nil {
		return "", fmt.Errorf("plan_load: normalize DAG input: %w", err)
	}

	policy := PlanPolicy{}
	if provider.policy != nil {
		policy = provider.policy()
	}
	nodeCount, err := ValidatePolicyLoad(policy, canonicalArgs)
	if err != nil {
		return "", err
	}
	maxFork := PolicyConcurrency(policy, nodeCount)

	// 环检测 + codec 导入（校验节点/边引用、重复边、DAG 可达性并 Seal）
	var spec PlanLoadSpec
	if err := json.Unmarshal([]byte(canonicalArgs), &spec); err != nil {
		return "", fmt.Errorf("plan_load: parse canonical plan: %w", err)
	}
	if err := DetectCycle(spec.Edges); err != nil {
		return "", fmt.Errorf("plan_load: %w", err)
	}
	document, err := CanonicalPlanDocument(canonicalArgs)
	if err != nil {
		return "", err
	}
	renderedPlan, err := codec.Render(document, provider.executor.currentNodeFactory()())
	if err != nil {
		return "", fmt.Errorf("plan_load: import DAG: %w", err)
	}
	entry, nodes, edgeCount, err := parseCanonicalPlan(canonicalArgs)
	if err != nil {
		return "", err
	}

	provider.mu.Lock()
	provider.loaded = &LoadedPlanDoc{
		Canonical: canonicalArgs, Entry: entry, NodeCount: nodes, EdgeCount: edgeCount,
		MaxForkConc: maxFork, Plan: renderedPlan,
	}
	provider.maxForkConcurrency = maxFork
	provider.mu.Unlock()

	result, _ := json.Marshal(map[string]interface{}{
		"status": "loaded", "node_count": nodes, "edge_count": edgeCount, "entry": entry,
	})
	return string(result), nil
}

type planMutationGuardHandler struct {
	provider *ToolProvider
	toolName string
	delegate tools.ToolHandler
}

func (handler *planMutationGuardHandler) Execute(ctx context.Context, argsJSON string) (string, error) {
	if handler.provider.authorize != nil {
		if err := handler.provider.authorize(ctx, handler.toolName); err != nil {
			return "", err
		}
	}
	if handler.delegate == nil {
		return "", fmt.Errorf("%s: handler is unavailable", handler.toolName)
	}
	return handler.delegate.Execute(ctx, argsJSON)
}

// planRunHandler 执行当前加载的 Plan DAG（workplan.NewFromPlan 入口）。
type planRunHandler struct {
	provider *ToolProvider
}

func (handler *planRunHandler) Execute(ctx context.Context, _ string) (string, error) {
	provider := handler.provider
	provider.mu.Lock()
	loaded := provider.loaded
	provider.mu.Unlock()
	if loaded == nil || loaded.Plan == nil {
		return "", fmt.Errorf("plan_run: no plan is loaded")
	}
	return provider.executor.RunPlan(ctx, loaded, true)
}

// runPlan 以 workplan.NewFromPlan 执行已加载的 Plan（api-map §8
// workplanRunEntry：必须经 WorkPlan.Run 入口，runner 事件配置才会生效）。
// 事件经 EventSink 落库并投影（PlanStatus）；节点完成经 runner
// NodeHook 投影（NodeStatus，含 kind/elapsed）。
// runPlan 执行已加载的 Plan。withNodeOutputs=false（fork 路径）时结果 JSON
// 只保留节点元数据、不内嵌完整节点输出——最终内容由 final_output（按子代理
// 数 ×n 放大的汇总窗口）承载，避免结果超限被归档后模型看不到内容而重跑。
func (executor *Executor) RunPlan(ctx context.Context, loaded *LoadedPlanDoc, withNodeOutputs bool) (string, error) {
	runID := newPlanRunID()
	executor.runMu.Lock()
	executor.currentRunID = runID
	executor.runMu.Unlock()
	defer func() {
		executor.runMu.Lock()
		if executor.currentRunID == runID {
			executor.currentRunID = ""
		}
		executor.runMu.Unlock()
	}()
	binding := executor.Binding()
	planID := binding.PlanID
	if planID == "" {
		planID = loaded.Entry
	}
	sink := executor.events
	wp := workplan.NewFromPlan(loaded.Plan, executor.CurrentAgentFactory(),
		workplan.WithEventSink(sink, planID),
		workplan.WithEventRunID(runID),
		workplan.WithEventHeartbeatPolicy(frameworkevent.HeartbeatPolicy{Interval: executor.deps.Heartbeat}),
		workplan.WithEventErrorHandler(executor.CurrentEventError()),
		workplan.WithMaxForkConcurrency(loaded.MaxForkConc),
		workplan.WithEventLocators(
			agent.EventLocator{AgentID: mainAgentID, SessionID: binding.SessionID, AccountID: binding.AccountID, Model: executor.deps.Model},
			workplan.EventLocator{PlanID: planID, RunID: runID},
		),
	)
	wp.NodeHook = func(nr *workplanTypes.NodeResult) {
		sink.AppendNodeResult(ctx, planID, runID, nr)
	}
	result, err := wp.Run(ctx)
	return planRunResultJSON(result, err, withNodeOutputs)
}

// newPlanRunID 生成一次 plan_run 的执行标识（事件相关性 run_id）。
func newPlanRunID() string {
	return fmt.Sprintf("run-%d", time.Now().UnixNano())
}

// planRunResultJSON 汇总 workplan 执行结果，格式与旧 WorkPlanTool 一致
// （NodeBase snake_case 平铺 JSON），供 application/core 的
// updatePlanFromRunResult / planRunFailure 解析。执行错误同时以返回值和
// "status":"failed" + "error" 字段表达，双通道均可观察。
func planRunResultJSON(result *workplanTypes.WorkPlanResult, err error, withNodeOutputs bool) (string, error) {
	status := "completed"
	if err != nil {
		status = "failed"
	} else if result != nil && result.Aborted {
		status = "aborted"
	}
	out := struct {
		Status      string                   `json:"status"`
		Error       string                   `json:"error,omitempty"`
		NodeCount   int                      `json:"node_count"`
		FinalOutput string                   `json:"final_output"`
		AbortReason string                   `json:"abort_reason,omitempty"`
		Nodes       []workplanTypes.NodeBase `json:"nodes,omitempty"`
	}{
		Status: status,
	}
	if err != nil {
		out.Error = err.Error()
	}
	if result != nil {
		nodes := make([]workplanTypes.NodeBase, 0, len(result.NodeResults))
		for _, nr := range result.NodeResults {
			if nr == nil {
				continue
			}
			nodeBase := nr.NodeBase
			if !withNodeOutputs {
				nodeBase.Output = "" // fork：结果瘦身，完整输出在子代理树/详情
			}
			nodes = append(nodes, nodeBase)
		}
		out.NodeCount = len(nodes)
		out.FinalOutput = result.FinalOutputString()
		out.AbortReason = result.AbortReason
		out.Nodes = nodes
	}
	encoded, marshalErr := json.Marshal(out)
	if marshalErr != nil {
		return "", marshalErr
	}
	return string(encoded), err
}

func (provider *ToolProvider) clearPlan(_ context.Context, _ string) (string, error) {
	provider.mu.Lock()
	provider.loaded = nil
	provider.mu.Unlock()
	return `{"status":"cleared"}`, nil
}

func (provider *ToolProvider) planStatus(_ context.Context, _ string) (string, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.loaded == nil {
		return `{"status":"none"}`, nil
	}
	doc := provider.loaded
	result, _ := json.Marshal(map[string]interface{}{
		"status": "loaded", "entry": doc.Entry, "node_count": doc.NodeCount, "edge_count": doc.EdgeCount,
	})
	return string(result), nil
}

func (provider *ToolProvider) planExport(_ context.Context, _ string) (string, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.loaded == nil {
		return "", fmt.Errorf("plan_export: no plan is loaded")
	}
	return provider.loaded.Canonical, nil
}

func (provider *ToolProvider) planValidate(_ context.Context, argsJSON string) (string, error) {
	var input struct {
		Plan string `json:"plan"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("plan_validate: invalid args: %w", err)
	}
	canonical, err := NormalizePlanLoadArguments(input.Plan)
	if err != nil {
		return "", fmt.Errorf("plan_validate: normalize DAG input: %w", err)
	}
	policy := PlanPolicy{}
	if provider.policy != nil {
		policy = provider.policy()
	}
	nodeCount, err := ValidatePolicyLoad(policy, canonical)
	if err != nil {
		return "", err
	}
	result, _ := json.Marshal(map[string]interface{}{"status": "valid", "node_count": nodeCount})
	return string(result), nil
}

// parseCanonicalPlan 从规范化 plan JSON 提取 entry/节点数/边数。
func parseCanonicalPlan(canonical string) (entry string, nodeCount, edgeCount int, err error) {
	var doc struct {
		Entry string                 `json:"entry"`
		Nodes map[string]interface{} `json:"nodes"`
		Edges map[string]interface{} `json:"edges"`
	}
	if err := json.Unmarshal([]byte(canonical), &doc); err != nil {
		return "", 0, 0, fmt.Errorf("plan_load: parse canonical plan: %w", err)
	}
	for _, targets := range doc.Edges {
		if list, ok := targets.([]interface{}); ok {
			edgeCount += len(list)
		}
	}
	return doc.Entry, len(doc.Nodes), edgeCount, nil
}
