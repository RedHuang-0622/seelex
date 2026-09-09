package sessionstore

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// SessionContextSchemaVersion 是会话上下文记录（state blob）的版本号。
// 损坏或不兼容的记录拒绝加载并显式失败（不静默重建），走会话恢复错误路径。
//
// v2（当前）：新增 GoalStack（goal 域第五栈）。goal 栈与会话聊天记录同属
// 会话级持久化（sessionstore context 通道），但**不进模型上下文**：不渲染为
// 前缀/尾部栈块、不做记忆前缀与匹配、不参与压缩摘要提取；只用于会话恢复与
// 后续 goal 治理。聊天记录中的 #goal 文本是普通转录内容，随上下文窗口压缩。
// v1（旧）：无 GoalStack；加载时兼容迁移（内存补空栈，下次 Persist 升 v2）。
const (
	SessionContextSchemaVersion = 2
	// SessionContextSchemaVersionLegacy 是 v1 旧记录版本号（无 goal 栈）。
	// Load 时显式接受并迁移；其它版本拒绝。
	SessionContextSchemaVersionLegacy = 1
)

// EvidenceRef 是证据引用：指向不可变工具结果/事件库条目。
type EvidenceRef struct {
	Ref     string `json:"ref"`
	Summary string `json:"summary,omitempty"`
}

// PlanFrame 是 PlanStack 的一帧（now using plan = 栈顶）。
type PlanFrame struct {
	PlanID    string        `json:"plan_id"`
	Title     string        `json:"title"`
	Status    string        `json:"status"` // active | closed
	Nodes     []NodeSummary `json:"nodes,omitempty"`
	EnteredAt time.Time     `json:"entered_at"`
	ClosedAt  *time.Time    `json:"closed_at,omitempty"`
}

// NodeSummary 是 PlanFrame 内的节点摘要。
type NodeSummary struct {
	ID     string `json:"id"`
	Label  string `json:"label,omitempty"`
	Status string `json:"status,omitempty"`
}

// TaskFrame 是 TaskStack 的一帧（now using task = 栈顶）。
type TaskFrame struct {
	TaskID    string        `json:"task_id"`
	Objective string        `json:"objective"`
	Status    string        `json:"status"` // active | completed | failed | needs_user_decision
	Evidence  []EvidenceRef `json:"evidence,omitempty"`
	// EnteredAt 是任务进入使用栈的时间（fork 四栈按 fork 时刻过滤用；
	// 旧记录缺失时视为 fork 点之前，保守保留）。
	EnteredAt time.Time `json:"entered_at,omitempty"`
}

// GoalBudget 是 GoalFrame 内嵌的预算护栏（与 goal 域 Budget 同形，纯 DTO）。
type GoalBudget struct {
	MaxLoops      int `json:"max_loops,omitempty"`
	MaxTokens     int `json:"max_tokens,omitempty"`
	TLTokensShare int `json:"tl_tokens_share,omitempty"`
}

// GoalProgress 是 GoalFrame 内的一条进度/发现（与 goal 域 Progress 同形）。
type GoalProgress struct {
	At      int64  `json:"at"`
	Kind    string `json:"kind"`
	Content string `json:"content"`
}

// GoalFrame 是 GoalStack 的一帧（goal 域第五栈，now governing goal = 栈顶）。
// 只承载 goal 治理/恢复所需的记录投影：goal 域 Controller 的栈变更经
// ReplaceGoalStack 全量写回。
//
// GoalStack 是**活栈投影**（不是 plan/task 那种关闭帧原地保留的累积栈）：
// begin 压栈即写入，finish/abort 弹栈即**同步删除**该帧；栈存储初始与终态
// 都为空（治理结束后不留任何 goal 帧）。终态帧的会话内审计只存在进程内
// goal.Controller.History，不落 GoalStack。
//
// 本帧**不参与 seelexctx 栈块渲染、记忆前缀匹配与压缩摘要提取**（该边界由
// seelexctx 渲染器保证：仅消费 Plan/Task/Skill/Compact 四栈）。
type GoalFrame struct {
	GoalID     string            `json:"goal_id"`
	Title      string            `json:"title"`
	Statement  string            `json:"statement,omitempty"`
	Acceptance []string          `json:"acceptance,omitempty"`
	OutOfScope []string          `json:"out_of_scope,omitempty"`
	Budget     GoalBudget        `json:"budget"`
	Status     string            `json:"status"` // active | paused | completed | aborted | ...
	Progress   []GoalProgress    `json:"progress,omitempty"`
	Directives []string          `json:"tl_directives,omitempty"`
	Meta       map[string]string `json:"meta,omitempty"`
	CreatedAt  int64             `json:"created_at,omitempty"`
	UpdatedAt  int64             `json:"updated_at,omitempty"`
	FinishedAt int64             `json:"finished_at,omitempty"`
	// EnteredAt 是 goal 进入使用栈的时间（fork 第五栈按 fork 时刻过滤用；
	// 旧/缺省记录视为 fork 点之前，与 plan/task 帧同一保守语义）。
	EnteredAt time.Time `json:"entered_at,omitempty"`
}

// GoalAuditEntry 是 goal 生命周期审计条目（append-only，按会话隔离）。
// 记录 goal 的 begin/update/finish/abort/restore 等状态迁移；Seq 由本会话
// 存储单调递增（只追加、不回改既有条目）。账本不跨会话共享：goal 从发起到
// 收口都在其所属会话的账本内；若用户在**其它会话**完成了该 goal，装配方可
// 在收口条目上携带 SourceSession 出处（证明/追溯"完成于其它会话"），条目
// 仍留在原会话账本，不写入其它会话。
type GoalAuditEntry struct {
	Seq uint64 `json:"seq"`
	// Kind 与 goal 域 EventKind 对齐：goal.begin|goal.update|goal.finish|
	// goal.abort|goal.restore。
	Kind   string `json:"kind"`
	GoalID string `json:"goal_id"`
	Title  string `json:"title,omitempty"`
	Status string `json:"status,omitempty"`
	At     int64  `json:"at,omitempty"`
	// SourceSession 非空表示本条目记录的状态迁移发生在/被证实于其它会话
	// （如用户在其它会话完成了该 goal，收口时由装配层携带出处）。
	SourceSession string `json:"source_session,omitempty"`
	Reason        string `json:"reason,omitempty"`
	Result        string `json:"result,omitempty"`
	Detail        string `json:"detail,omitempty"`
}

// SkillFrame 是 SkillStack 的一帧（now using skill = 栈顶）。
type SkillFrame struct {
	SkillID string `json:"skill_id"`
	Name    string `json:"name"`
	// ActivatedAt 是 skill 激活进入使用栈的时间（fork 四栈按 fork 时刻
	// 过滤用；旧记录缺失时视为 fork 点之前，保守保留）。
	ActivatedAt time.Time `json:"activated_at,omitempty"`
}

// Revision 标识一次提交版本（CommitID + 单调序号）；root manifest 发布后
// 才可作为恢复引用（模块化方案 interfaces.md §基础类型）。
type Revision struct {
	CommitID string `json:"commit_id,omitempty"`
	Number   uint64 `json:"number"`
}

// CompactFrame 是 CompactStack 的一帧（now using compact context = 栈顶）。
// From/To 保留 ChatQueue（ProviderHistory）中被压缩轮次的单元索引（兼容
// 审计不变量 plan.md §3.7.4）；RoundFrom/RoundTo、EventFrom/EventTo、
// MessageFrom/MessageTo 与 EventRevision/ConversationRevision 是模块化方案
// 的正式恢复定位（architecture.md §3.3），随机 segment_id 不得承担截断语义。
type CompactFrame struct {
	SegmentID string `json:"segment_id"`
	From      int    `json:"from"`
	To        int    `json:"to"`
	// RoundFrom/RoundTo 是会话轮次范围（RoundNo 体系启用前为 0）。
	RoundFrom uint64 `json:"round_from,omitempty"`
	RoundTo   uint64 `json:"round_to,omitempty"`
	// EventFrom/EventTo 是 Transcript EventSeq 范围（含端点）。
	EventFrom uint64 `json:"event_from,omitempty"`
	EventTo   uint64 `json:"event_to,omitempty"`
	// MessageFrom/MessageTo 是 UI 消息定位键范围。
	MessageFrom string `json:"message_from,omitempty"`
	MessageTo   string `json:"message_to,omitempty"`
	// EventRevision/ConversationRevision 标识该范围所属的提交版本。
	EventRevision        Revision      `json:"event_revision"`
	ConversationRevision Revision      `json:"conversation_revision"`
	Summary              string        `json:"summary"`
	Evidence             []EvidenceRef `json:"evidence,omitempty"`
	CompressedAt         time.Time     `json:"compressed_at"`

	// 超上下文索引（2026-09-06 压缩 DAG 详设 §3.1）：本帧覆盖的首尾
	// requestID。只作元数据与检索定位，不进模型可见正文；旧记录缺省
	// 为空（向后兼容）。
	RequestFrom string `json:"request_from,omitempty"`
	RequestTo   string `json:"request_to,omitempty"`

	// 链锚点（详设 §3.1，二阶导）：只指向前驱帧，不复制前驱全文。
	// 栈序正确性靠 PrevSegmentID 存在性 + request 首尾接续校验。
	PrevSegmentID      string `json:"prev_segment_id,omitempty"`
	PrevRequestFrom    string `json:"prev_request_from,omitempty"`
	PrevRequestTo      string `json:"prev_request_to,omitempty"`
	PrevSummaryOneLine string `json:"prev_summary_one_line,omitempty"`

	// SummarySource/AnchorSource 是兜底质量标记（详设 §4.5）：
	//   SummarySource: replay（前缀重放厚摘要）| local（确定性本地折叠）
	//   AnchorSource:  ok（锚点完整）| degraded（一句话摘要降级为空）
	// 空值 = 旧记录未声明。
	SummarySource string `json:"summary_source,omitempty"`
	AnchorSource  string `json:"anchor_source,omitempty"`
}

// SessionContextRecord 是会话级上下文状态（state blob）：
// SystemPrompt（永不压缩）+ Plan/Task/Skill/Compact 四个使用栈 + GoalStack
// （goal 域第五栈，活栈投影，只服务治理/恢复，不渲染进模型上下文）
// + GoalAudit（goal 生命周期审计，append-only，只追加不回改）。
type SessionContextRecord struct {
	SchemaVersion int              `json:"schema_version"`
	SystemPrompt  string           `json:"system_prompt"`
	PlanStack     []PlanFrame      `json:"plan_stack"`
	TaskStack     []TaskFrame      `json:"task_stack"`
	SkillStack    []SkillFrame     `json:"skill_stack"`
	CompactStack  []CompactFrame   `json:"compact_stack"`
	GoalStack     []GoalFrame      `json:"goal_stack"`
	GoalAudit     []GoalAuditEntry `json:"goal_audit"`
}

// SessionContextStore 读写 Router 的独立 context 通道（WriteContextState/
// ReadContextState，与 SessionRecord 的 state blob 物理隔离），带内存缓存：
// 首次 Load 后栈操作只改内存，Persist 时才落盘。
type SessionContextStore struct {
	router    *Router
	sessionID string
	// resolverMu 只保护 workspaceResolver（与 DurableHistory 同语义：会话级
	// 键落盘不依赖 Router active write scope）。
	resolverMu        sync.RWMutex
	workspaceResolver func() string

	mu     sync.RWMutex
	record SessionContextRecord
	loaded bool
}

// NewSessionContextStore 创建会话上下文存储（惰性加载）。
func NewSessionContextStore(router *Router, sessionID string) *SessionContextStore {
	return &SessionContextStore{
		router:    router,
		sessionID: sessionID,
		record: SessionContextRecord{
			SchemaVersion: SessionContextSchemaVersion,
		},
	}
}

// SetWorkspaceResolver 注入会话绑定 workspace 解析器（PushCompact 的 v8
// compact 桥接与 Persist 使用显式键，不污染其它会话）。
func (s *SessionContextStore) SetWorkspaceResolver(resolver func() string) {
	if s == nil {
		return
	}
	s.resolverMu.Lock()
	s.workspaceResolver = resolver
	s.resolverMu.Unlock()
}

func (s *SessionContextStore) workspace() string {
	if s == nil {
		return ""
	}
	s.resolverMu.RLock()
	resolver := s.workspaceResolver
	s.resolverMu.RUnlock()
	if resolver != nil {
		return resolver()
	}
	if s.router != nil {
		return s.router.Workspace()
	}
	return ""
}

// Router 返回绑定的事件/历史路由（DurableHistory 装配接缝：滑动窗口加载
// 与 state blob 共享同一后端）。
func (s *SessionContextStore) Router() *Router {
	if s == nil {
		return nil
	}
	return s.router
}

// Load 从 state blob 读取记录到内存缓存。损坏/版本不兼容显式失败。
func (s *SessionContextStore) Load(ctx context.Context) error {
	if s == nil || s.router == nil || s.sessionID == "" {
		return fmt.Errorf("session context: router or session ID is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return nil
	}
	payload, err := s.router.LoadContextState(s.sessionID)
	if err != nil {
		if isSessionNotFound(err) {
			s.loaded = true
			return nil
		}
		return fmt.Errorf("session context: load state %q: %w", s.sessionID, err)
	}
	if len(payload) == 0 {
		s.loaded = true
		return nil
	}
	var record SessionContextRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return fmt.Errorf("session context: decode state %q: %w", s.sessionID, err)
	}
	if record.SchemaVersion != SessionContextSchemaVersion &&
		record.SchemaVersion != SessionContextSchemaVersionLegacy {
		return fmt.Errorf("session context: %q has unsupported schema version %d (want %d)",
			s.sessionID, record.SchemaVersion, SessionContextSchemaVersion)
	}
	// v1 → v2 迁移：v1 记录无 GoalStack/GoalAudit，内存补空栈/空账本（JSON
	// 字段缺省即 nil）；文件在下次 Persist（含任何栈操作）时统一升为
	// SessionContextSchemaVersion。
	if record.SchemaVersion == SessionContextSchemaVersionLegacy {
		if record.GoalStack == nil {
			record.GoalStack = []GoalFrame{}
		}
		if record.GoalAudit == nil {
			record.GoalAudit = []GoalAuditEntry{}
		}
		record.SchemaVersion = SessionContextSchemaVersion
	}
	s.record = record
	s.loaded = true
	return nil
}

// Persist 把内存缓存写入 state blob。
func (s *SessionContextStore) Persist(ctx context.Context) error {
	if s == nil || s.router == nil || s.sessionID == "" {
		return fmt.Errorf("session context: router or session ID is unavailable")
	}
	s.mu.RLock()
	record := s.record
	s.mu.RUnlock()
	record.SchemaVersion = SessionContextSchemaVersion
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("session context: encode state %q: %w", s.sessionID, err)
	}
	if err := s.router.SaveContextState(s.sessionID, payload); err != nil {
		return fmt.Errorf("session context: save state %q: %w", s.sessionID, err)
	}
	return nil
}

// ResetCache 清空内存缓存与记录（Clear/Reset 语义配合）。
func (s *SessionContextStore) ResetCache() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.record = SessionContextRecord{SchemaVersion: SessionContextSchemaVersion}
	s.loaded = false
	s.mu.Unlock()
}

// SetSystemPrompt 设置会话级基础提示。
// 会话不变量（plan.md §3.7.2）：SystemPrompt 永不压缩、始终完整进入 provider
// 请求，不写入 ProviderHistory 消息（不随 ChatQueue 持久化）。
func (s *SessionContextStore) SetSystemPrompt(prompt string) error {
	return s.update(func(record *SessionContextRecord) error {
		record.SystemPrompt = prompt
		return nil
	})
}

// SystemPrompt 返回当前会话级基础提示。
func (s *SessionContextStore) SystemPrompt() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.record.SystemPrompt
}

// Snapshot 返回记录的内存拷贝（供 Assembler 渲染，不暴露内部状态）。
func (s *SessionContextStore) Snapshot() SessionContextRecord {
	if s == nil {
		return SessionContextRecord{SchemaVersion: SessionContextSchemaVersion}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSessionContextRecord(s.record)
}

// ── 栈操作（plan.md §3.7.2）──────────────────────────────────────────

// PushPlan 在 plan_load/plan_run 进入时压栈。
func (s *SessionContextStore) PushPlan(frame PlanFrame) error {
	return s.update(func(record *SessionContextRecord) error {
		if frame.PlanID == "" {
			return fmt.Errorf("session context: plan frame requires plan_id")
		}
		record.PlanStack = append(record.PlanStack, frame)
		return nil
	})
}

// CloseTopPlan 关闭栈顶匹配的 Plan 帧（终态/被取代时）。
func (s *SessionContextStore) CloseTopPlan(planID string) error {
	return s.update(func(record *SessionContextRecord) error {
		stack := record.PlanStack
		for index := len(stack) - 1; index >= 0; index-- {
			if stack[index].PlanID == planID {
				now := time.Now()
				stack[index].ClosedAt = &now
				stack[index].Status = "closed"
				return nil
			}
		}
		return fmt.Errorf("session context: plan %q is not on the plan stack", planID)
	})
}

// PushTask 在任务开始时压栈。
func (s *SessionContextStore) PushTask(frame TaskFrame) error {
	return s.update(func(record *SessionContextRecord) error {
		if frame.TaskID == "" {
			return fmt.Errorf("session context: task frame requires task_id")
		}
		if frame.EnteredAt.IsZero() {
			frame.EnteredAt = time.Now()
		}
		record.TaskStack = append(record.TaskStack, frame)
		return nil
	})
}

// CloseTopTask 关闭栈顶匹配的任务帧（终态工具接受后）。
func (s *SessionContextStore) CloseTopTask(taskID string) error {
	return s.update(func(record *SessionContextRecord) error {
		stack := record.TaskStack
		for index := len(stack) - 1; index >= 0; index-- {
			if stack[index].TaskID == taskID {
				stack[index].Status = "completed"
				return nil
			}
		}
		return fmt.Errorf("session context: task %q is not on the task stack", taskID)
	})
}

// ── goal 第五栈操作（goal 域适配；见 GoalFrame 注释）────────────────────

// PushGoal 在 goal_begin 压栈时追加一帧（goal 治理活栈：写入即当前栈投影，
// 不渲染进模型上下文）。终态帧不保留——收口/放弃走 CloseTopGoal 弹栈删除。
func (s *SessionContextStore) PushGoal(frame GoalFrame) error {
	return s.update(func(record *SessionContextRecord) error {
		if frame.GoalID == "" {
			return fmt.Errorf("session context: goal frame requires goal_id")
		}
		if frame.EnteredAt.IsZero() {
			frame.EnteredAt = time.Now()
		}
		record.GoalStack = append(record.GoalStack, frame)
		return nil
	})
}

// CloseTopGoal 弹掉栈顶 goal 帧并从持久化栈**同步删除**（goal 是 LIFO：
// 只有栈顶可收口/放弃；下层恢复 active 属 goal 域状态机，Controller 在
// Save 前已处理）。弹栈后栈存储为空即代表治理收口、无残留。
func (s *SessionContextStore) CloseTopGoal(goalID string) error {
	return s.update(func(record *SessionContextRecord) error {
		stack := record.GoalStack
		if len(stack) == 0 {
			return fmt.Errorf("session context: goal %q is not on the goal stack (empty)", goalID)
		}
		top := stack[len(stack)-1]
		if top.GoalID != goalID {
			return fmt.Errorf("session context: goal %q is not the stack top (top=%q)", goalID, top.GoalID)
		}
		record.GoalStack = append([]GoalFrame(nil), stack[:len(stack)-1]...)
		return nil
	})
}

// GoalStackSnapshot 返回 goal 栈的深拷贝（供 goal.Controller Reload 使用；
// 只读面，不暴露内部状态）。
func (s *SessionContextStore) GoalStackSnapshot() []GoalFrame {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneGoalFrames(s.record.GoalStack)
}

// ReplaceGoalStack 全量替换 goal 栈并持久化（goal.Controller 的 Store.Save
// 语义：每次状态机变更后保存当前栈投影；收口弹栈后终态帧从栈消失）。
func (s *SessionContextStore) ReplaceGoalStack(frames []GoalFrame) error {
	return s.update(func(record *SessionContextRecord) error {
		record.GoalStack = cloneGoalFrames(frames)
		for _, frame := range record.GoalStack {
			if frame.GoalID == "" {
				return fmt.Errorf("session context: goal frame requires goal_id")
			}
		}
		return nil
	})
}

// ── goal 审计账本（append-only，按会话隔离）──────────────────────────

// AppendGoalAudit 追加一条 goal 审计条目：Seq 由本会话账本单调递增
// （last+1，首条=1）；只追加不回改既有条目。GoalStack 弹栈即删除（终态帧
// 不进活栈），审计则保留收口记录——两者是"活栈 vs 审计"两个正交面。
func (s *SessionContextStore) AppendGoalAudit(entry GoalAuditEntry) error {
	return s.update(func(record *SessionContextRecord) error {
		if entry.Kind == "" || entry.GoalID == "" {
			return fmt.Errorf("session context: goal audit entry requires kind and goal_id")
		}
		entry.Seq = 1
		if length := len(record.GoalAudit); length > 0 {
			entry.Seq = record.GoalAudit[length-1].Seq + 1
		}
		record.GoalAudit = append(record.GoalAudit, entry)
		return nil
	})
}

// GoalAuditSnapshot 返回本会话 goal 审计账本的深拷贝（按 Seq 追加顺序；
// 调用方不得据此回改原账本）。
func (s *SessionContextStore) GoalAuditSnapshot() []GoalAuditEntry {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneGoalAudit(s.record.GoalAudit)
}

// PushSkill 在 skill 激活时压栈。
func (s *SessionContextStore) PushSkill(frame SkillFrame) error {
	return s.update(func(record *SessionContextRecord) error {
		if frame.SkillID == "" {
			return fmt.Errorf("session context: skill frame requires skill_id")
		}
		if frame.ActivatedAt.IsZero() {
			frame.ActivatedAt = time.Now()
		}
		record.SkillStack = append(record.SkillStack, frame)
		return nil
	})
}

// PopSkill 在 skill 退出时弹栈（匹配最近一帧）。
func (s *SessionContextStore) PopSkill(skillID string) error {
	return s.update(func(record *SessionContextRecord) error {
		stack := record.SkillStack
		for index := len(stack) - 1; index >= 0; index-- {
			if stack[index].SkillID == skillID {
				record.SkillStack = append(stack[:index], stack[index+1:]...)
				return nil
			}
		}
		return fmt.Errorf("session context: skill %q is not on the skill stack", skillID)
	})
}

// PushCompact 在窗口外压缩时压入摘要帧。
func (s *SessionContextStore) PushCompact(frame CompactFrame) error {
	err := s.update(func(record *SessionContextRecord) error {
		if frame.SegmentID == "" {
			return fmt.Errorf("session context: compact frame requires segment_id")
		}
		// From/To 语义校验（审计 R6）：累计 ChatQueue 单元索引，合并帧
		// From 恒为合并起点（0），To 单调递增 —— 负区间/倒置拒绝。
		if frame.From < 0 || frame.To < frame.From {
			return fmt.Errorf("session context: compact frame invalid range [%d,%d]", frame.From, frame.To)
		}
		// 正式定位范围校验（architecture.md §3.3）：RoundNo/EventSeq 范围
		// 要么都为零（未启用），要么成对给出且 From <= To；半填充拒绝。
		if (frame.RoundFrom == 0) != (frame.RoundTo == 0) || frame.RoundFrom > frame.RoundTo {
			return fmt.Errorf("session context: compact frame invalid round range [%d,%d]", frame.RoundFrom, frame.RoundTo)
		}
		if (frame.EventFrom == 0) != (frame.EventTo == 0) || frame.EventFrom > frame.EventTo {
			return fmt.Errorf("session context: compact frame invalid event range [%d,%d]", frame.EventFrom, frame.EventTo)
		}
		// 超上下文 request 索引（详设 §3.4）：同空或同非空；非空时按
		// 字符串序非倒置（首尾覆盖语义）。
		if (frame.RequestFrom == "") != (frame.RequestTo == "") {
			return fmt.Errorf("session context: compact frame request range must be both empty or both set")
		}
		if frame.RequestFrom != "" && frame.RequestFrom > frame.RequestTo {
			return fmt.Errorf("session context: compact frame invalid request range [%q,%q]",
				frame.RequestFrom, frame.RequestTo)
		}
		// 链锚点（详设 §3.4）：PrevSegmentID == "" 当且仅当本帧为首帧；
		// 非首帧必须给出且与栈内上一帧 SegmentID 一致。
		if len(record.CompactStack) == 0 {
			if frame.PrevSegmentID != "" {
				return fmt.Errorf("session context: first compact frame must not carry prev_segment_id")
			}
			record.CompactStack = append(record.CompactStack, frame)
			return nil
		}
		top := record.CompactStack[len(record.CompactStack)-1]
		if frame.PrevSegmentID == "" {
			return fmt.Errorf("session context: non-first compact frame requires prev_segment_id")
		}
		if frame.PrevSegmentID != top.SegmentID {
			return fmt.Errorf("session context: prev_segment_id %q must match stack top %q",
				frame.PrevSegmentID, top.SegmentID)
		}
		if frame.PrevRequestFrom != top.RequestFrom || frame.PrevRequestTo != top.RequestTo {
			return fmt.Errorf("session context: prev request range [%q,%q] must match stack top [%q,%q]",
				frame.PrevRequestFrom, frame.PrevRequestTo, top.RequestFrom, top.RequestTo)
		}
		record.CompactStack = append(record.CompactStack, frame)
		return nil
	})
	if err != nil || s.router == nil || s.sessionID == "" {
		return err
	}
	// v8 运行期接线：把运行期 compact 帧桥接进 v8 compact 通道（帧摘要进入
	// R2 装配）；非 v8 布局由 Router 返回 ok=false。桥接失败不回滚已持久化
	// 的 context 栈（compact.jsonl 可重建，下次提交重试/忽略）。
	if ok, bridgeErr := s.router.CommitCompactFrameWorkspace(s.workspace(), s.sessionID, frame); bridgeErr == nil && ok {
		_, _ = s.router.RetentionAdvisoryWorkspace(s.workspace(), s.sessionID)
	}
	return nil
}

// update 在加锁下执行栈操作并持久化 state blob。
func (s *SessionContextStore) update(mutate func(*SessionContextRecord) error) error {
	if s == nil || s.router == nil || s.sessionID == "" {
		return fmt.Errorf("session context: router or session ID is unavailable")
	}
	s.mu.Lock()
	err := mutate(&s.record)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return s.Persist(context.Background())
}

func cloneSessionContextRecord(record SessionContextRecord) SessionContextRecord {
	clone := record
	clone.PlanStack = append([]PlanFrame(nil), record.PlanStack...)
	clone.TaskStack = append([]TaskFrame(nil), record.TaskStack...)
	clone.SkillStack = append([]SkillFrame(nil), record.SkillStack...)
	clone.CompactStack = append([]CompactFrame(nil), record.CompactStack...)
	clone.GoalStack = cloneGoalFrames(record.GoalStack)
	clone.GoalAudit = cloneGoalAudit(record.GoalAudit)
	return clone
}

func cloneGoalFrames(frames []GoalFrame) []GoalFrame {
	if len(frames) == 0 {
		return nil
	}
	out := make([]GoalFrame, len(frames))
	for index := range frames {
		out[index] = frames[index]
		out[index].Acceptance = append([]string(nil), frames[index].Acceptance...)
		out[index].OutOfScope = append([]string(nil), frames[index].OutOfScope...)
		out[index].Progress = append([]GoalProgress(nil), frames[index].Progress...)
		out[index].Directives = append([]string(nil), frames[index].Directives...)
		if frames[index].Meta != nil {
			out[index].Meta = make(map[string]string, len(frames[index].Meta))
			for key, value := range frames[index].Meta {
				out[index].Meta[key] = value
			}
		}
	}
	return out
}

func cloneGoalAudit(entries []GoalAuditEntry) []GoalAuditEntry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]GoalAuditEntry, len(entries))
	copy(out, entries)
	return out
}
