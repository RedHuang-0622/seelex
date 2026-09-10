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
	Seq    uint64 `json:"seq"`
	TaskID string `json:"task_id,omitempty"`
	// MessageID 是同一逻辑单元的 UI 会话消息定位键（event-to-message 索引，
	// 模块化方案 §3.2）；无法稳定配对时为空。
	MessageID string `json:"message_id,omitempty"`
	// Kind 是事件在多线谱中的显式类别（tool_call/llm/user_input/…）。
	// 空串 = 旧数据未标注，消费方用 Role/ToolCalls 回退分类。
	Kind             string               `json:"kind,omitempty"`
	Role             string               `json:"role"`
	ReasoningContent string               `json:"reasoning_content,omitempty"`
	Content          string               `json:"content,omitempty"`
	ToolCallID       string               `json:"tool_call_id,omitempty"`
	Name             string               `json:"name,omitempty"`
	ToolCalls        []TranscriptToolCall `json:"tool_calls,omitempty"`
	ResultRef        string               `json:"result_ref,omitempty"`
	TokenCount       int                  `json:"token_count"`
	// WireMaterial 标记内部 user 材料（internal/context 行）：true = 该行
	// 作为材料进入装配（S19/D8：检查点渲染正文等必须由生产方置位）。
	WireMaterial bool      `json:"wire_material,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	// 群聊角色归属与排序键（§8.3）：message 行必须可识别角色，否则 UI/审计/
	// 冷恢复不可辨。user/main/tl/agent-team（以及系统状态行 system）共用同一
	// 事件通道；这里的 RoleName 是 Seelex 逻辑角色，provider 请求的 Role 仍
	// 只能是标准 role（真实端点实验：自定义 tl 会被拒绝）。
	RoleName      string `json:"role_name,omitempty"`
	RoleSessionID string `json:"role_session_id,omitempty"`
	RoundID       uint64 `json:"round_id,omitempty"`
	UnitSeq       uint64 `json:"unit_seq,omitempty"`
}

type TranscriptToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// TranscriptEventKind 是历史记录条目的显式类别，供轨迹多线谱（tool/llm/input/…）
// 与恢复顺序重放直接识别，避免仅靠 role/content 启发式判定。
// 取值与 sessionstore.Event.Kind 一致；空串表示旧数据，读取方必须回退到 role 判定。
const (
	TranscriptEventKindUserInput  = "user_input"
	TranscriptEventKindInternal   = "internal"
	TranscriptEventKindLLM        = "llm"
	TranscriptEventKindToolCall   = "tool_call"
	TranscriptEventKindToolOutput = "tool_output"
	TranscriptEventKindSystem     = "system"
	TranscriptEventKindError      = "error"
	TranscriptEventKindNotice     = "notice"
)

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
