// Package model defines the versioned application DTOs shared with clients.
package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// ProtocolVersion identifies the Snapshot/Event contract consumed by frontends.
const ProtocolVersion = 1

type Snapshot struct {
	ProtocolVersion int    `json:"protocol_version"`
	Revision        uint64 `json:"revision"`
	// ConversationRevision 是当前会话事件流最大 Seq，用作消息编辑的 CAS 令牌；
	// 与进程级 Revision 分离，目录刷新等无关变更不会让编辑失败。
	ConversationRevision uint64        `json:"conversation_revision"`
	Session              SessionState  `json:"session"`
	Sessions             []SessionInfo `json:"sessions"`
	Conversation         []Message     `json:"conversation"`
	Chat                 ChatState     `json:"chat"`
	Task                 *TaskState    `json:"task,omitempty"`
	Runtime              RuntimeState  `json:"runtime"`
	Interaction          *Interaction  `json:"interaction,omitempty"`
	Capabilities         Capabilities  `json:"capabilities"`
	HistoryOffset        int           `json:"history_offset"`
	TotalMessages        int           `json:"total_messages"`
	HasMoreHistory       bool          `json:"has_more_history"`
	ConversationWindow   int           `json:"conversation_window"`
	ReadFiles            []ReadFileRef `json:"read_files,omitempty"`
	// ReviewRequest 是 agent 发起的「请 GUI 在侧边资源管理器打开这些待审阅文件」
	// 批处理请求的只读投影（会话作用域的可重放状态；GUI 未开/被重连时，起来后
	// 按这份快照补放——不是一次性事件）。nil = 该会话当前没有待审阅请求。
	// 契约（上限 / 跳过原因词表 / 失效语义）见 dto.ReviewRequest。
	ReviewRequest     *dto.ReviewRequest `json:"review_request,omitempty"`
	Workspaces        []WorkspaceInfo    `json:"workspaces,omitempty"`
	CurrentWorkspace  *WorkspaceInfo     `json:"current_workspace,omitempty"`
	SessionWorkspaces map[string]string  `json:"session_workspaces,omitempty"`
}

// TaskState is the user-visible, evidence-oriented outcome of the latest
// request. It intentionally describes execution state rather than model
// internals or prompt content.
type TaskState struct {
	RequestID          string              `json:"request_id,omitempty"`
	Status             TurnStatus          `json:"status"`
	Summary            string              `json:"summary,omitempty"`
	ContextCompactions []ContextCompaction `json:"context_compactions,omitempty"`
	UpdatedAt          time.Time           `json:"updated_at,omitempty"`
}

// ContextCompaction is a user-visible record that the active provider context
// was condensed. It intentionally contains no prompt text, checkpoint body,
// tool argument, tool result, or conversation content: the frame body lives in
// the session content store and is only referenced here through FrameRef.
type ContextCompaction struct {
	Version uint64 `json:"version"`
	Reason  string `json:"reason"`
	// Origin 是这次压缩的来源（auto/explicit/explicit_after_turn）。记录门槛
	// 按它区分：自动路径（软/硬阈值）只在任务执行 Running 时写；显式路径
	// （/compact、compact_context）在回合已收尾时也写——「用户/模型明确要求
	// 现在压缩」这个事实本身就该被审计，而回合之间压缩恰恰是最自然的用法。
	// 空串 = 旧记录（当时没有这个字段，按 auto 口径读）。
	Origin string `json:"origin,omitempty"`
	// MessagesBefore 是压缩发生时**装配前的引擎历史条数**（engineHistory 长度，
	// 含 system 行）：冷加载/路由会话在该时刻合法为 0，而且它数的是引擎消息，
	// 不是被压的 transcript 消息条数。要展示/判断"压了多少、从哪到哪"请用下面的
	// 区间字段（与 /compact 提示同一口径）；这个数字只作诊断元数据。
	MessagesBefore  int       `json:"messages_before"`
	EstimatedTokens int       `json:"estimated_tokens"`
	CompactedAt     time.Time `json:"compacted_at"`
	// 压缩区间（记录，不推算）：被压出保留窗口、送进 compact_context 的
	// transcript 前缀。MessageFrom/MessageTo = UI 消息号（model.Message.ID），
	// EventFrom/EventTo = transcript 事件序号（model.TranscriptEvent.Seq）。
	// 边界取自窗口决策本身（保留窗口的首个事件），下游不再重算——token
	// 窗口预算（min(配置保留窗口, 占比 × 全量上下文)）无法在事后还原。
	MessageFrom string `json:"message_from,omitempty"`
	MessageTo   string `json:"message_to,omitempty"`
	EventFrom   uint64 `json:"event_from,omitempty"`
	EventTo     uint64 `json:"event_to,omitempty"`
	// SegmentID 是本次压缩在**会话压缩栈**（store.CompactFrame）里那一帧的
	// 标识，是快照侧记录与栈帧之间的互链键，同时是 read_compressed_turn 的必选
	// 入参。没有它，模型对这段被折出的区间就拿不到细筛入口——原文明明还在盘上，
	// 却只能靠 search_history 碰运气。
	// 空串 = 本次没有推帧（索引面未启用、栈不可用或推帧失败）；压缩本身照样成立，
	// 只是少了这一跳。帧正文的 readback 段会如实说明缺的是哪一跳、为什么。
	SegmentID string `json:"segment_id,omitempty"`
	// FrameRef 是可回读的帧正文引用：压缩发生那一刻的「有界 checkpoint 帧」
	// 正文（JSON 元数据 + Markdown 读后感）已写进会话内容存储，前端按 ref 分页
	// 读取（application.ToolResultContent / Bridge.ToolResultContent），
	// 因此快照只带引用、不带正文。空串 = 本次没有可回读正文（例如摘要为空）。
	FrameRef    string `json:"frame_ref,omitempty"`
	FrameBytes  int    `json:"frame_bytes,omitempty"`
	FrameTokens int    `json:"frame_tokens,omitempty"`
	// Failed 标记这条记录描述的是**一次压缩失败**：判据命中了，但这次压不下去
	// （拿不到模型读后感，或压缩换不来余量）。失败痕进同一个列表（状态页一行一条、
	// 跨轮次存活），但区间与帧字段恒空——这次什么都没动：上下文原样 append、
	// compact stack top 不动、上下文版本不推进（用户口径 2026-10-02）。
	//
	// 失败痕的四个恒等式（下游据此区分两种条目）：
	//   Failed=true  → MessageFrom/To、EventFrom/To、FrameRef 全为空；
	//   Failed=false → 这是一次真的压缩（区间来自窗口决策的记录值）。
	// 正因为它不带区间（EventTo=0），RetainedFromForCompactions 与前端分界
	// （compactionFrontier）都不会把它读成"已被压出窗口的上下文"。
	Failed bool `json:"failed,omitempty"`
	// Note 说明这次为什么压不成（失败痕专用；成功记录为空）。形状是
	// 「原因字面量 [数字事实…] [error=报错原文]」：只写原因与数字事实，绝不写正文
	// ——与会话记录的口径一致。末尾的 error= 段是**读数闸这次留下的报错证据**
	// （重放/回读调用的真实错误，或落回本地确定性压缩时的降级原因），它是记录里
	// 唯一无法由其它字段反推的那条事实（用户口径 2026-10-04）：前端据此在失败条目
	// 下方单独渲染一行报错，不必引号转义——报错原文是自由文本，只有放在末尾才能
	// 按"最后一个 ` error=` 之后取到结尾"还原。
	Note string `json:"note,omitempty"`
}

// 压缩来源（ContextCompaction.Origin）。
const (
	// CompactionOriginAuto 是自动路径（软阈值/硬阈值/自主压缩）产生的压缩。
	CompactionOriginAuto = "auto"
	// CompactionOriginExplicit 是回合执行中由 /compact 命令或 compact_context
	// 工具显式要求的压缩（任务状态仍是 Running）。
	CompactionOriginExplicit = "explicit"
	// CompactionOriginExplicitAfterTurn 是回合已收尾（任务状态不再是 Running）
	// 后由 /compact 命令或 compact_context 工具显式要求的压缩：压缩照做，
	// 记录同样写——否则"回合之间压缩"这一最自然的用法在前端完全不可见。
	CompactionOriginExplicitAfterTurn = "explicit_after_turn"
)

// ExplicitCompactionOrigin 报告该来源是否为显式要求（用户 /compact 或模型
// compact_context）。空串按旧记录口径视为非显式（与 auto 同处理）。
func ExplicitCompactionOrigin(origin string) bool {
	return origin == CompactionOriginExplicit || origin == CompactionOriginExplicitAfterTurn
}

// CompactionRangeLabel 把压缩区间（消息号 + transcript 事件序号）渲染成一句
// 人可读的范围，例如 `消息 message-1..message-103 / 事件 1..6`。
//
// 两段都缺时返回空串：调用方据此跳过这一段，不得改用别的数字（例如装配前的
// 引擎历史条数）顶替——那会印出与事实相反的句子（2026-09-23 实测：
// message-1..message-103 / 事件 1..6 配 messages_before=0）。
func CompactionRangeLabel(messageFrom, messageTo string, eventFrom, eventTo uint64) string {
	parts := make([]string, 0, 2)
	if label := messageRangeLabel(messageFrom, messageTo); label != "" {
		parts = append(parts, "消息 "+label)
	}
	if eventFrom > 0 || eventTo > 0 {
		parts = append(parts, fmt.Sprintf("事件 %d..%d", eventFrom, eventTo))
	}
	return strings.Join(parts, " / ")
}

// messageRangeLabel 渲染消息号区间：单号不写 `..`，只有一端就只写一端。
func messageRangeLabel(from, to string) string {
	switch {
	case from == "":
		return to
	case to == "" || to == from:
		return from
	default:
		return from + ".." + to
	}
}

// TurnStatus 是一次回合（用户这一次请求）的执行状态与结论。
//
// 词表只有一份，住在契约 `contract/dto`（`dto.TurnStatus`）：本包以别名
// 保持 model 侧的读法，**不再定义第二份词表**。历史上本包有一个 `type TaskStatus string`
// 与 `dto.TaskStatus`（工作表条目那一格）**同名却是两台不同的机器**，两套词表只在
// "进行中"上分叉（progressing vs running），中间靠一处手写映射接着——这一格合并的就是它。
type TurnStatus = dto.TurnStatus

// Turn* 是这一格的取值（转调契约常量；写状态的地方只许引这些名字）。
const (
	TurnUnknown           = dto.TurnUnknown
	TurnIdle              = dto.TurnIdle
	TurnProgressing       = dto.TurnProgressing
	TurnCompleted         = dto.TurnCompleted
	TurnNeedsUserDecision = dto.TurnNeedsUserDecision
	TurnBlocked           = dto.TurnBlocked
	TurnInterrupted       = dto.TurnInterrupted
	TurnFailed            = dto.TurnFailed
)

type SessionState struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	Draft bool   `json:"draft,omitempty"`
	// Status 是会话可见状态：draft | idle | running | queued | restoring。
	Status SessionStatus `json:"status,omitempty"`
	// Composer 已撤回：未发送输入不再作为"草稿会话"持久化/恢复（draft 语义改由
	// 「未完成会话」承担）。字段保留仅为落盘兼容，当前恒为空。
	Composer string `json:"composer,omitempty"`
	// ForkedFrom 是当前会话的权威血缘（子会话侧事实源）；nil = 非 fork 会话。
	ForkedFrom *SessionForkRef `json:"forked_from,omitempty"`
}

// ComposerDraft 是已撤回的「会话未发送输入草稿」载荷（字段保留仅为落盘兼容；
// 见 docs/devlog 撤回说明）。
type ComposerDraft struct {
	Text      string    `json:"text,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// SessionStatus 描述一个会话的可见状态（会话树/当前会话徽标数据源）。
//
// 词表只有一份，住在契约 `contract/dto`（`dto.SessionStatus`）：本包以别名
// 保持 model 侧的读法，**不再定义第二份词表**。落盘那一侧（store 的
// `Status` 字符串）在契约之下，转换点见 `wiring/adapters` 的 `sessionStatusOfRecord`。
type SessionStatus = dto.SessionStatus

const (
	SessionStatusUnknown          = dto.SessionStatusUnknown
	SessionStatusDraft            = dto.SessionStatusDraft
	SessionStatusIdle             = dto.SessionStatusIdle
	SessionStatusRunning          = dto.SessionStatusRunning
	SessionStatusQueued           = dto.SessionStatusQueued
	SessionStatusAwaitingApproval = dto.SessionStatusAwaitingApproval
	SessionStatusArchived         = dto.SessionStatusArchived
	// SessionStatusRestoring 表示会话正在后台冷加载（运行中切换到未驻留
	// 会话的异步路径）：视图已切到目标空壳，内容装载完成后由事件发布基线。
	SessionStatusRestoring = dto.SessionStatusRestoring
)

type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
	Result    string `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
	// Status 是"工具调用视图词"那一格：与**工具事件状态**同格
	// （`dto.ToolEvent*`：running | success | error）。它同时是快照 / 事件 wire 与存档
	// 的形状，所以字段本身是枚举、读回按词折一次——转换点 `ToolCallStatusOfRecord`。
	Status   dto.ToolEventStatus `json:"status"`
	Duration time.Duration       `json:"duration,omitempty"`
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
	Model      string `json:"model"`
	Provider   string `json:"provider"`
	Account    string `json:"account,omitempty"`
	Plugin     string `json:"plugin,omitempty"`
	Effort     string `json:"effort"`
	FullAccess bool   `json:"full_access"`
	// ReasoningEffort 是**当前生效的 wire 思考强度**（provider 词表：
	// minimal/low/medium/high/max），也是 composer 底部「推理强度」拉条的权威读数。
	// 它由两条路写：effort 档位切换（档位=同时设置两条拉条的预设）与用户直接拖拉条
	// （SetReasoningEffort）。空串 = 尚未确定，前端按档位派生值显示。
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// LoopLimit 是**当前生效的单回合循环上限**（composer 底部「循环次数」拉条）。
	// 与 ReasoningEffort 同一口径：档位切换会把它覆盖成该档默认（15/48/384/768），
	// 拖拉条只改它自己。0 = 尚未确定（回退当前档位默认值）。
	LoopLimit int `json:"loop_limit,omitempty"`
	// PermissionTier 是本会话生效的权限档位（manual/edit/auto/full；主 agent 在
	// 本会话的自动度）。FullAccess 保留为派生（tier==full），保前端与诊断兼容。
	PermissionTier string `json:"permission_tier,omitempty"`
	// PermissionTiers 是档位目录（进程级只读原件，前端据此渲染可选列表；
	// 后端下发避免前后端各写一套档位名）。
	PermissionTiers []dto.PermissionTierInfo `json:"permission_tiers,omitempty"`
	VisibleTools    []Tool                   `json:"visible_tools"`
	Skills          []SkillInfo              `json:"skills"`
	Tokens          string                   `json:"tokens"`
	// ContextCache 是 L1 前缀复用读面。只报告本地复用命中量，不冒充
	// provider usage / cached_tokens。
	ContextCache      *dto.ContextCacheView      `json:"context_cache,omitempty"`
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
	// GoalGovernance 是本会话 goal 治理只读视图（无 goal 时为 nil；前端
	// 据此渲染「目标 + 治理」面板）。
	GoalGovernance *dto.GoalGovernanceView `json:"goal_governance,omitempty"`
	// ActiveSkills 是当前任务的激活 skill ID 列表（「目标」面板数据源）。
	ActiveSkills []string `json:"active_skills,omitempty"`
	// WorkTable 是工作台统一工作表格的权威投影（plan 节点 / todolist 项 /
	// fork 子代理 → 扁平 WorkItem 行，含任务打点 trace）。有界（行数上限
	// limits.work_table_rows，trace 上限 limits.plan_node_events）。
	WorkTable []WorkItem `json:"work_table,omitempty"`
	// WorkTableBatches 是工作表格的批次分片头（按 CreatedAt 升序；空批次
	// 归入「早期任务」置底）。前端按 batch_id 分组渲染。
	WorkTableBatches []WorkTableBatch `json:"work_table_batches,omitempty"`
	// TeamworkBoard 是本会话团队看板的只读投影（计划 + 作业行 + 审计流水）。
	// 未装配 teamwork、或该会话尚无计划时为 nil——GUI 与 TUI 据此**整块退场**
	// （不留空壳，口径同目标看板）。契约见
	// docs/arch/team-board-gui-tui-contract.md。
	TeamworkBoard *dto.TeamworkBoardView `json:"teamwork_board,omitempty"`
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
	Messages []Message `json:"messages,omitempty"`
	// TotalMessages 是持久会话的完整可见序号规模；Messages 可以是冷加载只
	// 恢复的后缀窗口（compact 指针之后）。0 = 旧记录，调用方回退 len(Messages)。
	TotalMessages int       `json:"total_messages,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
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

	// —— 自动续跑（docs/arch/goal-auto-continue.md）——
	//
	// 四个字段都是**向后兼容的追加**：旧档缺字段读成零值 = "没停过、没暂停过、
	// 没续过"，语义正确（旧会话不会凭空续跑），因此不需要升 SessionRecord.Version。
	// 读侧（internal/host/session_workspace_ports.go 的 migrateSessionArchive）因此
	// 无需改形状：旧档映射过来就是零值，这正是要的语义。

	// StoppedByUser 是"用户显式按过停止"的落盘位：它是跨重启有效的**人工意图
	// 优先**判据（C3）。清除点唯一：开新回合（用户自己发了下一条）。
	//
	// 存在的理由：用户停止与 provider 失败落到的回合终态都是 TurnInterrupted
	// （application/conversation/turn_error.go），只能靠 TaskState.Summary 文案
	// 区分——字符串比对不可靠、也不跨重启。
	StoppedByUser bool      `json:"stopped_by_user,omitempty"`
	StoppedAt     time.Time `json:"stopped_at,omitempty"`
	// Paused 是层二子状态在存档里的**低半位**（⏸）。暂停 ≠ 中断：置位不打断正在跑的
	// 这一轮，只让轮边界不再自动续（pull 通道）。单独一个 bool 回答不了"这是哪张 goal
	// 的暂停"，所以**必须与 PausedGoalID 一起读**（dto.DeriveGoalRunState，I2）。
	Paused   bool      `json:"paused,omitempty"`
	PausedAt time.Time `json:"paused_at,omitempty"`
	// PausedGoalID 是暂停绑定在哪张 goal 上：只有它等于当前 Active goal 时层二才算
	// paused（换 goal → 回到 continue，不残留；旧档缺字段 = 不继承）。
	PausedGoalID string `json:"paused_goal_id,omitempty"`
	// Continuations 是续跑的 append-only 记录（跨重启的幂等账 + 计数权威）。
	// 判据用它回答两件事：同一中断 requestID 是否已经续过（幂等）、
	// 同一 goal 连续续了几次（上限 + 退避）。
	Continuations []ContinuationAttempt `json:"continuations,omitempty"`
	// ContextDraft 是上下文 draft actor 的最后一个持久现场：未提交字节与已 ack
	// 水位。空 = 没有在途 draft。它在回合原子落盘时随 record 一起提交。
	ContextDraft *ContextDraftState `json:"context_draft,omitempty"`
	// MemoryUpdates / ToolsUpdates 是 append-only 上下文纪元表。渲染与冷恢复
	// 只 fold 已记录 Bytes，不按本轮 query 重选。
	MemoryUpdates []MemoryUpdate `json:"memory_updates,omitempty"`
	ToolsUpdates  []ToolsUpdate  `json:"tools_updates,omitempty"`
	// ToolCatalog 是会话内冻结的 catalog；ToolsEpoch 只在 catalog 本身变化时 bump。
	ToolCatalog []ToolCatalogEntry `json:"tool_catalog,omitempty"`
	ToolsEpoch  uint64             `json:"tools_epoch,omitempty"`
}

// MemoryUpdate is one append-only memory epoch row.
type MemoryUpdate struct {
	Seq       uint64    `json:"seq"`
	Epoch     uint64    `json:"epoch"`
	Kind      string    `json:"kind"`
	SegmentID string    `json:"segment_id,omitempty"`
	Digest    string    `json:"digest"`
	Bytes     string    `json:"bytes,omitempty"`
	From      int       `json:"from,omitempty"`
	To        int       `json:"to,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

// ToolsUpdate is one append-only tool-set sentinel epoch row.
type ToolsUpdate struct {
	Seq           uint64    `json:"seq"`
	Epoch         uint64    `json:"epoch"`
	Kind          string    `json:"kind"`
	ToolNames     []string  `json:"tool_names,omitempty"`
	Digest        string    `json:"digest"`
	SentinelBytes string    `json:"sentinel_bytes,omitempty"`
	Reason        string    `json:"reason,omitempty"`
	CreatedAt     time.Time `json:"created_at,omitempty"`
}

// ToolCatalogEntry is the stable, session-frozen provider tool schema identity.
type ToolCatalogEntry struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ContextDraftItem 是一段已经获得会话内 Seq 的已发出字节。
type ContextDraftItem struct {
	Seq   uint64 `json:"seq"`
	Bytes string `json:"bytes"`
	Final bool   `json:"final,omitempty"`
}

// ContextDraftState 是 draft actor 的持久水位与待重放条目。
type ContextDraftState struct {
	Items       []ContextDraftItem `json:"items,omitempty"`
	Watermark   uint64             `json:"watermark,omitempty"`
	Uncommitted bool               `json:"uncommitted,omitempty"`
}

// ContinuationAttempt 是一次自动续跑（或一次"被上限截断"）的持久记录。
//
// 幂等键 = (GoalID, RequestID)：**同一个中断 requestID 永远只续一次**（含重启
// 之后；重启只可能读到更多记录，读不到更少）。Reason 只作诊断（interrupted /
// provider / restarted），不参与判据；Truncated=true 表示这次不是"发了一次
// 续跑"，而是"到这里被上限截断"——它是可核对的止步痕，不是重试。
//
// 一条记录有三种含义，读面按 (Attempt, Truncated, Manual) 三元区分——派生口径
// 只有这一处，不要在别处再造一份计数：
//   - **真续跑**：Attempt ≥ 1 且 Truncated=false。它是幂等判据的权威事实：
//     同一 (GoalID, RequestID) 不再续第二次（Manual=true 表示这次是 ⟳ 触发的）。
//   - **止步痕**：Truncated=true。没发出任何续跑，只挡住"同一中断点反复打
//     截断点"这种刷屏；它**不**阻止人工恢复后再试一次——否则 ⟳ 在触顶之后就废了。
//   - **清零标记**：Manual=true 且 Attempt=0。Resume 写的，"连续次数从这里
//     重新数"（清零不另设字段，痕迹就是这条记录——重置必须有痕）。
type ContinuationAttempt struct {
	At        time.Time `json:"at,omitempty"`
	GoalID    string    `json:"goal_id,omitempty"`
	RequestID string    `json:"request_id,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	Attempt   int       `json:"attempt,omitempty"`
	Truncated bool      `json:"truncated,omitempty"`
	Manual    bool      `json:"manual,omitempty"`
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
	// Composer 已撤回：未发送输入不再随 record 落盘（字段保留仅为落盘兼容，
	// 当前恒为空）。
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

// PlanStatus 是**一张 Plan 的生命周期状态**（计划面板/子代理事件的数据源）。
//
// 词表只有一份，住在契约（`dto.PlanStatus`；与"一次 plan_run 的结果"`dto.PlanRunStatus`
// 是**两格**，分工写在那两格的文件头）。取值面：pending | running | completed | failed | aborted。
type PlanStatus = dto.PlanStatus

const (
	PlanUnknown   = dto.PlanStatusUnknown
	PlanPending   = dto.PlanPending
	PlanRunning   = dto.PlanRunning
	PlanCompleted = dto.PlanCompleted
	PlanFailed    = dto.PlanFailed
	PlanAborted   = dto.PlanAborted
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
//
// Status 是**事件的原始词**（字符串）：这条时间线是"当时到底发生了什么"的流水，
// 里面既有节点状态词，也有我们自己的 worktree 收尾阶段词（worktree_unmerged /
// merge_blocked）——把阶段词硬折进节点状态枚举，就会让流水说谎（阶段事件曾经把
// 跑完的节点显成"待开始"）。节点**当前**状态那一格是 `PlanNode.Status`（枚举）。
type PlanNodeEventInfo struct {
	Status string    `json:"status"`
	At     time.Time `json:"at"`
	Output string    `json:"output,omitempty"`
}

// SubagentDetail 是子代理详情弹窗的数据载荷（会话记录 + 状态/耗时 +
// 上下文快照 + 功能打点 + worktree 现场）。Conversation 经应用层适配截断
// （单条 ≤ evidence_chars、总 ≤ 50 条）。2026-09-14 起弹窗只剩三块数据面
// （会话记录 / 上下文 / 功能打点），因此删除了只服务已删标签的字段：
// Output（最终输出标签）、Stages（第一视角标签）、Timeline（事件时间线
// 标签）。Worktree 保留：它是 P2a 恢复入口契约，仍被
// application/assembly/subagent_detail_test.go 断言。
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
	// Goal/SessionID/Assignee/Participants 是归属与认领展示（fork 不在
	// Plan 快照时由 SubAgentTree + 工作台回填）。
	Goal         string   `json:"goal,omitempty"`
	SessionID    string   `json:"session_id,omitempty"`
	Assignee     string   `json:"assignee,omitempty"`
	Participants []string `json:"participants,omitempty"`
	Summary      string   `json:"summary,omitempty"`
	// Error 是节点失败原文（SubAgentTree.Error / 运行时错误；含账号/供应商
	// 错误码与"未绑定工作区"等可执行原因，GUI 原样展示）。
	Error string `json:"error,omitempty"`
	// Trace 是功能打点 tab 的任务打点（工作台行 trace；派工/认领/完成
	// 即打点，不依赖会话正文）。工具活动经 ToolEvents 合并进同一张打点表。
	Trace []WorkTracePoint `json:"trace,omitempty"`
	// Async 是**后台作业行**（工作表格里 nodeID = `async:<句柄>` 的行）的作业读数。
	//
	// 为什么单开一格：这类节点在执行域里不是一个会话（没有 History、没有上下文
	// 快照），它的"记录"就是输出文件。把它硬塞进 Conversation 会让"会话记录"撒谎
	// （看着像一条条对话消息）；不给它任何读数，详情弹窗就只能永远停在占位
	// （2026-10-31 用户实测的"后台测试看不到实时记录，只有空文"）。
	//
	// 只上 GUI：命令行原文、日志绝对路径与输出正文都不进模型上下文（进上下文的那份
	// 仍是请求尾部打点块里的有界摘要）。
	Async *SubagentAsyncRun `json:"async,omitempty"`
}

// SubagentAsyncRun 是一次后台作业（bash_bg / read_batch / subagent）的详情读数。
//
// 生态位：它是 `dto.AsyncRunRecord`（执行域只读投影）在详情弹窗上的那一面，外加
// **有界输出尾部**（Output —— 详情弹窗每 1.5s 拉一次，读到的是日志末窗，不是全文）。
// 全文仍只走 `bg_manage(op=fetch)` 或 LogPath。
type SubagentAsyncRun struct {
	Handle string `json:"handle"`
	// Kind = process | inline | subagent（决定读取口与 kill 语义）。
	Kind string `json:"kind,omitempty"`
	// State = running | done | failed | killed（执行域的状态词原样搬运）。
	State string `json:"state,omitempty"`
	// Command 是命令行原文（单行截断后的形态）；Description 是模型派发时写的一句话。
	Command     string `json:"command,omitempty"`
	Description string `json:"description,omitempty"`
	// LogPath 是输出文件绝对路径（附件/完整日志入口，只上 GUI）。
	LogPath string `json:"log_path,omitempty"`
	// LogBytes = 已落盘字节数（真实进展信号）；Lines = 输出行数（终态才有）。
	LogBytes int64 `json:"log_bytes"`
	Lines    int   `json:"lines,omitempty"`
	// ExitCode 在 running 时是 -1（不是"退出码 0"）。
	ExitCode int `json:"exit_code"`
	// Summary 是终态有界摘要（退出码 + 行数 + 字节数 + 末行）。
	Summary string `json:"summary,omitempty"`
	// Tail 是末行采样（探针读数："它还在动"的那一眼）。
	Tail string `json:"tail,omitempty"`
	// Output 是日志末尾的有界窗口（多行正文）；OutputWindowed = 只给了末尾，
	// 前面还有内容（完整日志见 LogPath）。
	Output         string `json:"output,omitempty"`
	OutputWindowed bool   `json:"output_windowed,omitempty"`
	// Found = 句柄仍在登记表里。false 表示作业已结束被驱逐、已销项，或进程重启
	// 后登记表清空——此时读数全空，界面要如实说"不在册"，不得编造。
	Found bool `json:"found"`
	// Truncated = 输出文件已触执行域的字节上限；Degraded = 进程树挂不上，
	// 终止只能打到直接子进程。
	Truncated bool      `json:"truncated,omitempty"`
	Degraded  bool      `json:"degraded,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
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
	ID           string           `json:"id"`                     // 稳定键：plan:<id> | subagent:<id> | todo:<n> | task:<n>（自动号进程内唯一）
	SessionID    string           `json:"session_id,omitempty"`   // 归属会话（全局台账里标注条目产自哪个会话；GUI 会话筛选轴按此判定）
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

// WorkTableEvent 是 worktable.changed 增量的 payload（表格 + 批次头 +
// 可选子代理树增量，不整份 runtime）。
//
// SubAgentTree 是"树内容变化时才随包下发"的增量（nil = 本次不带、前端保留
// 既有树；空数组 = 树已清空）。详情入口在前端要先把行解析成节点（Plan DSL
// 或子代理树投影），而树只由整份快照与 runtime.changed 携带；同一批次还有
// 子代理在跑时，表格行已经通过本事件到达、树却还没到，详情点开会静默失败
// （2026-09-13 回归），因此树跟随表格增量一起送达。
type WorkTableEvent struct {
	Items        []WorkItem             `json:"items"`
	Batches      []WorkTableBatch       `json:"batches,omitempty"`
	SubAgentTree []dto.SubAgentTreeNode `json:"subagent_tree"`
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

// ToolRecordBoard / ToolRecordStat / ToolRecordReason 是**工具记录**看板的只读投影
// （单源在 dto）：TUI 与 GUI 都经 `Application.ToolRecordBoard()` 读同一份形状。
type ToolRecordBoard = dto.ToolRecordBoard

type ToolRecordStat = dto.ToolRecordStat

type ToolRecordReason = dto.ToolRecordReason

type ToolRecordSnapshot = dto.ToolRecordSnapshot

// NodeStatus 是**一个计划节点**的状态（plan 面板/子代理详情的数据源）。
//
// 词表只有一份，住在契约（`dto.NodeStatus`）：框架 workplan 的词与我们的 worktree
// 阶段词在这一格里各占一格词，折法见 `dto.NodeStatusFromFramework`（带 ok：认不得的
// 词**不是节点状态**，调用方不许拿它覆盖节点状态）。
type NodeStatus = dto.NodeStatus

const (
	NodeUnknown          = dto.NodeUnknown
	NodePending          = dto.NodePending
	NodeQueued           = dto.NodeQueued
	NodeRunning          = dto.NodeRunning
	NodeWorktreeCreating = dto.NodeWorktreeCreating
	NodeRebasing         = dto.NodeRebasing
	NodeMerging          = dto.NodeMerging
	NodeCompleted        = dto.NodeCompleted
	NodeFailed           = dto.NodeFailed
	NodeAborted          = dto.NodeAborted
	NodeSkipped          = dto.NodeSkipped
	NodeCanceled         = dto.NodeCanceled
	NodePanicked         = dto.NodePanicked
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
	// Skills 是该插件**随包携带**的 Skill 目录（不含 Prompt 正文，见 SkillInfo.Prompt
	// 的 `json:"-"`）。插件页面按插件逐个列出 Skill 时读它——那页只做「看/管理」，
	// 不做插件切换，所以每个插件自己的目录必须随插件一起下发，不能只靠
	// runtime.skills（那是**当前激活插件**的合并视图，切走就看不到了）。
	Skills []SkillInfo `json:"skills,omitempty"`
	// 「这个插件从哪来」（2026-10-05）：一块在前端**必须看得见**的读数——用户当面问过
	// 「这些前端显示出来的 plugin 我没有在我的 plugin 下面见过」，而此前模型里只有
	// Name/Description/Prompt，面板里一个字都没说。
	//
	// SourceKind/SourceURL 是**来源面**（builtin / vendored / local …；kind=local 时 URL
	// 是 `local:<根>`）：判定与文案同源，都来自 plugin.CuratedCatalog.ReadSource（即
	// plugins/README.md 声明的那条机读面），本处只是把那一行拆成两个字段。
	// **未登记就留空**——"不知道"不编成 builtin（缺失来源不得被读成"随发行包"）。
	SourceKind string `json:"source_kind,omitempty"`
	SourceURL  string `json:"source_url,omitempty"`
	// SourceRoot 是这个插件**实际**从哪个根加载进来的（多根 first-wins 的结果：同名
	// 插件只算链上先出现的那个根）。它不是来源类型，而是载入位置——"包里的那份"与
	// "你本机那份"同名时，靠它区分。
	SourceRoot string `json:"source_root,omitempty"`
}
type AccountInfo struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Disabled bool   `json:"disabled"`
	// Role 是账号在 accounts.yaml 中的显式角色。名称前缀只用于兼容旧快照，
	// 界面分组和权限装配不得再把展示名当作角色事实。
	Role string `json:"role,omitempty"`
	// APIKeySet 只报告“该账号有可用的凭据”，绝不携带密钥明文。
	APIKeySet bool `json:"api_key_set"`
	// Fallback 标记启动期因账号档不可用而启用的内置兜底账号。界面据此提示
	// “当前使用的不是用户配置”，避免把 fallback 误认成正确凭据。
	Fallback bool `json:"fallback,omitempty"`
	// ContextWindow 是该账号**生效**的上下文窗口（token）：账号级覆盖（界面「设置 →
	// 账号」设过的）> seelex.yaml 的 limits.context_window > 账号档自报值。界面按它
	// 显示当前挡位；改值走 Bridge 的 SetAccountWindow，不在这里回写。
	ContextWindow int `json:"context_window,omitempty"`
}

// WorkspaceInfo is the minimal workspace summary carried by application state.
type WorkspaceInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	RootPath  string `json:"root_path"`
	GitRemote string `json:"git_remote,omitempty"`
	// SSH / Git 是项目级连接配置的**非秘密摘要**（主机/端口/用户、git 账号名）。
	// 口令永远不在这里：只以 SSHPasswordSet / GitPasswordSet 布尔出现。
	SSH            *WorkspaceSSHInfo `json:"ssh,omitempty"`
	Git            *WorkspaceGitInfo `json:"git,omitempty"`
	SSHPasswordSet bool              `json:"ssh_password_set,omitempty"`
	GitPasswordSet bool              `json:"git_password_set,omitempty"`
}

// WorkspaceSSHInfo 是项目级 SSH 端点的读面（无口令）。
type WorkspaceSSHInfo struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	User string `json:"user"`
	// RemoteRoot 是**远端工作目录**（= 远端项目根，对端 POSIX 绝对路径）。
	//
	// 它让路径门在远端面上有一个真正的根可比对（registry 的 SSHEndpoint.RemoteRoot
	// 由它折出）：根内路径直接受 ssh 通道位约束，走出它才检查 SSH 路径白名单。
	// 空 = 未知——此时路径门只能把"本地根也容不下的绝对路径"全算成越界候选
	// （更保守，不是更松）。
	RemoteRoot string `json:"remote_root,omitempty"`
}

// WorkspaceGitInfo 是项目级 git 访问配置的读面（无口令）。
type WorkspaceGitInfo struct {
	Username string `json:"username"`
	// Kind 是访问方式："https" | "ssh"；空 = 未声明。
	Kind string `json:"kind,omitempty"`
}

// WorkspaceConnection 是连接配置的**权威读回**值（读写口共用这一个形状）。
type WorkspaceConnection struct {
	SSH            *WorkspaceSSHInfo `json:"ssh,omitempty"`
	Git            *WorkspaceGitInfo `json:"git,omitempty"`
	SSHPasswordSet bool              `json:"ssh_password_set"`
	GitPasswordSet bool              `json:"git_password_set"`
}

// WorkspaceEnvironmentInfo 是项目侧**一个工作环境**的读面（无口令，只给布尔）。
//
// Host/User 都为空 = 本地环境（在项目根执行）；有端点 = SSH 环境。
// 会话不持有连接字段，只持有名字（见 SessionSSHInfo）。
type WorkspaceEnvironmentInfo struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
	Name        string `json:"name"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	User        string `json:"user"`
	RemoteRoot  string `json:"remote_root,omitempty"`
	Note        string `json:"note,omitempty"`
	PasswordSet bool   `json:"password_set"`
	// Default 报告它是不是项目默认环境（会话没指定环境时落到这里）。
	Default bool `json:"default"`
}

// WorkspaceEnvironmentChange 是环境的写面（口令三态：nil = 不改 / "" = 清除 / 写入）。
type WorkspaceEnvironmentChange struct {
	Host       string
	Port       int
	User       string
	RemoteRoot string
	Note       string
	Password   *string
	// MakeDefault 写完把这个环境设为项目默认（Delete=true 时忽略）。
	MakeDefault bool
	// Delete 删除这个环境（连同口令）；与其它字段互斥。
	Delete bool
}

// SessionSSHInfo 是**会话级 SSH 状态**的读面：固定环境 + 旧版放权集合兼容字段。
//
// 不变式（在应用层强制，读侧再兜一次）：
//   - AllowedEnvs ⊆ 项目环境表；
//   - CurrentEnv 为空 = 未固定命名环境，工具在本地项目根执行；
//   - CurrentEnv 首次由用户固定，之后 agent 与工具入参都不能修改或清空；
//   - AllowedEnvs 仅为旧状态兼容保留，新链路不再以它作为 SSH 使用闸门。
type SessionSSHInfo struct {
	SessionID string `json:"session_id"`
	// CurrentEnv 是会话首次绑定的固定工作环境；空 = 本地执行。绑定后不可切换。
	CurrentEnv  string   `json:"current_env,omitempty"`
	AllowedEnvs []string `json:"allowed_envs"`
}

// WorkspaceConnectionChange 是连接配置的写面。
//
// 口令三态由两个字段表达（**不要**用"空串"一个字段兼表两件事）：
//
//	SSHPassword 非空          → 写入（或轮换）
//	ClearSSHPassword = true   → 清除
//	两者都不给                → 保持原值
//
// UI 回填时拿不到旧口令，所以"保持原值"必须是默认行为，否则一次保存就会抹掉口令。
type WorkspaceConnectionChange struct {
	SSH *WorkspaceSSHInfo
	Git *WorkspaceGitInfo

	SSHPassword      string
	ClearSSHPassword bool
	GitPassword      string
	ClearGitPassword bool
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
	Effort     string `json:"effort"`
	FullAccess bool   `json:"full_access"`
	// ReasoningEffort 是**当前生效的 wire 思考强度**（provider 词表：
	// minimal/low/medium/high/max），也是 composer 底部「推理强度」拉条的权威读数。
	// 它由两条路写：effort 档位切换（档位=同时设置两条拉条的预设）与用户直接拖拉条
	// （SetReasoningEffort）。空串 = 尚未确定，前端按档位派生值显示。
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// LoopLimit 是**当前生效的单回合循环上限**（composer 底部「循环次数」拉条）。
	// 与 ReasoningEffort 同一口径：档位切换会把它覆盖成该档默认（15/48/384/768），
	// 拖拉条只改它自己。0 = 尚未确定（回退当前档位默认值）。
	LoopLimit int `json:"loop_limit,omitempty"`
	// PermissionTier 是本会话生效的权限档位（full_access 的档位化形态；full 档
	// ⇔ full_access=true）。
	PermissionTier string         `json:"permission_tier,omitempty"`
	Tokens         string         `json:"tokens,omitempty"`
	Replan         ReplanMonitor  `json:"replan"`
	Plan           *PlanState     `json:"plan,omitempty"`
	TodoItems      []dto.TodoItem `json:"todo_items,omitempty"`
	// SubAgentTree 是 fork 子代理树的权威投影（本会话槽）。
	SubAgentTree    []dto.SubAgentTreeNode  `json:"subagent_tree,omitempty"`
	GoalSkillActive bool                    `json:"goal_skill_active,omitempty"`
	GoalGovernance  *dto.GoalGovernanceView `json:"goal_governance,omitempty"`
	ActiveSkills    []string                `json:"active_skills,omitempty"`
	// WorkTable 是工作台统一工作表格的权威投影（本会话槽）。
	WorkTable        []WorkItem       `json:"work_table,omitempty"`
	WorkTableBatches []WorkTableBatch `json:"work_table_batches,omitempty"`
	// TeamworkBoard 是本会话团队看板的只读投影（计划 + 作业行 + 审计流水；
	// 未装配 teamwork 或该会话没有计划时为 nil，前端整块退场）。
	// 契约：docs/arch/team-board-gui-tui-contract.md。
	TeamworkBoard *dto.TeamworkBoardView `json:"teamwork_board,omitempty"`
}

// SessionSnapshot 是会话粒度、传输完备的快照制品（G3）：每会话一份，只承载
// 该会话的事实（会话身份/可见对话/聊天运行态/会话运行原件/task/工作表格/
// 子代理树/revision/窗口游标）。进程级目录与能力清单在 ProcessSnapshot。
type SessionSnapshot struct {
	ProtocolVersion      int            `json:"protocol_version"`
	Revision             uint64         `json:"revision"`
	ConversationRevision uint64         `json:"conversation_revision"`
	Session              SessionState   `json:"session"`
	Conversation         []Message      `json:"conversation"`
	Chat                 ChatState      `json:"chat"`
	Task                 *TaskState     `json:"task,omitempty"`
	Runtime              SessionRuntime `json:"runtime"`
	// Approvals 是本会话待批请求（G4 归属进 Unit 后填充；当前为预留）。
	Approvals          []Interaction `json:"approvals,omitempty"`
	Capabilities       Capabilities  `json:"capabilities"`
	HistoryOffset      int           `json:"history_offset"`
	TotalMessages      int           `json:"total_messages"`
	HasMoreHistory     bool          `json:"has_more_history"`
	ConversationWindow int           `json:"conversation_window"`
	ReadFiles          []ReadFileRef `json:"read_files,omitempty"`
	// ReviewRequest 是 agent 发起的「请 GUI 打开这些待审阅文件」批处理请求
	// （会话作用域的可重放状态；会话快照是补放的真相源）。nil = 无请求。
	// 契约见 dto.ReviewRequest。
	ReviewRequest *dto.ReviewRequest `json:"review_request,omitempty"`
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
	copySnapshot.Session.ForkedFrom = CloneSessionForkRef(snapshot.Session.ForkedFrom)
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
	copySnapshot.ReviewRequest = dto.CloneReviewRequest(snapshot.ReviewRequest)
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

// CloneSessionForkRef 返回会话血缘的独立副本。
func CloneSessionForkRef(ref *SessionForkRef) *SessionForkRef {
	if ref == nil {
		return nil
	}
	copyRef := *ref
	return &copyRef
}

// CloneRuntimeState returns an independent copy of mutable runtime slices.
func CloneRuntimeState(runtime RuntimeState) RuntimeState {
	copyRuntime := runtime
	copyRuntime.VisibleTools = append([]Tool(nil), runtime.VisibleTools...)
	copyRuntime.Skills = append([]SkillInfo(nil), runtime.Skills...)
	copyRuntime.Plugins = append([]PluginInfo(nil), runtime.Plugins...)
	copyRuntime.Accounts = append([]AccountInfo(nil), runtime.Accounts...)
	copyRuntime.PermissionTiers = append([]dto.PermissionTierInfo(nil), runtime.PermissionTiers...)
	copyRuntime.TodoItems = append([]dto.TodoItem(nil), runtime.TodoItems...)
	copyRuntime.ActiveSkills = append([]string(nil), runtime.ActiveSkills...)
	if runtime.ContextCache != nil {
		cache := *runtime.ContextCache
		cache.LastChanged = append([]string(nil), runtime.ContextCache.LastChanged...)
		copyRuntime.ContextCache = &cache
	}
	if runtime.GoalGovernance != nil {
		goalView := *runtime.GoalGovernance
		copyRuntime.GoalGovernance = &goalView
	}
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
	copyRuntime.TeamworkBoard = CloneTeamworkBoardView(runtime.TeamworkBoard)
	return copyRuntime
}

// CloneTeamworkBoardView 深拷贝团队看板投影：切片**与元素内嵌的切片**都要独立。
// 快照是并发读者的共享值，浅拷贝会让前端读到一半的写（work_items[].depends_on /
// milestones[].depends_on 这类内嵌切片最容易被漏掉——外层切片换新、内层仍指向同一
// 底层数组）。
func CloneTeamworkBoardView(view *dto.TeamworkBoardView) *dto.TeamworkBoardView {
	if view == nil {
		return nil
	}
	cloned := *view
	cloned.Members = append([]dto.TeamworkMemberView(nil), view.Members...)
	for index, member := range view.Members {
		// 装配两格也要独立：Plugins 是内嵌切片（同 depends_on 那一类），Assembly 是
		// **指针**（浅拷贝就是两个读者共享同一条读数，其中一人改一下两边都变）。
		cloned.Members[index].Plugins = append([]string(nil), member.Plugins...)
		if member.Assembly != nil {
			assembly := *member.Assembly
			assembly.Plugins = append([]string(nil), member.Assembly.Plugins...)
			assembly.PluginFaceMissing = append([]string(nil), member.Assembly.PluginFaceMissing...)
			cloned.Members[index].Assembly = &assembly
		}
	}
	if view.Milestones != nil {
		cloned.Milestones = make([]dto.TeamworkMilestoneView, len(view.Milestones))
		for index, milestone := range view.Milestones {
			cloned.Milestones[index] = milestone
			cloned.Milestones[index].DependsOn = append([]string(nil), milestone.DependsOn...)
			cloned.Milestones[index].Required = append([]string(nil), milestone.Required...)
		}
	}
	if view.WorkItems != nil {
		cloned.WorkItems = make([]dto.TeamworkWorkItemView, len(view.WorkItems))
		for index, item := range view.WorkItems {
			cloned.WorkItems[index] = item
			cloned.WorkItems[index].DependsOn = append([]string(nil), item.DependsOn...)
		}
	}
	cloned.Jobs = append([]dto.TeamworkJobView(nil), view.Jobs...)
	cloned.Events = append([]dto.TeamworkEventView(nil), view.Events...)
	return &cloned
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
