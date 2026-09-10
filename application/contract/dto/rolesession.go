package dto

import (
	"encoding/json"
	"time"
)

// 本文件是 R2/R4 群聊角色会话的应用层纯 DTO（S27 收口）。
//
// 边界：这些都是**数据形状**，不带行为方法、不 import 存储包。sessionstore 的
// `Event`/`RoleDraftRow`/`RoleSnapshot` 等类型只在 `internal/adapters` 里出现，
// 由适配器做 DTO ↔ 存储映射；`application/core` 与 `gui`/`tui` 只认这里的类型。
//
// 语义仍以 docs/arch/a2a-agent-team-factory.md 与 my_design §8.3 为准：
// role_name 只是 metadata，provider role 仍只有标准集；subagent 不属于任何
// RoleKind（它是 tool calling 能力）。

// RoleToolCall 是一行 message 里的工具调用载荷（与 provider tool_call 形状对齐）。
type RoleToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// RoleRow 是群聊 message 行 / 角色备份行的应用层投影。
// 字段与存储行一一对应，保证 headless 巡检与真实 API 冒烟的 JSON 形状不变。
type RoleRow struct {
	Seq              uint64          `json:"seq"`
	TaskID           string          `json:"task_id,omitempty"`
	MessageID        string          `json:"message_id,omitempty"`
	Kind             string          `json:"kind,omitempty"`
	Role             string          `json:"role"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
	Content          string          `json:"content,omitempty"`
	ToolCallID       string          `json:"tool_call_id,omitempty"`
	Name             string          `json:"name,omitempty"`
	ToolCalls        []RoleToolCall  `json:"tool_calls,omitempty"`
	ResultRef        string          `json:"result_ref,omitempty"`
	TokenCount       int             `json:"token_count"`
	CreatedAt        time.Time       `json:"created_at"`
	CommitID         string          `json:"commit_id,omitempty"`
	InOutJSON        json.RawMessage `json:"in_out_json,omitempty"`
	WireMaterial     bool            `json:"wire_material,omitempty"`
	RoleName         string          `json:"role_name,omitempty"`
	RoleSessionID    string          `json:"role_session_id,omitempty"`
	RoundID          uint64          `json:"round_id,omitempty"`
	UnitSeq          uint64          `json:"unit_seq,omitempty"`
}

// RoleSessionInfo 是角色会话的基础坐标（不含引擎内部对象）。
type RoleSessionInfo struct {
	MainSessionID string `json:"main_session_id"`
	RoleName      string `json:"role_name"`
	RoleSessionID string `json:"role_session_id"`
	Root          string `json:"root"`
}

// RoleDraftRow 是一行未同步角色草稿：Event 是最终要 append 进 message 的事件行，
// round_id/role_name/role_session_id/unit_seq/message_id 是 sequencer 的排序与
// 幂等凭据。
type RoleDraftRow struct {
	RoundID       uint64  `json:"round_id"`
	RoleName      string  `json:"role_name"`
	RoleSessionID string  `json:"role_session_id"`
	UnitSeq       uint64  `json:"unit_seq"`
	MessageID     string  `json:"message_id,omitempty"`
	Event         RoleRow `json:"event"`
}

// RoleDraftSyncResult 是一次 sequencer sync 的结果。AlreadySynced 表示该批 draft
// 已由同一 commit_id 发布过，本次只清理残留 draft，未重复 append。
type RoleDraftSyncResult struct {
	CommitID      string `json:"commit_id"`
	SyncedRows    int    `json:"synced_rows"`
	LastSeq       uint64 `json:"last_seq"`
	LastMessageID string `json:"last_message_id,omitempty"`
	AlreadySynced bool   `json:"already_synced"`
}

// RoleFloor 是当前发言权记录（唯一写者 = sequencer，随 message head 发布）。
type RoleFloor struct {
	RoleName      string    `json:"role_name"`
	RoleSessionID string    `json:"role_session_id,omitempty"`
	RoundID       uint64    `json:"round_id,omitempty"`
	Seq           uint64    `json:"seq,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// CompactFrameRef 是角色会话对 main compact 帧的引用（角色不生成第二份帧）。
type CompactFrameRef struct {
	FrameID    string `json:"frame_id"`
	AppliedSeq uint64 `json:"applied_seq"`
}

// RoleSnapshot 是角色会话的只读观察面（headless 巡检 / 真实 API 冒烟用）：
// main 正文 + 角色备份 + 未同步 draft + floor + join/compact 切点。只读，不写状态。
type RoleSnapshot struct {
	MainSessionID string `json:"main_session_id"`
	RoleName      string `json:"role_name"`
	RoleSessionID string `json:"role_session_id"`
	Root          string `json:"root,omitempty"`

	JoinSeqID   uint64           `json:"join_seq_id,omitempty"`
	CompactRef  *CompactFrameRef `json:"compact_ref,omitempty"`
	OrderPolicy string           `json:"order_policy,omitempty"`
	OrderRoles  []string         `json:"order_roles,omitempty"`

	MainHeadCommitID string     `json:"main_head_commit_id,omitempty"`
	MainHeadSeq      uint64     `json:"main_head_seq,omitempty"`
	Floor            *RoleFloor `json:"floor,omitempty"`

	MainRows  []RoleRow      `json:"main_rows,omitempty"`
	RoleRows  []RoleRow      `json:"role_rows,omitempty"`
	DraftRows []RoleDraftRow `json:"draft_rows,omitempty"`

	// UnassignedRoleRows 是 main message 中缺 role_name 的行数（R4 生产者覆盖度）。
	UnassignedRoleRows int `json:"unassigned_role_rows,omitempty"`
	// DesignWarnings 是存储层可判定的设计稿偏差（只报事实，不自动修补）。
	DesignWarnings []string `json:"design_warnings,omitempty"`
}

// RoleWireMessage 是角色可见 wire 的公开形态（与 provider 消息字段对齐）。
type RoleWireMessage struct {
	Role             string         `json:"role"`
	Content          string         `json:"content,omitempty"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	ToolCalls        []RoleToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
	Name             string         `json:"name,omitempty"`
	ResultRef        string         `json:"result_ref,omitempty"`
	Seq              uint64         `json:"seq,omitempty"`
	Internal         bool           `json:"internal,omitempty"`
	Repair           bool           `json:"repair,omitempty"`
	Attempt          bool           `json:"attempt,omitempty"`
}

// RoleWireSnapshot 是角色物化 wire 的观察面：main compact 引用 +
// seq > max(join_seq_id, compact_ref.applied_seq) 的已发布行 + 自身 pending draft。
type RoleWireSnapshot struct {
	MainSessionID string `json:"main_session_id"`
	RoleName      string `json:"role_name"`
	RoleSessionID string `json:"role_session_id"`
	AppliedSeq    uint64 `json:"applied_seq"`

	Messages     []RoleWireMessage `json:"messages"`
	NeedCompact  bool              `json:"need_compact"`
	PrefixDigest string            `json:"prefix_digest"`
	TailStartSeq uint64            `json:"tail_start_seq"`
	FrameApplied bool              `json:"frame_applied,omitempty"`
	Open         bool              `json:"open,omitempty"`
	PendingRows  int               `json:"pending_rows,omitempty"`

	DesignWarnings []string `json:"design_warnings,omitempty"`
}

// ScheduleEventPayload 是定时任务式插话 EVENT 的 payload（schedule.registered /
// schedule.cancelled / schedule.fired）；冷启动按这些事件重放重建 timer。
type ScheduleEventPayload struct {
	ScheduleID    string `json:"schedule_id"`
	RoleName      string `json:"role_name"`
	RoleSessionID string `json:"role_session_id"`
	Cron          string `json:"cron,omitempty"`
	Interval      string `json:"interval,omitempty"`
	NextFireAt    string `json:"next_fire_at,omitempty"`
	PayloadRef    string `json:"payload_ref,omitempty"`
}
