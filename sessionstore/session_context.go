package sessionstore

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// SessionContextSchemaVersion 是会话上下文记录（context blob）的版本号。
// 损坏或不兼容的记录拒绝加载并显式失败（不静默重建），走会话恢复错误路径。
//
// v3（当前）：blob 不再承载三栈——plan/task/goal 由 §2.4 栈通道
// （session/{plan,task,goal}/{active,history}.jsonl + metadata/stack.json）
// 独占；context 只剩尚未按设计稿分文件的字段（system_prompt / skill 记录 /
// compact 栈 / goal 审计），这几项分别在 S2/S4 收口。
// v2 及更早（栈内联在 blob 里）不再兼容读取。
const SessionContextSchemaVersion = 3

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

// Load 装载会话上下文：blob（system/skill/compact/audit）+ 三栈（§2.4 通道，
// 权威）。通道回读在不持 s.mu 的外层做，避免与 refreshStack 的加锁嵌套。
func (s *SessionContextStore) Load(ctx context.Context) error {
	if err := s.loadBlob(ctx); err != nil {
		return err
	}
	for _, kind := range []StackKind{StackKindPlan, StackKindTask, StackKindGoal} {
		if err := s.refreshStack(kind); err != nil {
			return fmt.Errorf("session context: load %s stack %q: %w", kind, s.sessionID, err)
		}
	}
	return nil
}

// loadBlob 从 context blob 读取记录到内存缓存。损坏或版本不兼容显式失败。
func (s *SessionContextStore) loadBlob(_ context.Context) error {
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
	var record SessionContextRecord
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &record); err != nil {
			return fmt.Errorf("session context: decode state %q: %w", s.sessionID, err)
		}
		if record.SchemaVersion != SessionContextSchemaVersion {
			return fmt.Errorf("session context: %q has unsupported schema version %d (want %d)",
				s.sessionID, record.SchemaVersion, SessionContextSchemaVersion)
		}
	}
	s.record = record
	s.loaded = true
	return nil
}

// Persist 把内存缓存写入 context blob。三栈不在此落盘（权威只有 §2.4 栈
// 通道），落盘前剥离，避免 blob 里留一份可被回读的副本。
func (s *SessionContextStore) Persist(ctx context.Context) error {
	if s == nil || s.router == nil || s.sessionID == "" {
		return fmt.Errorf("session context: router or session ID is unavailable")
	}
	s.mu.RLock()
	record := s.record
	s.mu.RUnlock()
	record.SchemaVersion = SessionContextSchemaVersion
	record.PlanStack = nil
	record.TaskStack = nil
	record.GoalStack = nil
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

// ── 栈操作（plan/task/goal 走 §2.4 栈通道，非 context blob）──────────────

// writeStack 把一次栈变更写进通道并回读该 kind 的 active 投影。
func (s *SessionContextStore) writeStack(kind StackKind, mutate func() error) error {
	if err := mutate(); err != nil {
		return err
	}
	return s.refreshStack(kind)
}

// refreshStack 从栈通道回读指定 kind 的 active 条目覆盖内存投影。
func (s *SessionContextStore) refreshStack(kind StackKind) error {
	projectID := s.workspace()
	rows, err := s.router.StackActive(projectID, s.sessionID, kind)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch kind {
	case StackKindPlan:
		s.record.PlanStack = s.record.PlanStack[:0]
		for _, row := range rows {
			var frame PlanFrame
			if json.Unmarshal(row.Payload, &frame) == nil {
				frame.PlanID = row.ItemID
				frame.Status = row.Status
				s.record.PlanStack = append(s.record.PlanStack, frame)
			}
		}
	case StackKindTask:
		s.record.TaskStack = s.record.TaskStack[:0]
		for _, row := range rows {
			var frame TaskFrame
			if json.Unmarshal(row.Payload, &frame) == nil {
				frame.TaskID = row.ItemID
				frame.Status = row.Status
				s.record.TaskStack = append(s.record.TaskStack, frame)
			}
		}
	case StackKindGoal:
		s.record.GoalStack = s.record.GoalStack[:0]
		for _, row := range rows {
			var frame GoalFrame
			if json.Unmarshal(row.Payload, &frame) == nil {
				frame.GoalID = row.ItemID
				frame.Status = row.Status
				frame.EnteredAt = row.EnteredAt
				s.record.GoalStack = append(s.record.GoalStack, frame)
			}
		}
	}
	return nil
}

// stackPayload 编码栈帧为通道 payload。
func stackPayload(frame any) json.RawMessage {
	data, err := json.Marshal(frame)
	if err != nil {
		return nil
	}
	return data
}

// PushPlan 在 plan_load/plan_run 进入时压栈（一个计划 = 一个批次）。
func (s *SessionContextStore) PushPlan(frame PlanFrame) error {
	if frame.PlanID == "" {
		return fmt.Errorf("session context: plan frame requires plan_id")
	}
	if frame.EnteredAt.IsZero() {
		frame.EnteredAt = time.Now()
	}
	status := frame.Status
	if status == "" {
		status = "active"
	}
	return s.writeStack(StackKindPlan, func() error {
		_, err := s.router.StackPush(s.workspace(), s.sessionID, StackKindPlan, "plan:"+frame.PlanID, []StackItemInput{{
			ItemID: frame.PlanID, Kind: StackKindPlan, Status: status,
			Payload: stackPayload(frame), EnteredAt: frame.EnteredAt,
		}})
		return err
	})
}

// CloseTopPlan 关闭栈顶匹配的 Plan 帧；该计划批次随即整批归档（§2.4）。
func (s *SessionContextStore) CloseTopPlan(planID string) error {
	if err := s.ensureStackItem(StackKindPlan, planID, "plan"); err != nil {
		return err
	}
	return s.writeStack(StackKindPlan, func() error {
		_, err := s.router.StackSetStatus(s.workspace(), s.sessionID, StackKindPlan, planID, "closed")
		return err
	})
}

// PushTask 在任务开始时压栈（一个任务 = 一个批次）。
func (s *SessionContextStore) PushTask(frame TaskFrame) error {
	if frame.TaskID == "" {
		return fmt.Errorf("session context: task frame requires task_id")
	}
	if frame.EnteredAt.IsZero() {
		frame.EnteredAt = time.Now()
	}
	status := frame.Status
	if status == "" {
		status = "active"
	}
	return s.writeStack(StackKindTask, func() error {
		_, err := s.router.StackPush(s.workspace(), s.sessionID, StackKindTask, "task:"+frame.TaskID, []StackItemInput{{
			ItemID: frame.TaskID, Kind: StackKindTask, Status: status,
			Payload: stackPayload(frame), EnteredAt: frame.EnteredAt,
		}})
		return err
	})
}

// CloseTopTask 关闭栈顶匹配的任务帧（终态工具接受后）。
func (s *SessionContextStore) CloseTopTask(taskID string) error {
	if err := s.ensureStackItem(StackKindTask, taskID, "task"); err != nil {
		return err
	}
	return s.writeStack(StackKindTask, func() error {
		_, err := s.router.StackSetStatus(s.workspace(), s.sessionID, StackKindTask, taskID, "completed")
		return err
	})
}

// ensureStackItem 校验条目确实在该 kind 的 active 栈上（保持旧的显式失败语义）。
func (s *SessionContextStore) ensureStackItem(kind StackKind, itemID, label string) error {
	if err := s.Load(context.Background()); err != nil {
		return err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var ids []string
	switch kind {
	case StackKindPlan:
		for _, frame := range s.record.PlanStack {
			ids = append(ids, frame.PlanID)
		}
	case StackKindTask:
		for _, frame := range s.record.TaskStack {
			ids = append(ids, frame.TaskID)
		}
	case StackKindGoal:
		for _, frame := range s.record.GoalStack {
			ids = append(ids, frame.GoalID)
		}
	}
	for _, id := range ids {
		if id == itemID {
			return nil
		}
	}
	return fmt.Errorf("session context: %s %q is not on the %s stack", label, itemID, kind)
}

// ── goal 第五栈操作（goal 域适配；见 GoalFrame 注释）────────────────────

// PushGoal 在 goal_begin 压栈时追加一帧（goal 治理活栈，单条目批次 LIFO）。
// goal 栈不入模型上下文，只服务恢复与治理。
func (s *SessionContextStore) PushGoal(frame GoalFrame) error {
	if frame.GoalID == "" {
		return fmt.Errorf("session context: goal frame requires goal_id")
	}
	if frame.EnteredAt.IsZero() {
		frame.EnteredAt = time.Now()
	}
	status := frame.Status
	if status == "" {
		status = "active"
	}
	return s.writeStack(StackKindGoal, func() error {
		_, err := s.router.StackPush(s.workspace(), s.sessionID, StackKindGoal, "goal:"+frame.GoalID, []StackItemInput{{
			ItemID: frame.GoalID, Kind: StackKindGoal, Status: status,
			Payload: stackPayload(frame), EnteredAt: frame.EnteredAt,
		}})
		return err
	})
}

// CloseTopGoal 弹掉栈顶 goal 帧并归档（goal 是 LIFO：只有栈顶可收口/放弃；
// 下层恢复 active 属 goal 域状态机）。
func (s *SessionContextStore) CloseTopGoal(goalID string) error {
	s.mu.RLock()
	empty := len(s.record.GoalStack) == 0
	var top string
	if !empty {
		top = s.record.GoalStack[len(s.record.GoalStack)-1].GoalID
	}
	s.mu.RUnlock()
	if empty {
		return fmt.Errorf("session context: goal %q is not on the goal stack (empty)", goalID)
	}
	if top != goalID {
		return fmt.Errorf("session context: goal %q is not the stack top (top=%q)", goalID, top)
	}
	return s.writeStack(StackKindGoal, func() error {
		_, err := s.router.StackPopTop(s.workspace(), s.sessionID, StackKindGoal, goalID, "closed")
		return err
	})
}

// GoalStackSnapshot 返回 goal 栈的深拷贝（供 goal.Controller Reload 使用）。
func (s *SessionContextStore) GoalStackSnapshot() []GoalFrame {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneGoalFrames(s.record.GoalStack)
}

// ReplaceGoalStack 用当前栈投影替换 goal active 栈：通道按逐条迁移落地
// （新增压栈、状态变化更新、投影中消失的条目按其末态归档），因此 goal 的
// begin/update/finish/abort 都会留下 goal.* EVENT（§2.4 + 附录 A.1）。
func (s *SessionContextStore) ReplaceGoalStack(frames []GoalFrame) error {
	items := make([]StackItemInput, 0, len(frames))
	for _, frame := range frames {
		if frame.GoalID == "" {
			return fmt.Errorf("session context: goal frame requires goal_id")
		}
		items = append(items, StackItemInput{
			ItemID: frame.GoalID, Kind: StackKindGoal, Status: frame.Status,
			Payload: stackPayload(frame), EnteredAt: frame.EnteredAt,
		})
	}
	return s.writeStack(StackKindGoal, func() error {
		_, err := s.router.StackReplace(s.workspace(), s.sessionID, StackKindGoal, items, "closed")
		return err
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
	// 运行期接线：把运行期 compact 帧桥接进 compact 通道（帧摘要进入
	// wire 装配）；非 会话存储布局由 Router 返回 ok=false。桥接失败不回滚已持久化
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
