package core

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	taskStatusRunning   = "running"
	taskStatusCompleted = "completed"
	taskStatusFailed    = "failed"

	taskCompleteTool = "task_complete"
	taskFailedTool   = "task_failed"
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
}

type taskExecutionState struct {
	requestID      string
	objective      string
	effort         string
	planArguments  string
	status         string
	checkpoints    map[string]*NodeCheckpoint
	toolSignatures map[string]struct{}
	progressEpoch  uint64
	terminal       *taskTerminal
	contextVersion uint64
	compactedEpoch uint64
}

func newTaskExecutionState(requestID, objective, effort string) *taskExecutionState {
	return &taskExecutionState{
		requestID: requestID, objective: objective, effort: effort, status: taskStatusRunning,
		checkpoints: make(map[string]*NodeCheckpoint), toolSignatures: make(map[string]struct{}),
	}
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
	state.progressEpoch++
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
	var out strings.Builder
	fmt.Fprintf(&out, "objective: %s\nstatus: %s\neffort: %s\n", state.objective, state.status, state.effort)
	if state.planArguments != "" {
		out.WriteString("authoritative plan is loaded; do not replace it.\n")
	}
	if evidence := state.evidenceText(); evidence != "" {
		out.WriteString("checkpoint evidence:\n")
		out.WriteString(evidence)
	}
	if state.terminal != nil {
		fmt.Fprintf(&out, "terminal=%s summary=%q\n", state.terminal.Kind, state.terminal.Summary)
	}
	return out.String()
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
	service.mu.Lock()
	defer service.mu.Unlock()
	state := service.taskExecution
	if state == nil || state.requestID != service.snapshot.Chat.RequestID || !service.snapshot.Chat.Running {
		return "", fmt.Errorf("%s: no active task execution", kind)
	}
	if state.terminal != nil {
		return "", fmt.Errorf("%s: task already reached %s", kind, state.terminal.Kind)
	}
	state.terminal = &input
	if kind == taskCompleteTool {
		state.status = taskStatusCompleted
	} else {
		state.status = taskStatusFailed
	}
	state.progressEpoch++
	encoded, _ := json.Marshal(map[string]string{"status": "accepted", "terminal": kind})
	return string(encoded), nil
}

// finalizeTaskExecution converts a natural model stop into an auditable
// implicit completion only when no WorkPlan remains active. An in-flight DAG
// is never silently treated as a completed task.
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
			return fmt.Errorf("task execution ended before authoritative plan reached a terminal state; call %s or %s", taskCompleteTool, taskFailedTool)
		case PlanFailed, PlanAborted:
			state.status = taskStatusFailed
			state.checkpoint("plan", "authoritative plan", string(plan.Status), "", "plan did not complete")
			return nil
		}
	}
	state.status = taskStatusCompleted
	state.terminal = &taskTerminal{
		Kind: taskCompleteTool, Summary: "Model returned a final response without an explicit terminal tool call.",
	}
	state.progressEpoch++
	return nil
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
