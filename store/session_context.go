package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	storecontext "github.com/RedHuang-0622/seelex/store/context"
)

// SessionContextSchemaVersion 是会话上下文记录（context blob）的版本号。
// 损坏或不兼容的记录拒绝加载并显式失败（不静默重建），走会话恢复错误路径。
//
// v3（当前）：blob 不再承载三栈——plan/task/goal 由 §2.4 栈通道
// （session/{plan,task,goal}/{active,history}.jsonl +
// metadata/stack_{plan,task,goal}.json）
// 独占；context 只剩尚未按设计稿分文件的字段（system_prompt / skill 记录 /
// compact 栈 / goal 审计），这几项分别在 S2/S4 收口。
// v2 及更早（栈内联在 blob 里）不再兼容读取。
const SessionContextSchemaVersion = storecontext.SessionContextSchemaVersion

type EvidenceRef = storecontext.EvidenceRef
type PlanFrame = storecontext.PlanFrame
type NodeSummary = storecontext.NodeSummary
type TaskFrame = storecontext.TaskFrame
type GoalBudget = storecontext.GoalBudget
type GoalProgress = storecontext.GoalProgress
type GoalFrame = storecontext.GoalFrame
type GoalAuditEntry = storecontext.GoalAuditEntry
type SkillFrame = storecontext.SkillFrame
type Revision = storecontext.Revision
type CompactFrame = storecontext.CompactFrame
type SessionContextRecord = storecontext.SessionContextRecord

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

	// bridgeMu 串行 compact 链的"压栈 → 桥接落盘 → 持久化"整段（§2.8）：
	// s.mu 只护 record 与纯读，落盘一律在 s.mu 之外；帧靠 PrevSegmentID
	// 成链、compact 通道会拒绝乱序帧，所以"落盘顺序 = 压栈顺序"必须由某把
	// 锁保证——bridgeMu 就是它，且它不覆盖任何纯读。
	bridgeMu sync.Mutex
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
//
// 注入是**装配义务**：未注入时按默认项目（未关联会话）落盘，而不是退回活跃
// 写作用域——用视图作用域当会话键正是同一会话在两个项目目录里长出两份的根因
// （见 workspace）。
func (s *SessionContextStore) SetWorkspaceResolver(resolver func() string) {
	if s == nil {
		return
	}
	s.resolverMu.Lock()
	s.workspaceResolver = resolver
	s.resolverMu.Unlock()
}

// workspace 返回本会话的落盘/回读项目键（R3 键漂移纪律）。
//
// 定序只有一条：注入的会话绑定解析器 → ""（未关联会话 = 默认项目）。
// **刻意不回退 Router 的活跃写作用域**：活跃作用域是**视图**状态（用户现在在看
// 哪个项目），会随切换改变；拿它当会话级键，system prompt / 压缩帧就写进了视图
// 项目——同一会话因此在两个项目目录里各留一份，目录枚举按项目分格（前端把它显示
// 成两个会话，尽管没有任何 fork 指令）。装配点必须注入绑定解析器，见
// wiring/adapters.SessionPort.AttachSessionContext。
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
		if IsSessionNotFound(err) {
			// v8 会话可以没有 context blob（各字段已有独立落点）——继续从
			// 模块/通道回读，而不是提前结束（S19）。
			payload = nil
		} else {
			return fmt.Errorf("session context: load state %q: %w", s.sessionID, err)
		}
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
	if record.SchemaVersion == 0 {
		record.SchemaVersion = SessionContextSchemaVersion
	}
	s.record = record
	// S19：system prompt 权威在 metadata/system.json（v8 JSON 布局）；blob 只
	// 保留未 v8 化字段。
	if snapshot, handled, err := s.router.LoadSystemPromptWorkspace(s.workspace(), s.sessionID); err != nil {
		if !IsSessionNotFound(err) {
			return fmt.Errorf("session context: load system prompt %q: %w", s.sessionID, err)
		}
	} else if handled && snapshot.Prompt != "" {
		s.record.SystemPrompt = snapshot.Prompt
	}
	// S19：CompactStack 权威在 compact 通道；v8 JSON 布局从通道回读（blob
	// 不再双写），未 v8 化后端保留 blob。
	if frames, handled, err := s.router.CompactFramesWorkspace(s.workspace(), s.sessionID); err != nil {
		if !IsSessionNotFound(err) {
			return fmt.Errorf("session context: load compact frames %q: %w", s.sessionID, err)
		}
	} else if handled {
		s.record.CompactStack = frames
	}
	if s.router.LayoutV8() {
		// D1/S19：skill 记录不再是独立栈（普通 message 行承载正文），blob
		// 不再持久化 SkillStack；GoalAudit 权威在 EVENT goal.*。
		s.record.SkillStack = nil
		if entries, handled, err := s.router.GoalAuditEvents(s.workspace(), s.sessionID); err != nil {
			return fmt.Errorf("session context: load goal audit %q: %w", s.sessionID, err)
		} else if handled {
			s.record.GoalAudit = entries
		}
	}
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
	// S19：system prompt 迁 metadata/system.json；blob 不再承载该字段。
	if handled, err := s.router.SaveSystemPromptWorkspace(s.workspace(), s.sessionID, record.SystemPrompt, ""); err != nil {
		return fmt.Errorf("session context: save system prompt %q: %w", s.sessionID, err)
	} else if handled {
		record.SystemPrompt = ""
	}
	// S19：CompactStack 停双写——权威 = compact 通道（帧已由 PushCompact
	// 桥接落盘），v8 JSON 布局的 blob 不再承载。
	compactHandled := false
	if _, handled, err := s.router.CompactFramesWorkspace(s.workspace(), s.sessionID); err != nil {
		if !IsSessionNotFound(err) {
			return fmt.Errorf("session context: check compact channel %q: %w", s.sessionID, err)
		}
	} else if handled {
		compactHandled = true
	}
	if compactHandled {
		record.CompactStack = nil
	}
	if s.router.LayoutV8() {
		// S19/D1：SkillStack 停 blob（普通 message 行）；GoalAudit 由 EVENT
		// 通道承载。
		record.SkillStack = nil
		record.GoalAudit = nil
	}
	record.PlanStack = nil
	record.TaskStack = nil
	record.GoalStack = nil
	if s.router.LayoutV8() {
		// S19 收口：v8 JSON 的 context.json 已无任何字段承载（system/compact/
		// skill/goal/audit 全有独立落点）——整文件停写。
		return nil
	}
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

// CompactStackSnapshot 只返回压缩栈的深拷贝。压缩检索、重复压缩基准与相关记忆
// 选取不得为了读取该栈而依赖完整上下文记录。
func (s *SessionContextStore) CompactStackSnapshot() []CompactFrame {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneCompactFrames(s.record.CompactStack)
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
	item, err := storecontext.NewPlanStackInput(frame, time.Now())
	if err != nil {
		return err
	}
	return s.writeStack(StackKindPlan, func() error {
		_, err := s.router.StackPush(s.workspace(), s.sessionID, StackKindPlan, "plan:"+frame.PlanID, []StackItemInput{item})
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
	item, err := storecontext.NewTaskStackInput(frame, time.Now())
	if err != nil {
		return err
	}
	return s.writeStack(StackKindTask, func() error {
		_, err := s.router.StackPush(s.workspace(), s.sessionID, StackKindTask, "task:"+frame.TaskID, []StackItemInput{item})
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
	item, err := storecontext.NewGoalStackInput(frame, time.Now())
	if err != nil {
		return err
	}
	return s.writeStack(StackKindGoal, func() error {
		_, err := s.router.StackPush(s.workspace(), s.sessionID, StackKindGoal, "goal:"+frame.GoalID, []StackItemInput{item})
		return err
	})
}

// CloseTopGoal 弹掉栈顶 goal 帧并归档（goal 是 LIFO：只有栈顶可收口/放弃；
// 下层恢复 active 属 goal 域状态机）。
func (s *SessionContextStore) CloseTopGoal(goalID string) error {
	s.mu.RLock()
	stack := append([]GoalFrame(nil), s.record.GoalStack...)
	s.mu.RUnlock()
	if err := storecontext.RequireTopGoal(stack, goalID); err != nil {
		return err
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
			RoleName: frame.RoleName, RoleSessionID: frame.RoleSessionID,
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
	if s == nil || s.router == nil || s.sessionID == "" {
		return fmt.Errorf("session context: router or session ID is unavailable")
	}
	if entry.Kind == "" || entry.GoalID == "" {
		return fmt.Errorf("session context: goal audit entry requires kind and goal_id")
	}
	s.mu.Lock()
	entry.Seq = uint64(len(s.record.GoalAudit) + 1)
	s.record.GoalAudit = append(s.record.GoalAudit, entry)
	s.loaded = true
	s.mu.Unlock()
	// S19：v8 JSON 布局权威在 EVENT goal.*；未 v8 化后端保留 blob。
	if handled, err := s.router.AppendGoalAuditEvent(s.workspace(), s.sessionID, entry); err != nil {
		return fmt.Errorf("session context: append goal audit %q: %w", s.sessionID, err)
	} else if handled {
		return nil
	}
	return s.Persist(context.Background())
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
		stack, err := storecontext.PushSkillFrame(record.SkillStack, frame, time.Now())
		if err != nil {
			return err
		}
		record.SkillStack = stack
		return nil
	})
}

// PopSkill 在 skill 退出时弹栈（匹配最近一帧）。
func (s *SessionContextStore) PopSkill(skillID string) error {
	return s.update(func(record *SessionContextRecord) error {
		stack, err := storecontext.PopSkillFrame(record.SkillStack, skillID)
		if err != nil {
			return err
		}
		record.SkillStack = stack
		return nil
	})
}

// PushCompact 在窗口外压缩时压入摘要帧。
//
// 锁纪律（§2.8）：s.mu 内只做校验与压栈（内存），compact 通道落盘（bridge）
// 移到 s.mu 之外——写盘期间不再阻塞本会话的纯读。帧靠 PrevSegmentID 成链、
// 通道会拒绝乱序帧，所以落盘必须与压栈同序：bridgeMu 串行"压栈 → 落盘 →
// 持久化"整段来保证（它不覆盖任何纯读）。
func (s *SessionContextStore) PushCompact(frame CompactFrame) error {
	s.bridgeMu.Lock()
	defer s.bridgeMu.Unlock()
	if err := s.mutate(func(record *SessionContextRecord) error {
		stack, err := storecontext.AppendCompact(record.CompactStack, frame)
		if err != nil {
			return err
		}
		record.CompactStack = stack
		return nil
	}); err != nil {
		return err
	}
	// 桥接与持久化都在 s.mu 之外：写盘不再阻塞本会话的纯读（SystemPrompt/
	// Snapshot/审计快照等）。
	s.bridgeCompactFrame(frame)
	return s.Persist(context.Background())
}

// bridgeCompactFrame 把运行期压缩帧写进 compact 通道（S19：单源写入；
// 失败不阻断内存/持久流程——compact.jsonl 可重建、下次提交重试）。
func (s *SessionContextStore) bridgeCompactFrame(frame CompactFrame) {
	if s == nil || s.router == nil || s.sessionID == "" {
		return
	}
	if ok, err := s.router.CommitCompactFrameWorkspace(s.workspace(), s.sessionID, frame); err == nil && ok {
		_, _ = s.router.RetentionAdvisoryWorkspace(s.workspace(), s.sessionID)
	}
}

// mutate 在 s.mu 内执行栈操作，**不落盘**：锁内只允许改内存（§2.8——落盘
// 放进来会让一次写盘阻塞本会话的全部纯读）。
func (s *SessionContextStore) mutate(apply func(*SessionContextRecord) error) error {
	if s == nil || s.router == nil || s.sessionID == "" {
		return fmt.Errorf("session context: router or session ID is unavailable")
	}
	s.mu.Lock()
	err := apply(&s.record)
	if err == nil {
		// 本次进程已持有内存态：v8 的内存字段（SkillStack 等）不得被随后
		// 的 Load 从已剥离的 blob 覆盖。
		s.loaded = true
	}
	s.mu.Unlock()
	return err
}

// update 在加锁下执行栈操作并持久化 state blob。
func (s *SessionContextStore) update(apply func(*SessionContextRecord) error) error {
	if err := s.mutate(apply); err != nil {
		return err
	}
	return s.Persist(context.Background())
}

func cloneSessionContextRecord(record SessionContextRecord) SessionContextRecord {
	clone := record
	clone.PlanStack = append([]PlanFrame(nil), record.PlanStack...)
	clone.TaskStack = append([]TaskFrame(nil), record.TaskStack...)
	clone.SkillStack = append([]SkillFrame(nil), record.SkillStack...)
	clone.CompactStack = cloneCompactFrames(record.CompactStack)
	clone.GoalStack = cloneGoalFrames(record.GoalStack)
	clone.GoalAudit = cloneGoalAudit(record.GoalAudit)
	return clone
}

func cloneCompactFrames(frames []CompactFrame) []CompactFrame {
	if len(frames) == 0 {
		return nil
	}
	out := make([]CompactFrame, len(frames))
	for index := range frames {
		out[index] = frames[index]
		out[index].Evidence = append([]EvidenceRef(nil), frames[index].Evidence...)
	}
	return out
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
