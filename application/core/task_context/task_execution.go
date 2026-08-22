package task_context

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/internal/limits"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/application/prompt"
)

const (
	StatusRunning           = "running"
	StatusCompleted         = "completed"
	StatusNeedsUserDecision = "needs_user_decision"
	StatusBlocked           = "blocked"
	StatusInterrupted       = "interrupted"
	StatusFailed            = "failed"

	ToolComplete          = "task_complete"
	ToolCheckNode         = "task_check_node"
	ToolNeedsUserDecision = "task_needs_user_decision"
	ToolFailed            = "task_failed"
)

// NodeCheckpoint 是一个 Plan 节点的有界、证据优先记录（刻意排除模型推理
// 与原始命令日志）。
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

// taskTerminal 是一个终态/打点工具的应用输入（task_complete / task_failed /
// task_needs_user_decision / task_check_node）。
type taskTerminal struct {
	Kind              string   `json:"kind"`
	Summary           string   `json:"summary"`
	NodeID            string   `json:"node_id,omitempty"`
	Output            string   `json:"output,omitempty"`
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

// TaskExecutionState 是单个任务的功能打点快照与终态判定输入。任务开始创建，
// 终态即结束；它不持久化、不承担会话恢复（恢复经 checkpoint/transcript）。
type TaskExecutionState struct {
	RequestID          string
	Objective          string
	Effort             string
	PlanArguments      string
	Status             string
	CompactedEpoch     uint64
	ProgressEpoch      uint64
	ContextVersion     uint64
	TokenAudit         model.TokenAudit
	ActiveSkills       []model.ActiveSkill
	TrustedSkillLayers []prompt.PromptLayer
	ContextCompactions []model.ContextCompaction

	checkpoints         map[string]*NodeCheckpoint
	toolSignatures      map[string]struct{}
	toolOutcomes        []string
	Terminal            *taskTerminal
	InheritedCheckpoint *model.TaskCheckpoint
}

// NewTaskExecutionState 构造一个运行中任务状态。
func NewTaskExecutionState(requestID, objective, effort string) *TaskExecutionState {
	return &TaskExecutionState{
		RequestID: requestID, Objective: objective, Effort: effort, Status: StatusRunning,
		checkpoints: make(map[string]*NodeCheckpoint), toolSignatures: make(map[string]struct{}), ContextVersion: 1,
	}
}

func continuationTaskExecutionState(requestID, objective, effort string, previous *TaskExecutionState, checkpoint model.TaskCheckpoint) *TaskExecutionState {
	state := NewTaskExecutionState(requestID, objective, effort)
	if previous == nil || !IsContinuableStatus(previous.Status) {
		return state
	}
	if strings.TrimSpace(previous.Objective) != "" {
		state.Objective = previous.Objective
	}
	state.PlanArguments = previous.PlanArguments
	state.checkpoints = cloneNodeCheckpoints(previous.checkpoints)
	state.toolSignatures = make(map[string]struct{}, len(previous.toolSignatures))
	for signature := range previous.toolSignatures {
		state.toolSignatures[signature] = struct{}{}
	}
	state.toolOutcomes = append([]string(nil), previous.toolOutcomes...)
	state.ProgressEpoch = previous.ProgressEpoch + 1
	state.ContextVersion = previous.ContextVersion
	if state.ContextVersion == 0 {
		state.ContextVersion = 1
	}
	state.ContextCompactions = append([]model.ContextCompaction(nil), previous.ContextCompactions...)
	state.TokenAudit = previous.TokenAudit
	if HasSubstantiveCheckpoint(checkpoint) {
		cloned := CloneTaskCheckpoint(checkpoint)
		state.InheritedCheckpoint = &cloned
	}
	return state
}

// IsContinuableStatus 判定任务状态是否可被续接（排队输入/恢复路径）。
func IsContinuableStatus(status string) bool {
	switch status {
	case StatusRunning, StatusInterrupted, StatusBlocked, StatusNeedsUserDecision:
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

// RecordTool 记录一次工具执行观测（tool 签名去重 + progressEpoch 推进）。
func (state *TaskExecutionState) RecordTool(name, result string, toolErr error) {
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
	state.ProgressEpoch++
}

func taskToolOutcome(name, result string, toolErr error) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if toolErr != nil {
		return fmt.Sprintf("tool=%s status=error detail=%q", name, BoundedEvidence(toolErr.Error()))
	}
	if evidence := BoundedEvidence(result); evidence != "" {
		return fmt.Sprintf("tool=%s status=completed result=%q", name, evidence)
	}
	return fmt.Sprintf("tool=%s status=completed", name)
}

// Checkpoint 记录一个节点打点（节点终态/证据；推进 progressEpoch）。
func (state *TaskExecutionState) Checkpoint(nodeKey, objective, status, output, failure string) {
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
	if fact := BoundedEvidence(output); fact != "" && !ContainsString(checkpoint.Facts, fact) {
		checkpoint.Facts = append(checkpoint.Facts, fact)
		changed = true
	}
	if changed {
		state.ProgressEpoch++
	}
}

// NodeCheckpoint 返回指定节点的打点记录（未找到 → nil）。
func (state *TaskExecutionState) NodeCheckpoint(nodeKey string) *NodeCheckpoint {
	if state == nil {
		return nil
	}
	return state.checkpoints[nodeKey]
}

// EvidenceText 返回节点打点的证据文本（replan 请求/恢复摘要用）。
func (state *TaskExecutionState) EvidenceText() string {
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

// ContextSummary 返回任务的恢复摘要（objective/plan/checkpoint/evidence/
// terminal；无实质内容 → ""）。
func (state *TaskExecutionState) ContextSummary() string {
	if state == nil {
		return ""
	}
	checkpoint := model.TaskCheckpoint{}
	if state.InheritedCheckpoint != nil {
		checkpoint = *state.InheritedCheckpoint
	}
	evidence := state.EvidenceText()
	meaningful := strings.TrimSpace(state.Objective) != "" || state.PlanArguments != "" ||
		HasSubstantiveCheckpoint(checkpoint) || evidence != "" || len(state.toolOutcomes) > 0 ||
		(state.Terminal != nil && strings.TrimSpace(state.Terminal.Summary) != "")
	if !meaningful {
		return ""
	}
	limit := limits.Get().MaxToolResultChars
	var out strings.Builder
	appendContextSummary(&out, limit, fmt.Sprintf("objective: %s\nstatus: %s\neffort: %s\n", state.Objective, state.Status, state.Effort))
	if state.PlanArguments != "" {
		appendContextSummary(&out, limit, "authoritative plan is loaded; do not replace it.\n")
	}
	if state.InheritedCheckpoint != nil {
		appendContextSummary(&out, limit, checkpointSummary(*state.InheritedCheckpoint))
	}
	if evidence != "" {
		appendContextSummary(&out, limit, "checkpoint evidence:\n"+evidence)
	}
	if len(state.toolOutcomes) > 0 {
		if appendContextSummary(&out, limit, "completed tool outcomes (do not repeat unless more detail is needed):\n") {
			for index := len(state.toolOutcomes) - 1; index >= 0; index-- {
				if !appendContextSummary(&out, limit, state.toolOutcomes[index]+"\n") {
					break
				}
			}
		}
	}
	if state.Terminal != nil {
		appendContextSummary(&out, limit, fmt.Sprintf("terminal=%s summary=%q\n", state.Terminal.Kind, state.Terminal.Summary))
	}
	return out.String()
}

func checkpointSummary(checkpoint model.TaskCheckpoint) string {
	if !HasSubstantiveCheckpoint(checkpoint) {
		return ""
	}
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

// HasSubstantiveCheckpoint 区分可恢复任务事实与上下文过渡期的仅元数据标记。
func HasSubstantiveCheckpoint(checkpoint model.TaskCheckpoint) bool {
	return len(checkpoint.CompletedWork) > 0 || len(checkpoint.PendingWork) > 0 ||
		len(checkpoint.Decisions) > 0 || len(checkpoint.Failures) > 0 ||
		len(checkpoint.ChangedFiles) > 0 || len(checkpoint.Artifacts) > 0 ||
		len(checkpoint.ToolResultRefs) > 0
}

// appendContextSummary 保持 checkpoint 在 provider 上下文预算内（只接收
// 完整结构化记录，恢复绝不收到误导性的半行）。
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

// BoundedEvidence 把证据文本截断到 limits.evidence_chars（默认 800）。
func BoundedEvidence(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	maxChars := limits.Get().EvidenceChars // limits.evidence_chars（默认 800）
	if len(value) > maxChars {
		return value[:maxChars] + "…"
	}
	return value
}

// ContainsString 判断 values 是否包含 want。
func ContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// CloneTaskCheckpoint 深拷贝 checkpoint 的 slice 字段。
func CloneTaskCheckpoint(checkpoint model.TaskCheckpoint) model.TaskCheckpoint {
	checkpoint.CompletedWork = append([]string(nil), checkpoint.CompletedWork...)
	checkpoint.PendingWork = append([]string(nil), checkpoint.PendingWork...)
	checkpoint.Decisions = append([]string(nil), checkpoint.Decisions...)
	checkpoint.Failures = append([]string(nil), checkpoint.Failures...)
	checkpoint.ChangedFiles = append([]string(nil), checkpoint.ChangedFiles...)
	checkpoint.Artifacts = append([]string(nil), checkpoint.Artifacts...)
	checkpoint.ToolResultRefs = append([]string(nil), checkpoint.ToolResultRefs...)
	return checkpoint
}
