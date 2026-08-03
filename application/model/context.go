package model

import "time"

const TaskContextProjectionSchemaVersion = 1

// ActiveSkill records one installed and content-addressed Skill activated for
// the current task. Prompt content is deliberately not persisted here.
type ActiveSkill struct {
	SkillID     string    `json:"skill_id"`
	Version     string    `json:"version"`
	ContentHash string    `json:"content_hash"`
	Scope       string    `json:"scope"`
	ActivatedAt time.Time `json:"activated_at"`
	SourceEvent uint64    `json:"source_event"`
}

// ActivePlanProjection is the executable slice of a durable Plan revision.
// CanonicalPlanRef points at SessionRecord.PlanStack; it is never reconstructed
// from a historical plan_load message.
type ActivePlanProjection struct {
	PlanID           string   `json:"plan_id"`
	Version          uint64   `json:"version"`
	CanonicalPlanRef string   `json:"canonical_plan_ref"`
	Status           string   `json:"status"`
	CurrentNode      string   `json:"current_node,omitempty"`
	CompletedNodes   []string `json:"completed_nodes,omitempty"`
	FailedNodes      []string `json:"failed_nodes,omitempty"`
	PendingNodes     []string `json:"pending_nodes,omitempty"`
}

type EventRange struct {
	Start uint64 `json:"start"`
	End   uint64 `json:"end"`
}

// TaskCheckpoint contains bounded facts and references only. Raw tool output,
// model reasoning, and prompt text are intentionally excluded.
type TaskCheckpoint struct {
	Version          uint64     `json:"version"`
	CoversEventRange EventRange `json:"covers_event_range"`
	CompletedWork    []string   `json:"completed_work,omitempty"`
	PendingWork      []string   `json:"pending_work,omitempty"`
	Decisions        []string   `json:"decisions,omitempty"`
	Failures         []string   `json:"failures,omitempty"`
	ChangedFiles     []string   `json:"changed_files,omitempty"`
	Artifacts        []string   `json:"artifacts,omitempty"`
	ToolResultRefs   []string   `json:"tool_result_refs,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type TokenAudit struct {
	Model                 string    `json:"model,omitempty"`
	Counter               string    `json:"counter"`
	Budget                int       `json:"budget"`
	SoftThreshold         int       `json:"soft_threshold"`
	HardThreshold         int       `json:"hard_threshold"`
	TargetAfterCompaction int       `json:"target_after_compaction"`
	EstimatedPromptTokens int       `json:"estimated_prompt_tokens"`
	ActualPromptTokens    int       `json:"actual_prompt_tokens,omitempty"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// TaskContextProjection is the versioned source used to rebuild bounded
// provider context after compaction, interruption, or process restart.
type TaskContextProjection struct {
	SchemaVersion int                   `json:"schema_version"`
	ProjectID     string                `json:"project_id,omitempty"`
	SessionID     string                `json:"session_id"`
	TaskID        string                `json:"task_id"`
	Status        string                `json:"status"`
	ObjectiveRef  string                `json:"objective_ref,omitempty"`
	ActiveSkills  []ActiveSkill         `json:"active_skills,omitempty"`
	ActivePlan    *ActivePlanProjection `json:"active_plan,omitempty"`
	Checkpoint    TaskCheckpoint        `json:"checkpoint"`
	TokenAudit    TokenAudit            `json:"token_audit"`
	UpdatedAt     time.Time             `json:"updated_at"`
}

// TranscriptEvent is an append-only protocol event. Oversized tool content is
// represented by ResultRef; the raw value lives in ToolResultStore.
type TranscriptEvent struct {
	Seq              uint64               `json:"seq"`
	TaskID           string               `json:"task_id,omitempty"`
	Role             string               `json:"role"`
	ReasoningContent string               `json:"reasoning_content,omitempty"`
	Content          string               `json:"content,omitempty"`
	ToolCallID       string               `json:"tool_call_id,omitempty"`
	Name             string               `json:"name,omitempty"`
	ToolCalls        []TranscriptToolCall `json:"tool_calls,omitempty"`
	ResultRef        string               `json:"result_ref,omitempty"`
	TokenCount       int                  `json:"token_count"`
	CreatedAt        time.Time            `json:"created_at"`
}

type TranscriptToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolResultRef is the durable metadata stored in SessionRecord. Content is
// persisted independently and addressed only through Ref.
type ToolResultRef struct {
	Ref        string    `json:"ref"`
	Tool       string    `json:"tool"`
	Digest     string    `json:"digest"`
	Size       int       `json:"size"`
	TokenCount int       `json:"token_count"`
	CreatedAt  time.Time `json:"created_at"`
}

// StoredToolResult is the application-to-storage commit payload. Content is
// never serialized into SessionRecord or frontend snapshots.
type StoredToolResult struct {
	ToolResultRef
	Content string `json:"-"`
}
