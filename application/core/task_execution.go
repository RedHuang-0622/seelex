package core

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/seelexctx"
)

const (
	taskStatusRunning           = "running"
	taskStatusCompleted         = "completed"
	taskStatusNeedsUserDecision = "needs_user_decision"
	taskStatusBlocked           = "blocked"
	taskStatusInterrupted       = "interrupted"
	taskStatusFailed            = "failed"

	taskCompleteTool          = "task_complete"
	taskNeedsUserDecisionTool = "task_needs_user_decision"
	taskFailedTool            = "task_failed"
)

// NodeCheckpoint is a bounded, evidence-first record for one Plan node. It
// deliberately excludes model reasoning and raw command logs.
type NodeCheckpoint struct {
	NodeKey       string    `json:"node_key"`
	Objective     string    `json:"objective,omitempty"`
	Status        string    `json:"status"`
	Facts         []string  `json:"facts,omitempty"`
	Evidence      []string  `json:"evidence,omitempty"`
	ChangedFiles  []string  `json:"changed_files,omitempty"`
	Artifacts     []string  `json:"artifacts,omitempty"`
	RemainingWork []string  `json:"remaining_work,omitempty"`
	Failure       string    `json:"failure,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type taskTerminal struct {
	Kind              string   `json:"kind"`
	Summary           string   `json:"summary"`
	CompletedNodes    []string `json:"completed_nodes,omitempty"`
	Artifacts         []string `json:"artifacts,omitempty"`
	Evidence          []string `json:"evidence,omitempty"`
	RemainingRisks    []string `json:"remaining_risks,omitempty"`
	FailureType       string   `json:"failure_type,omitempty"`
	FailedNode        string   `json:"failed_node,omitempty"`
	PartialProgress   []string `json:"partial_progress,omitempty"`
	ReplanRecommended bool     `json:"replan_recommended,omitempty"`
	DecisionQuestion  string   `json:"decision_question,omitempty"`
	DecisionOptions   []string `json:"decision_options,omitempty"`
}

type taskExecutionState struct {
	requestID           string
	objective           string
	effort              string
	planArguments       string
	status              string
	checkpoints         map[string]*NodeCheckpoint
	toolSignatures      map[string]struct{}
	toolOutcomes        []string
	progressEpoch       uint64
	terminal            *taskTerminal
	contextVersion      uint64
	compactedEpoch      uint64
	contextCompactions  []ContextCompaction
	activeSkills        []ActiveSkill
	trustedSkillLayers  []PromptLayer
	tokenAudit          TokenAudit
	inheritedCheckpoint *TaskCheckpoint
}

func newTaskExecutionState(requestID, objective, effort string) *taskExecutionState {
	return &taskExecutionState{
		requestID: requestID, objective: objective, effort: effort, status: taskStatusRunning,
		checkpoints: make(map[string]*NodeCheckpoint), toolSignatures: make(map[string]struct{}), contextVersion: 1,
	}
}

func continuationTaskExecutionState(requestID, objective, effort string, previous *taskExecutionState, checkpoint TaskCheckpoint) *taskExecutionState {
	state := newTaskExecutionState(requestID, objective, effort)
	if previous == nil || !isContinuableTaskStatus(previous.status) {
		return state
	}
	if strings.TrimSpace(previous.objective) != "" {
		state.objective = previous.objective
	}
	state.planArguments = previous.planArguments
	state.checkpoints = cloneNodeCheckpoints(previous.checkpoints)
	state.toolSignatures = make(map[string]struct{}, len(previous.toolSignatures))
	for signature := range previous.toolSignatures {
		state.toolSignatures[signature] = struct{}{}
	}
	state.toolOutcomes = append([]string(nil), previous.toolOutcomes...)
	state.progressEpoch = previous.progressEpoch + 1
	state.contextVersion = previous.contextVersion
	if state.contextVersion == 0 {
		state.contextVersion = 1
	}
	state.contextCompactions = append([]ContextCompaction(nil), previous.contextCompactions...)
	state.tokenAudit = previous.tokenAudit
	cloned := cloneTaskCheckpoint(checkpoint)
	state.inheritedCheckpoint = &cloned
	return state
}

func isContinuableTaskStatus(status string) bool {
	switch status {
	case taskStatusRunning, taskStatusInterrupted, taskStatusBlocked, taskStatusNeedsUserDecision:
		return true
	default:
		return false
	}
}

func cloneNodeCheckpoints(source map[string]*NodeCheckpoint) map[string]*NodeCheckpoint {
	cloned := make(map[string]*NodeCheckpoint, len(source))
	for key, checkpoint := range source {
		if checkpoint == nil {
			continue
		}
		copy := *checkpoint
		copy.Facts = append([]string(nil), checkpoint.Facts...)
		copy.Evidence = append([]string(nil), checkpoint.Evidence...)
		copy.ChangedFiles = append([]string(nil), checkpoint.ChangedFiles...)
		copy.Artifacts = append([]string(nil), checkpoint.Artifacts...)
		copy.RemainingWork = append([]string(nil), checkpoint.RemainingWork...)
		cloned[key] = &copy
	}
	return cloned
}

func (state *taskExecutionState) recordTool(name, result string, toolErr error) {
	if state == nil {
		return
	}
	fingerprint := name + "\x00" + result
	if toolErr != nil {
		fingerprint += "\x00" + toolErr.Error()
	}
	sum := sha256.Sum256([]byte(fingerprint))
	key := fmt.Sprintf("%x", sum[:])
	if _, exists := state.toolSignatures[key]; exists {
		return
	}
	state.toolSignatures[key] = struct{}{}
	if outcome := taskToolOutcome(name, result, toolErr); outcome != "" {
		state.toolOutcomes = append(state.toolOutcomes, outcome)
	}
	state.progressEpoch++
}

func taskToolOutcome(name, result string, toolErr error) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if toolErr != nil {
		return fmt.Sprintf("tool=%s status=error detail=%q", name, boundedEvidence(toolErr.Error()))
	}
	if evidence := boundedEvidence(result); evidence != "" {
		return fmt.Sprintf("tool=%s status=completed result=%q", name, evidence)
	}
	return fmt.Sprintf("tool=%s status=completed", name)
}

func (state *taskExecutionState) checkpoint(nodeKey, objective, status, output, failure string) {
	if state == nil || strings.TrimSpace(nodeKey) == "" {
		return
	}
	checkpoint := state.checkpoints[nodeKey]
	if checkpoint == nil {
		checkpoint = &NodeCheckpoint{NodeKey: nodeKey, Objective: objective}
		state.checkpoints[nodeKey] = checkpoint
	}
	changed := checkpoint.Status != status || checkpoint.Failure != failure
	checkpoint.Status, checkpoint.Failure, checkpoint.UpdatedAt = status, failure, time.Now()
	if fact := boundedEvidence(output); fact != "" && !containsString(checkpoint.Facts, fact) {
		checkpoint.Facts = append(checkpoint.Facts, fact)
		changed = true
	}
	if changed {
		state.progressEpoch++
	}
}

func (state *taskExecutionState) evidenceText() string {
	if state == nil {
		return ""
	}
	keys := make([]string, 0, len(state.checkpoints))
	for key := range state.checkpoints {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out strings.Builder
	for _, key := range keys {
		checkpoint := state.checkpoints[key]
		fmt.Fprintf(&out, "node=%s status=%s", checkpoint.NodeKey, checkpoint.Status)
		if checkpoint.Failure != "" {
			fmt.Fprintf(&out, " failure=%q", checkpoint.Failure)
		}
		for _, fact := range checkpoint.Facts {
			fmt.Fprintf(&out, " fact=%q", fact)
		}
		out.WriteByte('\n')
	}
	return out.String()
}

func (state *taskExecutionState) contextSummary() string {
	if state == nil {
		return ""
	}
	limit := seelexctx.DefaultContextConfig().MaxToolResultChars
	var out strings.Builder
	appendContextSummary(&out, limit, fmt.Sprintf("objective: %s\nstatus: %s\neffort: %s\n", state.objective, state.status, state.effort))
	if state.planArguments != "" {
		appendContextSummary(&out, limit, "authoritative plan is loaded; do not replace it.\n")
	}
	if state.inheritedCheckpoint != nil {
		appendContextSummary(&out, limit, checkpointSummary(*state.inheritedCheckpoint))
	}
	if evidence := state.evidenceText(); evidence != "" {
		appendContextSummary(&out, limit, "checkpoint evidence:\n"+evidence)
	}
	if len(state.toolOutcomes) > 0 {
		if appendContextSummary(&out, limit, "completed tool outcomes (do not repeat unless more detail is needed):\n") {
			// Most-recent observations are the most actionable after a recovery.
			for index := len(state.toolOutcomes) - 1; index >= 0; index-- {
				if !appendContextSummary(&out, limit, state.toolOutcomes[index]+"\n") {
					break
				}
			}
		}
	}
	if state.terminal != nil {
		appendContextSummary(&out, limit, fmt.Sprintf("terminal=%s summary=%q\n", state.terminal.Kind, state.terminal.Summary))
	}
	return out.String()
}

func checkpointSummary(checkpoint TaskCheckpoint) string {
	var out strings.Builder
	for _, item := range checkpoint.CompletedWork {
		fmt.Fprintf(&out, "completed=%q\n", item)
	}
	for _, item := range checkpoint.PendingWork {
		fmt.Fprintf(&out, "pending=%q\n", item)
	}
	for _, item := range checkpoint.Failures {
		fmt.Fprintf(&out, "failure=%q\n", item)
	}
	for _, item := range checkpoint.Decisions {
		fmt.Fprintf(&out, "decision=%q\n", item)
	}
	return out.String()
}

// appendContextSummary keeps checkpoints beneath the same provider-context
// budget that bounds individual tool results. It admits complete structured
// records only, so a recovery never receives a misleading half line.
func appendContextSummary(out *strings.Builder, limit int, value string) bool {
	if value == "" || limit <= out.Len() {
		return false
	}
	if out.Len()+len(value) > limit {
		if out.Len()+len("additional checkpoint detail remains in session storage; re-read it only if needed.\n") <= limit {
			out.WriteString("additional checkpoint detail remains in session storage; re-read it only if needed.\n")
		}
		return false
	}
	out.WriteString(value)
	return true
}

// TaskTerminalHandler returns a Runtime-facing handler while keeping request
// state owned by Application. The handler has no external side effects.
func (service *Service) TaskTerminalHandler(kind string) func(context.Context, string) (string, error) {
	return func(_ context.Context, argsJSON string) (string, error) {
		return service.recordTaskTerminal(kind, argsJSON)
	}
}

func (service *Service) recordTaskTerminal(kind, argsJSON string) (string, error) {
	var input taskTerminal
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("%s: invalid JSON: %w", kind, err)
	}
	input.Kind = kind
	input.Summary = strings.TrimSpace(input.Summary)
	if input.Summary == "" {
		return "", fmt.Errorf("%s: summary is required", kind)
	}
	if kind == taskFailedTool && strings.TrimSpace(input.FailureType) == "" {
		return "", fmt.Errorf("%s: failure_type is required", kind)
	}
	if kind == taskNeedsUserDecisionTool && strings.TrimSpace(input.DecisionQuestion) == "" {
		return "", fmt.Errorf("%s: decision_question is required", kind)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	state := service.taskExecution
	if state == nil || state.requestID != service.snapshot.Chat.RequestID || !service.snapshot.Chat.Running {
		return "", fmt.Errorf("%s: no active task execution", kind)
	}
	if state.terminal != nil {
		return "", fmt.Errorf("%s: task already reached %s", kind, state.terminal.Kind)
	}
	if kind == taskCompleteTool {
		if err := service.completeAuthoritativePlanLocked(input.CompletedNodes); err != nil {
			return "", err
		}
	}
	state.terminal = &input
	switch kind {
	case taskCompleteTool:
		state.status = taskStatusCompleted
		service.setTaskStateLocked(state.requestID, TaskCompleted, input.Summary)
	case taskNeedsUserDecisionTool:
		state.status = taskStatusNeedsUserDecision
		service.setTaskStateLocked(state.requestID, TaskNeedsUserDecision, input.Summary)
	case taskFailedTool:
		if input.FailureType == "blocked" || input.FailureType == "external_dependency" {
			state.status = taskStatusBlocked
			service.setTaskStateLocked(state.requestID, TaskBlocked, input.Summary)
		} else {
			state.status = taskStatusFailed
			service.setTaskStateLocked(state.requestID, TaskFailed, input.Summary)
		}
	default:
		return "", fmt.Errorf("unsupported task terminal %q", kind)
	}
	state.progressEpoch++
	encoded, _ := json.Marshal(map[string]string{"status": "accepted", "terminal": kind})
	return string(encoded), nil
}

func (service *Service) completeAuthoritativePlanLocked(completedNodes []string) error {
	plan := service.snapshot.Runtime.Plan
	if plan == nil {
		return nil
	}
	completed := make(map[string]struct{}, len(completedNodes))
	for _, nodeID := range completedNodes {
		completed[nodeID] = struct{}{}
	}
	for _, node := range plan.Nodes {
		if _, ok := completed[node.ID]; !ok {
			return fmt.Errorf("%s: completed_nodes must include authoritative plan node %q", taskCompleteTool, node.ID)
		}
	}
	for index := range plan.Nodes {
		node := &plan.Nodes[index]
		node.Status = NodeCompleted
	}
	plan.Status = PlanCompleted
	plan.Progress = 1
	return nil
}

// finalizeTaskExecution converts a natural model stop into an auditable
// completion or handoff. An in-flight authoritative Plan is not silently
// completed; if the model stops before executing it, the visible result is a
// user-decision handoff rather than an opaque runtime error.
func (service *Service) finalizeTaskExecution(requestID string) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	state := service.taskExecution
	if state == nil || state.requestID != requestID || state.terminal != nil {
		return nil
	}
	if plan := service.snapshot.Runtime.Plan; plan != nil {
		switch plan.Status {
		case PlanPending, PlanRunning:
			state.status = taskStatusNeedsUserDecision
			state.terminal = &taskTerminal{
				Kind:             taskNeedsUserDecisionTool,
				Summary:          "The authoritative plan is ready but has not been executed.",
				DecisionQuestion: "Should Seelex execute the loaded plan, revise it, or stop here?",
				DecisionOptions:  []string{"execute", "revise", "stop"},
			}
			service.setTaskStateLocked(requestID, TaskNeedsUserDecision, "Plan is ready but not executed. Choose whether to execute it, revise it, or stop here.")
			state.progressEpoch++
			return nil
		case PlanFailed, PlanAborted:
			state.status = taskStatusFailed
			state.checkpoint("plan", "authoritative plan", string(plan.Status), "", "plan did not complete")
			service.setTaskStateLocked(requestID, TaskFailed, "The authoritative plan did not reach completion.")
			return nil
		}
	}
	state.status = taskStatusCompleted
	state.terminal = &taskTerminal{
		Kind: taskCompleteTool, Summary: "Model returned a final response without an explicit terminal tool call.",
	}
	service.setTaskStateLocked(requestID, TaskCompleted, state.terminal.Summary)
	state.progressEpoch++
	return nil
}

func (service *Service) setTaskStateLocked(requestID string, status TaskStatus, summary string) {
	var compactions []ContextCompaction
	if state := service.taskExecution; state != nil && state.requestID == requestID {
		compactions = append([]ContextCompaction(nil), state.contextCompactions...)
	}
	service.snapshot.Task = &TaskState{
		RequestID:          requestID,
		Status:             status,
		Summary:            strings.TrimSpace(summary),
		ContextCompactions: compactions,
		UpdatedAt:          time.Now(),
	}
}

func (service *taskContextCoordinator) recordContextCompactionLocked(requestID string, compaction ContextCompaction) bool {
	state := service.taskExecution
	if state == nil || state.requestID != requestID || state.status != taskStatusRunning {
		return false
	}
	state.contextCompactions = append(state.contextCompactions, compaction)
	if task := service.snapshot.Task; task != nil && task.RequestID == requestID {
		task.ContextCompactions = append([]ContextCompaction(nil), state.contextCompactions...)
		task.UpdatedAt = compaction.CompactedAt
	}
	return true
}

func boundedEvidence(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	const maxEvidenceChars = 800
	if len(value) > maxEvidenceChars {
		return value[:maxEvidenceChars] + "…"
	}
	return value
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
