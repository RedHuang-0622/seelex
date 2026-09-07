// Package model defines the versioned application DTOs shared with clients.
package model

import (
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// ProtocolVersion identifies the Snapshot/Event contract consumed by frontends.
const ProtocolVersion = 1

type Snapshot struct {
	ProtocolVersion    int               `json:"protocol_version"`
	Revision           uint64            `json:"revision"`
	Session            SessionState      `json:"session"`
	Sessions           []SessionInfo     `json:"sessions"`
	Conversation       []Message         `json:"conversation"`
	Chat               ChatState         `json:"chat"`
	Task               *TaskState        `json:"task,omitempty"`
	Runtime            RuntimeState      `json:"runtime"`
	Interaction        *Interaction      `json:"interaction,omitempty"`
	Capabilities       Capabilities      `json:"capabilities"`
	HistoryOffset      int               `json:"history_offset"`
	TotalMessages      int               `json:"total_messages"`
	HasMoreHistory     bool              `json:"has_more_history"`
	ConversationWindow int               `json:"conversation_window"`
	ReadFiles          []ReadFileRef     `json:"read_files,omitempty"`
	Workspaces         []WorkspaceInfo   `json:"workspaces,omitempty"`
	CurrentWorkspace   *WorkspaceInfo    `json:"current_workspace,omitempty"`
	SessionWorkspaces  map[string]string `json:"session_workspaces,omitempty"`
}

// TaskState is the user-visible, evidence-oriented outcome of the latest
// request. It intentionally describes execution state rather than model
// internals or prompt content.
type TaskState struct {
	RequestID          string              `json:"request_id,omitempty"`
	Status             TaskStatus          `json:"status"`
	Summary            string              `json:"summary,omitempty"`
	ContextCompactions []ContextCompaction `json:"context_compactions,omitempty"`
	UpdatedAt          time.Time           `json:"updated_at,omitempty"`
}

// ContextCompaction is a user-visible record that the active provider context
// was condensed. It intentionally contains no prompt text, checkpoint body,
// tool argument, tool result, or conversation content.
type ContextCompaction struct {
	Version         uint64    `json:"version"`
	Reason          string    `json:"reason"`
	MessagesBefore  int       `json:"messages_before"`
	EstimatedTokens int       `json:"estimated_tokens"`
	CompactedAt     time.Time `json:"compacted_at"`
}

type TaskStatus string

const (
	TaskProgressing       TaskStatus = "progressing"
	TaskCompleted         TaskStatus = "completed"
	TaskNeedsUserDecision TaskStatus = "needs_user_decision"
	TaskBlocked           TaskStatus = "blocked"
	TaskInterrupted       TaskStatus = "interrupted"
	TaskFailed            TaskStatus = "failed"
)

type SessionState struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	Draft bool   `json:"draft,omitempty"`
	// Status 是会话可见状态：draft | idle | running | queued | restoring。
	Status SessionStatus `json:"status,omitempty"`
	// Composer 是当前视图会话的未发送输入草稿正文（草稿会话跨重启恢复用；
	// 只出现在视图当前会话，不进入目录行）。
	Composer string `json:"composer,omitempty"`
}

// ComposerDraft 是会话"未发送输入"草稿（G4：归属进 SessionUnit，随会话
// record 持久化，跨重启恢复；提交成功后清空）。Text 为未发送正文；
// 附件/排队项在后续波次扩展，JSON 结构保持可向后兼容地加字段。
type ComposerDraft struct {
	Text      string    `json:"text,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// SessionStatus 描述一个会话的可见状态（会话树/当前会话徽标数据源）。
type SessionStatus string

const (
	SessionStatusDraft            SessionStatus = "draft"
	SessionStatusIdle             SessionStatus = "idle"
	SessionStatusRunning          SessionStatus = "running"
	SessionStatusQueued           SessionStatus = "queued"
	SessionStatusAwaitingApproval SessionStatus = "awaiting_approval"
	SessionStatusArchived         SessionStatus = "archived"
	// SessionStatusRestoring 表示会话正在后台冷加载（运行中切换到未驻留
	// 会话的异步路径）：视图已切到目标空壳，内容装载完成后由事件发布基线。
	SessionStatusRestoring SessionStatus = "restoring"
)

type Message struct {
	ID      string `json:"id"`
	Role    string `json:"role"`
	Content string `json:"content,omitempty"`
	// Kind 是消息在多线谱中的显式类别（llm/tool_call/tool_output/user_input/…）。
	// 空串 = 旧数据未标注，前端按 Role/Tool 回退分类。
	Kind string `json:"kind,omitempty"`
	// ReasoningContent 是模型推理内容（thinking）。可见消息里与 Content 分离：
	// 聊天区只做一行带过，轨迹区完整查看；不做 HTML 注入，前端按纯文本渲染。
	ReasoningContent string    `json:"reasoning_content,omitempty"`
	Tool             *ToolCall `json:"tool,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}
type ToolCall struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Arguments string        `json:"arguments,omitempty"`
	Result    string        `json:"result,omitempty"`
	Error     string        `json:"error,omitempty"`
	Status    string        `json:"status"`
	Duration  time.Duration `json:"duration,omitempty"`
	// ResultRef 是完整工具输出的归档引用（仅当输出超过快照截断阈值时
	// 设置）：快照里的 Result 只含预览，前端"加载完整输出"经
	// ToolResultContent 按此 ref 分页读回。空 = 输出未截断（Result 即全文）。
	ResultRef string `json:"result_ref,omitempty"`
	// Truncated 标记快照输出已被截断（Result 为预览，全文在 ResultRef）。
	Truncated bool `json:"truncated,omitempty"`
	// TotalChars 是完整输出的原始字符数（截断时前端用于展示
	// "+N 字符"，无需预先拉全文）。
	TotalChars int `json:"total_chars,omitempty"`
}

// ToolResultPage 是按 result_ref 分页读回工具完整输出的一页（GUI
// ToolResultContent 与 read_tool_result 工具共用同一持久化通道；字段
// 与 encodeToolResultPage 的 JSON 载荷一一对应）。
type ToolResultPage struct {
	ResultRef  string `json:"result_ref"`
	Tool       string `json:"tool"`
	Digest     string `json:"digest"`
	Offset     int    `json:"offset"`
	NextOffset int    `json:"next_offset"`
	TotalBytes int    `json:"total_bytes"`
	HasMore    bool   `json:"has_more"`
	Content    string `json:"content"`
}

// PerfStats 是 GUI 性能追踪钩子的后端数据面：只上报数量/体积等无内容
// 指标，供前端渲染进程对照（DOM 节点数 ↔ 快照载荷体积 ↔ JS heap）。
type PerfStats struct {
	// Snapshot 载荷体积（字节）：会话可见区 JSON 序列化大小估算。
	SnapshotBytes int `json:"snapshot_bytes"`
	// ConversationMessages 是可见会话消息条数（含 tool 消息）。
	ConversationMessages int `json:"conversation_messages"`
	// ConversationChars 是可见会话全部消息 content/参数/结果字符总数。
	ConversationChars int `json:"conversation_chars"`
	// LargestMessageChars 是单条可见消息的最大字符数（截断后）。
	LargestMessageChars int `json:"largest_message_chars"`
	// ToolResults 是已归档 result_ref 条数。
	ToolResults int `json:"tool_results"`
	// ArchivedBytes 是已归档工具结果的字节总量（内存态 pending + 会话
	// 注册表元数据；磁盘占用以 Size 为准）。
	ArchivedBytes int `json:"archived_bytes"`
	// TruncatedOutputs 是快照中被截断的工具输出条数。
	TruncatedOutputs int `json:"truncated_outputs"`
	// HistoryWindow 是当前生效的可见会话窗口上限。
	HistoryWindow int `json:"history_window"`
	// Revision 是当前快照修订号。
	Revision uint64 `json:"revision"`
}
type ChatState struct {
	Running     bool      `json:"running"`
	RequestID   string    `json:"request_id,omitempty"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	Error       string    `json:"error,omitempty"`
	QueuedCount int       `json:"queued_count"`          // 排队中的输入数
	InputQueue  []string  `json:"input_queue,omitempty"` // 排队消息内容（TUI 显示用）
}
type RuntimeState struct {
	Model             string                     `json:"model"`
	Provider          string                     `json:"provider"`
	Account           string                     `json:"account,omitempty"`
	Plugin            string                     `json:"plugin,omitempty"`
	Effort            string                     `json:"effort"`
	FullAccess        bool                       `json:"full_access"`
	VisibleTools      []Tool                     `json:"visible_tools"`
	Skills            []SkillInfo                `json:"skills"`
	Tokens            string                     `json:"tokens"`
	Replan            ReplanMonitor              `json:"replan"`
	Plan              *PlanState                 `json:"plan,omitempty"`
	Plugins           []PluginInfo               `json:"plugins,omitempty"`            // 完整插件列表（含描述）
	Accounts          []AccountInfo              `json:"accounts,omitempty"`           // 账号池
	TodoItems         []dto.TodoItem             `json:"todo_items,omitempty"`         // todolist 清单（GUI 待办面板）
	ScheduledTasks    []dto.ScheduledTaskStatus  `json:"scheduled_tasks,omitempty"`    // 定时周期任务（GUI 定时任务面板）
	ScheduledCommands []dto.ScheduledCommandInfo `json:"scheduled_commands,omitempty"` // 白名单命令（新建弹窗下拉）
	// SubAgentTree 是 fork 子代理树的权威投影（内存态，不落盘；GUI 树视图
	// 数据源）。节点状态由 fork 子代理会话生命周期投影，随节点事件增量刷新。
	SubAgentTree []dto.SubAgentTreeNode `json:"subagent_tree,omitempty"`
	// GoalSkillActive 是 goal skill 激活投影（右侧栏「目标」面板 badge）。
	GoalSkillActive bool `json:"goal_skill_active,omitempty"`
	// ActiveSkills 是当前任务的激活 skill ID 列表（「目标」面板数据源）。
	ActiveSkills []string `json:"active_skills,omitempty"`
	// WorkTable 是工作台统一工作表格的权威投影（plan 节点 / todolist 项 /
	// fork 子代理 → 扁平 WorkItem 行，含任务打点 trace）。有界（行数上限
	// limits.work_table_rows，trace 上限 limits.plan_node_events）。
	WorkTable []WorkItem `json:"work_table,omitempty"`
	// WorkTableBatches 是工作表格的批次分片头（按 CreatedAt 升序；空批次
	// 归入「早期任务」置底）。前端按 batch_id 分组渲染。
	WorkTableBatches []WorkTableBatch `json:"work_table_batches,omitempty"`
}

// ReplanMonitor exposes bounded recovery-planning usage without exposing
// request content or provider credentials.
// ReadFileRef is the compact, persistent cache of files successfully read by
// the agent in this session. It records provenance, not file content.
type ReadFileRef struct {
	Path   string    `json:"path"`
	ReadAt time.Time `json:"read_at,omitempty"`
}

// SessionTitle is stable session metadata. It is stored under the session's
// unique persistence key and is never reconstructed from mutable chat history.
type SessionTitle struct {
	Value        string    `json:"value"`
	Source       string    `json:"source,omitempty"`
	FinalizedAt  time.Time `json:"finalized_at,omitempty"`
	UserEditedAt time.Time `json:"user_edited_at,omitempty"`
}

// ConversationRecord is the application-visible, append-only conversation
// projection. It is intentionally distinct from provider history, which is an
// execution cache and may be compacted or replaced.
type ConversationRecord struct {
	Messages  []Message `json:"messages,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// SessionPlanFrame preserves one loaded Plan revision. ActivePlanID identifies
// the frame whose Plan is currently executable; older frames remain evidence
// for recovery and workbench history.
type SessionPlanFrame struct {
	ID        string     `json:"id"`
	Plan      *PlanState `json:"plan,omitempty"`
	Arguments string     `json:"arguments,omitempty"`
	LoadedAt  time.Time  `json:"loaded_at,omitempty"`
	UpdatedAt time.Time  `json:"updated_at,omitempty"`
}

// SessionExecutionRecord groups non-conversation execution data that survives
// provider-history compaction.
type SessionExecutionRecord struct {
	Task         *TaskState    `json:"task,omitempty"`
	ReadFiles    []ReadFileRef `json:"read_files,omitempty"`
	Continuation string        `json:"continuation,omitempty"`
}

// SessionRecord is the durable backend record addressed by
// (workspace_id, session_id). It is the source of truth for stable metadata,
// Plan revisions, and visible conversation; framework history is not.
type SessionRecord struct {
	Version int          `json:"version"`
	ID      string       `json:"id"`
	Title   SessionTitle `json:"title"`
	// Status 是会话落盘可见状态（草稿行用：draft 记录在重启后仍以
	// status=draft 进入目录；idle/running/queued 由运行期叠加，不落盘）。
	Status SessionStatus `json:"status,omitempty"`
	// Composer 是会话未发送输入草稿（早分配 SID 的草稿会话跨重启恢复用；
	// 物化提交成功后清空）。
	Composer ComposerDraft `json:"composer,omitempty"`
	// ForkedFrom 是 fork 血缘（子会话侧事实源；nil = 非 fork 会话）。
	// 父目录 children 索引只是可重建的展示层，不承载血缘事实。
	ForkedFrom   *SessionForkRef    `json:"forked_from,omitempty"`
	ActivePlanID string             `json:"active_plan_id,omitempty"`
	PlanStack    []SessionPlanFrame `json:"plan_stack,omitempty"`
	// Tasks 是 task 注册表快照（worktable 条目；复用 session stack 持久化，
	// 与 PlanStack 同一 immutable 存储通道，T4）。
	Tasks        []dto.TaskRecord       `json:"tasks,omitempty"`
	Conversation ConversationRecord     `json:"conversation"`
	Execution    SessionExecutionRecord `json:"execution"`
	Projection   *TaskContextProjection `json:"projection,omitempty"`
	Checkpoints  []TaskCheckpoint       `json:"checkpoints,omitempty"`
	ToolResults  []ToolResultRef        `json:"tool_results,omitempty"`
	UpdatedAt    time.Time              `json:"updated_at,omitempty"`
}

// SessionArchive is the v1 sidecar shape retained only for migration. New
// writes use SessionRecord, whose title, Plan stack, and conversation record
// are explicit independent components.
type SessionArchive struct {
	Version       int           `json:"version"`
	Name          string        `json:"name,omitempty"`
	Conversation  []Message     `json:"conversation,omitempty"`
	Task          *TaskState    `json:"task,omitempty"`
	Plan          *PlanState    `json:"plan,omitempty"`
	PlanArguments string        `json:"plan_arguments,omitempty"`
	ReadFiles     []ReadFileRef `json:"read_files,omitempty"`
	Continuation  string        `json:"continuation,omitempty"`
	UpdatedAt     time.Time     `json:"updated_at,omitempty"`
}

type ReplanMonitor struct {
	InFlight               int       `json:"in_flight"`
	ConcurrentLimit        int       `json:"concurrent_limit"`
	WindowAttempts         int       `json:"window_attempts"`
	WindowLimit            int       `json:"window_limit"`
	WindowStartedAt        time.Time `json:"window_started_at,omitempty"`
	Accepted               uint64    `json:"accepted"`
	Succeeded              uint64    `json:"succeeded"`
	Failed                 uint64    `json:"failed"`
	Rejected               uint64    `json:"rejected"`
	DuplicateRejected      uint64    `json:"duplicate_rejected"`
	ProviderRequests       uint64    `json:"provider_requests"`
	ProviderWindowRequests int       `json:"provider_window_requests"`
	ProviderWindowLimit    int       `json:"provider_window_limit"`
}

// ── Plan 可视化 ────────────────────────────────────────────

// PlanState 描述当前 WorkPlan 的执行状态（nil = 无活跃 Plan）。
type PlanState struct {
	Name        string         `json:"name"`
	EntryNodeID string         `json:"entry_node_id"`
	Status      PlanStatus     `json:"status"`
	Nodes       []PlanNode     `json:"nodes,omitempty"`
	Edges       []dto.PlanEdge `json:"edges,omitempty"`
	Progress    float64        `json:"progress"`
	Elapsed     string         `json:"elapsed,omitempty"`
	ReplanCount int            `json:"replan_count,omitempty"`
}

type PlanStatus string

const (
	PlanPending   PlanStatus = "pending"
	PlanRunning   PlanStatus = "running"
	PlanCompleted PlanStatus = "completed"
	PlanFailed    PlanStatus = "failed"
	PlanAborted   PlanStatus = "aborted"
)

type PlanNode struct {
	ID         string              `json:"id"`
	Label      string              `json:"label"`
	Kind       string              `json:"kind"`
	Status     NodeStatus          `json:"status"`
	Depth      int                 `json:"depth,omitempty"`  // 缩进层级（0 = 根）
	Output     string              `json:"output,omitempty"` // 节点输出内容
	Elapsed    string              `json:"elapsed,omitempty"`
	Events     []PlanNodeEventInfo `json:"events,omitempty"`      // 节点事件时间线（详情页）
	ToolEvents []SubagentToolEvent `json:"tool_events,omitempty"` // 子代理工具活动（有界）
	Children   []PlanNode          `json:"children,omitempty"`    // Fork 子节点
}

// PlanNodeEventInfo 是节点事件时间线的一条记录（子代理详情页数据源）：
// queued → running（可含心跳刷新）→ 终态，含时间戳与输出快照。
type PlanNodeEventInfo struct {
	Status NodeStatus `json:"status"`
	At     time.Time  `json:"at"`
	Output string     `json:"output,omitempty"`
}

// SubagentDetail 是子代理详情弹窗的数据载荷（会话记录 + 状态/耗时/输出）。
// Conversation 经应用层适配截断（单条 ≤ evidence_chars、总 ≤ 50 条）。
// 2026-09-07 起扩展为“弹窗分类实时数据面”：除会话记录外还携带
// Goal/SessionID/Assignee/Participants（展示归属）、Stages（第一视角历史
// 阶段日志）、Trace（功能打点）、Timeline（事件时间线）、Summary（树/工作
// 台摘要）。GUI 打开详情后按这些分类做节流实时刷新，不再只依赖打开瞬间的
// 一次快照。
type SubagentDetail struct {
	Conversation []Message           `json:"conversation,omitempty"`
	ToolEvents   []SubagentToolEvent `json:"tool_events,omitempty"`
	Context      *SubagentContext    `json:"context,omitempty"`
	// Worktree 是节点 worktree 现场（失败/合并被拒时保留，Path 即人工恢复
	// 入口；成功路径已清理 → nil）。
	Worktree *SubagentWorktreeInfo `json:"worktree,omitempty"`
	Running  bool                  `json:"running"`
	Status   NodeStatus            `json:"status"`
	Elapsed  string                `json:"elapsed,omitempty"`
	Output   string                `json:"output,omitempty"`
	// Goal/SessionID/Assignee/Participants 是归属与认领展示（fork 不在
	// Plan 快照时由 SubAgentTree + 工作台回填）。
	Goal         string   `json:"goal,omitempty"`
	SessionID    string   `json:"session_id,omitempty"`
	Assignee     string   `json:"assignee,omitempty"`
	Participants []string `json:"participants,omitempty"`
	Summary      string   `json:"summary,omitempty"`
	// Stages 是第一视角（阶段）tab 的历史回放（spawn/turn/tool/result；
	// 即使详情在运行中途打开也能补全，不受 live dispatcher 启动时刻影响）。
	Stages []dto.NodeStageLog `json:"stages,omitempty"`
	// Trace 是功能打点 tab 的任务打点（工作台行 trace；派工/认领/完成
	// 即打点，不依赖会话正文）。
	Trace []WorkTracePoint `json:"trace,omitempty"`
	// Timeline 是事件时间线 tab 的归一化事件（由阶段日志 + 打点推导）。
	Timeline []PlanNodeEventInfo `json:"timeline,omitempty"`
}

// SubagentWorktreeInfo 是节点 worktree 现场的只读摘要（详情弹窗"工作区"
// 数据源）。节点失败/被拒时文件保留在 Path，分支改动仍可 git merge 恢复。
type SubagentWorktreeInfo struct {
	Path       string `json:"path,omitempty"`
	Branch     string `json:"branch,omitempty"`
	MainBranch string `json:"main_branch,omitempty"`
}

// SubagentContext 是子代理运行过程的结构化上下文快照（详情弹窗"上下文"
// 标签数据源）。只暴露可公开的证据面：目标/进度/发现/决策/约束/待办/
// 消息数/token 估算；不含 prompt 原文、工具参数或秘密。
type SubagentContext struct {
	Goal          string                    `json:"goal,omitempty"`
	Progress      string                    `json:"progress,omitempty"`
	MessageCount  int                       `json:"message_count"`
	TokenEstimate int                       `json:"token_estimate,omitempty"`
	Findings      []string                  `json:"findings,omitempty"`
	Decisions     []SubagentContextDecision `json:"decisions,omitempty"`
	Constraints   []string                  `json:"constraints,omitempty"`
	PendingWork   []string                  `json:"pending_work,omitempty"`
}

// ── 工作表格（Work Table）─────────────────────────────────

// WorkItem 是工作台工作表格的统一只读投影行：把 plan 节点、todolist 项与
// fork 子代理归一为同一张多维表格；Trace 是任务打点（有界，按时间倒序）。
type WorkItem struct {
	ID           string           `json:"id"`                     // 稳定键：plan:<id> | todo:<index> | subagent:<id>
	Phase        string           `json:"phase"`                  // 展示派生字段：plan | tasklist | task | subagent
	Task         string           `json:"task"`                   // 任务名/节点 label/goal
	Description  string           `json:"description,omitempty"`  // 描述/output 摘要
	Status       string           `json:"status"`                 // 权威状态（来源状态机）
	RetryCount   int              `json:"retry_count,omitempty"`  // 重试数字（RETRY n）
	Assignee     string           `json:"assignee,omitempty"`     // main:<mainSessionID> | subagent:<subagentSessionID>；role:sessionID 被动识别
	Dependencies []string         `json:"dependencies,omitempty"` // 前置任务（WorkItem ID 引用）
	Attachments  []string         `json:"attachments,omitempty"`  // 可选：worktree/read_file 路径
	Kind         string           `json:"kind"`                   // 权威类型：plan | todo | task | subagent
	SourceID     string           `json:"source_id,omitempty"`    // 原数据面 ID（详情溯源）
	Participants []string         `json:"participants,omitempty"` // 名单：创建者自动上名单；接管者（role:sessionID）追加并成为当前 Assignee
	BatchID      string           `json:"batch_id,omitempty"`     // 所属批次（chat 请求 requestID；空 = 早期会话）
	BatchLabel   string           `json:"batch_label,omitempty"`  // 批次展示标签（由 CreatedAt 派生；与 batches[].label 一致）
	CreatedAt    time.Time        `json:"created_at,omitempty"`   // 条目创建时间（批次排序/展示）
	StartedAt    time.Time        `json:"started_at,omitempty"`
	EndedAt      time.Time        `json:"ended_at,omitempty"`
	Elapsed      string           `json:"elapsed,omitempty"`
	Trace        []WorkTracePoint `json:"trace,omitempty"`
}

// WorkTracePoint 是任务打点（操作/状态/时间/证据；evidence 已截断）。
type WorkTracePoint struct {
	At        time.Time `json:"at,omitempty"`
	Status    string    `json:"status"`
	Operation string    `json:"operation,omitempty"` // node.lifecycle | task_check_node | tool 名 | subagent.lifecycle
	Evidence  string    `json:"evidence,omitempty"`
	Duration  string    `json:"duration,omitempty"`
}

// WorkTableBatch 是 worktable 的批次分片头：一批 = 一次 chat 请求创建的
// 全部条目。Counts 按权威类型统计（all/plan/task/todo/subagent）。
type WorkTableBatch struct {
	ID        string         `json:"id"`
	Label     string         `json:"label"`
	CreatedAt time.Time      `json:"created_at,omitempty"`
	Counts    map[string]int `json:"counts"`
}

// WorkTableEvent 是 worktable.changed 增量的 payload（只含表格与批次头，
// 不整份 runtime）。
type WorkTableEvent struct {
	Items   []WorkItem       `json:"items"`
	Batches []WorkTableBatch `json:"batches,omitempty"`
}

// TaskChangedEvent 是 task.changed 增量的 payload：单个 task 的内部变更
// （状态/打点/retry），Task 是 WorkItem 同构快照。
type TaskChangedEvent struct {
	TaskID string   `json:"task_id"`
	Task   WorkItem `json:"task"`
}

// CloneWorkItems 返回工作表格行的深拷贝（并发读者安全；供快照克隆与事件发布）。
func CloneWorkItems(items []WorkItem) []WorkItem {
	if len(items) == 0 {
		return nil
	}
	cloned := append([]WorkItem(nil), items...)
	for index := range cloned {
		cloned[index].Dependencies = append([]string(nil), items[index].Dependencies...)
		cloned[index].Attachments = append([]string(nil), items[index].Attachments...)
		cloned[index].Participants = append([]string(nil), items[index].Participants...)
		cloned[index].Trace = append([]WorkTracePoint(nil), items[index].Trace...)
	}
	return cloned
}

// SubagentContextDecision 是子代理关键决策（What/Why）。
type SubagentContextDecision struct {
	What string `json:"what"`
	Why  string `json:"why,omitempty"`
}

// SubagentEvent 是节点生命周期的前端增量载荷。Node 是权威 Snapshot
// 中该节点的完整有界投影；PlanStatus/Progress 让 reducer 无需整份重载。
type SubagentEvent struct {
	PlanID     string     `json:"plan_id,omitempty"`
	RunID      string     `json:"run_id,omitempty"`
	NodeID     string     `json:"node_id"`
	Node       PlanNode   `json:"node"`
	PlanStatus PlanStatus `json:"plan_status"`
	Progress   float64    `json:"progress"`
}

// SubagentToolEvent 是子代理内部工具调用的有界活动投影（单源在 dto；
// runtime 事件与快照投影同一形状，本包以别名保持契约兼容）。
type SubagentToolEvent = dto.SubagentToolEvent

type NodeStatus string

const (
	NodePending          NodeStatus = "pending"
	NodeQueued           NodeStatus = "queued"
	NodeRunning          NodeStatus = "running"
	NodeWorktreeCreating NodeStatus = "worktree_creating"
	NodeRebasing         NodeStatus = "rebasing"
	NodeMerging          NodeStatus = "merging"
	NodeCompleted        NodeStatus = "completed"
	NodeFailed           NodeStatus = "failed"
	NodeAborted          NodeStatus = "aborted"
	NodeSkipped          NodeStatus = "skipped"
	NodeCanceled         NodeStatus = "canceled"
	NodePanicked         NodeStatus = "panicked"
)

type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}
type SkillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Prompt      string `json:"-"`
}
type PluginInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Prompt      string `json:"-"`
}
type AccountInfo struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Disabled bool   `json:"disabled"`
}

// WorkspaceInfo is the minimal workspace summary carried by application state.
type WorkspaceInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	RootPath  string `json:"root_path"`
	GitRemote string `json:"git_remote,omitempty"`
}

type SessionInfo struct {
	ID         string        `json:"id"`
	Name       string        `json:"name,omitempty"`
	UpdatedAt  time.Time     `json:"updated_at"`
	TokenCount int           `json:"token_count"`
	Status     SessionStatus `json:"status,omitempty"`
	// ApprovalCount 是本会话当前待批审批数（波 4 approval 会话级归属：
	// awaiting_approval 状态行/侧栏计数数据源，随快照覆盖下发）。
	ApprovalCount int `json:"approval_count,omitempty"`
	// Resident 是本会话引擎 bundle 是否驻留（G6：驱逐/诊断/侧栏；随
	// 快照覆盖下发，非持久字段）。
	Resident bool `json:"resident,omitempty"`
	// Meta 是用户侧展示元数据（置顶/别名/排序位）。随目录由后端下发，客户端
	// 不再存在 localStorage（否则换窗口/换设备即分叉）。
	Meta SessionMeta `json:"meta,omitempty"`
}

// SessionMeta 描述"用户怎么看这个会话"，不参与执行与存储归属。
type SessionMeta struct {
	Pinned    bool   `json:"pinned,omitempty"`
	Alias     string `json:"alias,omitempty"`
	SortOrder int    `json:"sort_order,omitempty"`
}
type Interaction struct {
	ID string `json:"id"`
	// SessionID 是审批类交互的会话级归属（波 4：awaiting_approval/待批
	// 列表按 sid 分格；session/account/plan_retry 等视图单格交互不填）。
	SessionID string              `json:"session_id,omitempty"`
	Kind      string              `json:"kind"`
	Title     string              `json:"title"`
	Question  string              `json:"question,omitempty"`
	Risk      string              `json:"risk,omitempty"`
	ToolName  string              `json:"tool_name,omitempty"`
	Preview   string              `json:"preview,omitempty"`
	Options   []InteractionOption `json:"options"`
	OpenedAt  time.Time           `json:"opened_at"`
	Timeout   time.Duration       `json:"timeout,omitempty"`
}
type InteractionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Style       string `json:"style,omitempty"`
}
type Capabilities struct {
	SessionResume       bool   `json:"session_resume"`
	SessionResumeReason string `json:"session_resume_reason,omitempty"`
	// SessionSnapshot 声明当前快照制品是"会话粒度、传输完备"的
	// SessionSnapshot（G3）：载荷只含本会话事实，不含进程级目录/能力清单；
	// 客户端据此决定是否需要另拉 ProcessSnapshot（进程隔离退路）。
	SessionSnapshot bool `json:"session_snapshot,omitempty"`
}

// SessionRuntime 是会话快照的会话专属运行原件（target-design §2.4 字段归属：
// effort/fullAccess/plan/todo/worktable/subagent 树/tokens/replan/激活 skill；
// model/provider/account/plugin/能力清单等进程级原件不进入本结构）。
type SessionRuntime struct {
	Effort     string         `json:"effort"`
	FullAccess bool           `json:"full_access"`
	Tokens     string         `json:"tokens,omitempty"`
	Replan     ReplanMonitor  `json:"replan"`
	Plan       *PlanState     `json:"plan,omitempty"`
	TodoItems  []dto.TodoItem `json:"todo_items,omitempty"`
	// SubAgentTree 是 fork 子代理树的权威投影（本会话槽）。
	SubAgentTree    []dto.SubAgentTreeNode `json:"subagent_tree,omitempty"`
	GoalSkillActive bool                   `json:"goal_skill_active,omitempty"`
	ActiveSkills    []string               `json:"active_skills,omitempty"`
	// WorkTable 是工作台统一工作表格的权威投影（本会话槽）。
	WorkTable        []WorkItem       `json:"work_table,omitempty"`
	WorkTableBatches []WorkTableBatch `json:"work_table_batches,omitempty"`
}

// SessionSnapshot 是会话粒度、传输完备的快照制品（G3）：每会话一份，只承载
// 该会话的事实（会话身份/可见对话/聊天运行态/会话运行原件/task/工作表格/
// 子代理树/revision/窗口游标）。进程级目录与能力清单在 ProcessSnapshot。
type SessionSnapshot struct {
	ProtocolVersion int            `json:"protocol_version"`
	Revision        uint64         `json:"revision"`
	Session         SessionState   `json:"session"`
	Conversation    []Message      `json:"conversation"`
	Chat            ChatState      `json:"chat"`
	Task            *TaskState     `json:"task,omitempty"`
	Runtime         SessionRuntime `json:"runtime"`
	// Approvals 是本会话待批请求（G4 归属进 Unit 后填充；当前为预留）。
	Approvals          []Interaction `json:"approvals,omitempty"`
	Capabilities       Capabilities  `json:"capabilities"`
	HistoryOffset      int           `json:"history_offset"`
	TotalMessages      int           `json:"total_messages"`
	HasMoreHistory     bool          `json:"has_more_history"`
	ConversationWindow int           `json:"conversation_window"`
	ReadFiles          []ReadFileRef `json:"read_files,omitempty"`
	// Resident 是本会话引擎 bundle 是否驻留（C1 冷读：非驻留会话从
	// record/Transcript/事件库拼只读基线时 Resident=false；展示方据此标
	// 只读/未加载，不参与任何写入判定）。
	Resident bool `json:"resident,omitempty"`
}

// ProcessRuntime 是进程级运行原件（target-design §2.1：model/provider/
// account/plugin、能力清单与定时任务——进程单例，只读原件，深拷贝进渲染）。
type ProcessRuntime struct {
	Model             string                     `json:"model"`
	Provider          string                     `json:"provider"`
	Account           string                     `json:"account,omitempty"`
	Plugin            string                     `json:"plugin,omitempty"`
	VisibleTools      []Tool                     `json:"visible_tools"`
	Skills            []SkillInfo                `json:"skills"`
	Plugins           []PluginInfo               `json:"plugins,omitempty"`
	Accounts          []AccountInfo              `json:"accounts,omitempty"`
	ScheduledTasks    []dto.ScheduledTaskStatus  `json:"scheduled_tasks,omitempty"`
	ScheduledCommands []dto.ScheduledCommandInfo `json:"scheduled_commands,omitempty"`
}

// ProcessSnapshot 是进程级权威快照制品（G3）：会话目录/工作区/绑定/
// 能力清单与进程级运行原件；为进程隔离保留传输完备的退路。
type ProcessSnapshot struct {
	ProtocolVersion   int               `json:"protocol_version"`
	Revision          uint64            `json:"revision"`
	Sessions          []SessionInfo     `json:"sessions"`
	Workspaces        []WorkspaceInfo   `json:"workspaces,omitempty"`
	SessionWorkspaces map[string]string `json:"session_workspaces,omitempty"`
	CurrentWorkspace  *WorkspaceInfo    `json:"current_workspace,omitempty"`
	Capabilities      Capabilities      `json:"capabilities"`
	Runtime           ProcessRuntime    `json:"runtime"`
}

// CloneSnapshot returns an independent copy suitable for concurrent readers.
func CloneSnapshot(snapshot Snapshot) Snapshot {
	copySnapshot := snapshot
	copySnapshot.Sessions = append([]SessionInfo(nil), snapshot.Sessions...)
	if snapshot.SessionWorkspaces != nil {
		copySnapshot.SessionWorkspaces = make(map[string]string, len(snapshot.SessionWorkspaces))
		for sessionID, workspaceID := range snapshot.SessionWorkspaces {
			copySnapshot.SessionWorkspaces[sessionID] = workspaceID
		}
	}
	if snapshot.CurrentWorkspace != nil {
		workspace := *snapshot.CurrentWorkspace
		copySnapshot.CurrentWorkspace = &workspace
	}
	copySnapshot.Conversation = append([]Message(nil), snapshot.Conversation...)
	copySnapshot.ReadFiles = append([]ReadFileRef(nil), snapshot.ReadFiles...)
	if snapshot.Task != nil {
		task := *snapshot.Task
		task.ContextCompactions = append([]ContextCompaction(nil), snapshot.Task.ContextCompactions...)
		copySnapshot.Task = &task
	}
	if copySnapshot.Conversation == nil {
		copySnapshot.Conversation = []Message{} // 确保 JSON 序列化为 [] 而非 null
	}
	// 标量字段 (HistoryOffset, TotalMessages, HasMoreHistory) 已值拷贝
	for index := range copySnapshot.Conversation {
		if copySnapshot.Conversation[index].Tool != nil {
			tool := *copySnapshot.Conversation[index].Tool
			copySnapshot.Conversation[index].Tool = &tool
		}
	}
	copySnapshot.Runtime = CloneRuntimeState(snapshot.Runtime)
	if snapshot.Interaction != nil {
		interaction := *snapshot.Interaction
		interaction.Options = append([]InteractionOption(nil), snapshot.Interaction.Options...)
		copySnapshot.Interaction = &interaction
	}
	return copySnapshot
}

// CloneRuntimeState returns an independent copy of mutable runtime slices.
func CloneRuntimeState(runtime RuntimeState) RuntimeState {
	copyRuntime := runtime
	copyRuntime.VisibleTools = append([]Tool(nil), runtime.VisibleTools...)
	copyRuntime.Skills = append([]SkillInfo(nil), runtime.Skills...)
	copyRuntime.Plugins = append([]PluginInfo(nil), runtime.Plugins...)
	copyRuntime.Accounts = append([]AccountInfo(nil), runtime.Accounts...)
	copyRuntime.TodoItems = append([]dto.TodoItem(nil), runtime.TodoItems...)
	copyRuntime.ActiveSkills = append([]string(nil), runtime.ActiveSkills...)
	copyRuntime.ScheduledTasks = append([]dto.ScheduledTaskStatus(nil), runtime.ScheduledTasks...)
	copyRuntime.ScheduledCommands = append([]dto.ScheduledCommandInfo(nil), runtime.ScheduledCommands...)
	if runtime.Plan != nil {
		planCopy := *runtime.Plan
		planCopy.Nodes = clonePlanNodes(runtime.Plan.Nodes)
		planCopy.Edges = append([]dto.PlanEdge(nil), runtime.Plan.Edges...)
		copyRuntime.Plan = &planCopy
	}
	copyRuntime.SubAgentTree = cloneSubAgentTree(runtime.SubAgentTree)
	copyRuntime.WorkTable = CloneWorkItems(runtime.WorkTable)
	copyRuntime.WorkTableBatches = CloneWorkTableBatches(runtime.WorkTableBatches)
	return copyRuntime
}

// CloneWorkTableBatches 深拷贝批次头（Counts map 必须独立，避免并发读者
// 与写入者共享同一 map）。
func CloneWorkTableBatches(batches []WorkTableBatch) []WorkTableBatch {
	if len(batches) == 0 {
		return nil
	}
	cloned := append([]WorkTableBatch(nil), batches...)
	for index := range cloned {
		if batches[index].Counts != nil {
			counts := make(map[string]int, len(batches[index].Counts))
			for key, value := range batches[index].Counts {
				counts[key] = value
			}
			cloned[index].Counts = counts
		}
	}
	return cloned
}

// cloneSubAgentTree 深拷贝子代理树投影（快照克隆不改旧快照的契约）。
func cloneSubAgentTree(nodes []dto.SubAgentTreeNode) []dto.SubAgentTreeNode {
	if len(nodes) == 0 {
		return nil
	}
	cloned := append([]dto.SubAgentTreeNode(nil), nodes...)
	for index := range cloned {
		cloned[index].Children = cloneSubAgentTree(nodes[index].Children)
	}
	return cloned
}

func clonePlanNodes(nodes []PlanNode) []PlanNode {
	if len(nodes) == 0 {
		return nil
	}
	cloned := append([]PlanNode(nil), nodes...)
	for index := range cloned {
		cloned[index].Children = clonePlanNodes(nodes[index].Children)
		if nodes[index].Events != nil {
			cloned[index].Events = append([]PlanNodeEventInfo(nil), nodes[index].Events...)
		}
		if nodes[index].ToolEvents != nil {
			cloned[index].ToolEvents = append([]SubagentToolEvent(nil), nodes[index].ToolEvents...)
		}
	}
	return cloned
}
