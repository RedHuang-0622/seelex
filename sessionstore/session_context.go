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
const SessionContextSchemaVersion = 1

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
}

// SkillFrame 是 SkillStack 的一帧（now using skill = 栈顶）。
type SkillFrame struct {
	SkillID string `json:"skill_id"`
	Name    string `json:"name"`
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
}

// SessionContextRecord 是会话级上下文状态（state blob）：
// SystemPrompt（永不压缩）+ Plan/Task/Skill/Compact 四个使用栈。
type SessionContextRecord struct {
	SchemaVersion int            `json:"schema_version"`
	SystemPrompt  string         `json:"system_prompt"`
	PlanStack     []PlanFrame    `json:"plan_stack"`
	TaskStack     []TaskFrame    `json:"task_stack"`
	SkillStack    []SkillFrame   `json:"skill_stack"`
	CompactStack  []CompactFrame `json:"compact_stack"`
}

// SessionContextStore 读写 Router 的独立 context 通道（WriteContextState/
// ReadContextState，与 SessionRecord 的 state blob 物理隔离），带内存缓存：
// 首次 Load 后栈操作只改内存，Persist 时才落盘。
type SessionContextStore struct {
	router    *Router
	sessionID string

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
	if record.SchemaVersion != SessionContextSchemaVersion {
		return fmt.Errorf("session context: %q has unsupported schema version %d (want %d)",
			s.sessionID, record.SchemaVersion, SessionContextSchemaVersion)
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

// PushSkill 在 skill 激活时压栈。
func (s *SessionContextStore) PushSkill(frame SkillFrame) error {
	return s.update(func(record *SessionContextRecord) error {
		if frame.SkillID == "" {
			return fmt.Errorf("session context: skill frame requires skill_id")
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
	return s.update(func(record *SessionContextRecord) error {
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
		record.CompactStack = append(record.CompactStack, frame)
		return nil
	})
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
	return clone
}
