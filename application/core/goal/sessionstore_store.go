package goal

// sessionstore_store.go — goal 域第五栈的 sessionstore 适配。
//
// goal 栈（LIFO）随会话聊天记录同域持久化到 sessionstore 的
// SessionContextRecord.GoalStack（context 通道，按 (project, session) 隔离）。
// 语义边界（docs/2026-09-08-govern-loop/design.md §2.1 + 澄清）：
//
//  1. goal 栈只用于会话恢复与后续 goal 治理（对齐 plan/task 的会话级使用栈）；
//  2. goal 栈**不入模型上下文**：seelexctx 只渲染 Plan/Task/Skill/Compact 四栈，
//     本适配不向任何 prompt 装配器暴露 GoalStack；
//  3. 栈内 goal 不做前缀与记忆匹配；
//  4. 聊天记录中的 #goal 文本是普通转录内容，随上下文窗口/压缩一起被压缩，
//     与本栈无关。
//
// Controller 每次状态机变更经 Store.Save 全量写当前栈投影（收口弹栈后终态
// 帧从栈消失，History 审计仍是进程内态）；Restore 经 Controller.Reload 从
// GoalStackSnapshot 重建。

import (
	"context"
	"fmt"
	"time"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// ContextStateStore 实现 Store：把 goal.Controller 栈投影到会话级
// SessionContextStore 的 GoalStack（第五栈）。
type ContextStateStore struct {
	session *sessionstore.SessionContextStore
}

// NewContextStateStore 构造适配器。session 为 nil 时 Load/Save 返回
// ErrStoreUnavailable（未装配会话上下文存储的降级路径）。
func NewContextStateStore(session *sessionstore.SessionContextStore) *ContextStateStore {
	return &ContextStateStore{session: session}
}

// Load 实现 Store：从会话 GoalStack 读取当前栈（空栈返回空切片）。
func (s *ContextStateStore) Load(ctx context.Context) ([]*GoalRecord, error) {
	if s == nil || s.session == nil {
		return nil, ErrStoreUnavailable
	}
	if err := s.session.Load(ctx); err != nil {
		return nil, fmt.Errorf("%w: 加载会话 goal 栈: %v", ErrStoreUnavailable, err)
	}
	return recordsFromGoalFrames(s.session.GoalStackSnapshot()), nil
}

// Save 实现 Store：全量替换会话 GoalStack 并持久化。写入前先确保会话
// context 已 Load（避免未加载即覆盖磁盘上的既有 goal 栈）。
func (s *ContextStateStore) Save(ctx context.Context, records []*GoalRecord) error {
	if s == nil || s.session == nil {
		return ErrStoreUnavailable
	}
	if err := s.session.Load(ctx); err != nil {
		return fmt.Errorf("%w: 保存前加载会话 goal 栈: %v", ErrStoreUnavailable, err)
	}
	if err := s.session.ReplaceGoalStack(goalFramesFromRecords(records)); err != nil {
		return fmt.Errorf("%w: 保存会话 goal 栈: %v", ErrStoreUnavailable, err)
	}
	return nil
}

// goalFramesFromRecords 把 goal 域记录投影为 sessionstore 第五栈帧
// （深拷贝切片/映射；EnteredAt 由 CreatedAt 推导，fork 时间截断可用）。
func goalFramesFromRecords(records []*GoalRecord) []sessionstore.GoalFrame {
	if len(records) == 0 {
		return nil
	}
	out := make([]sessionstore.GoalFrame, 0, len(records))
	for _, record := range records {
		if record == nil {
			continue
		}
		frame := sessionstore.GoalFrame{
			GoalID:     record.ID,
			Title:      record.Title,
			Statement:  record.Statement,
			Acceptance: append([]string(nil), record.Acceptance...),
			OutOfScope: append([]string(nil), record.OutOfScope...),
			Status:     string(record.Status),
			Directives: append([]string(nil), record.Directives...),
			CreatedAt:  record.CreatedAt,
			UpdatedAt:  record.UpdatedAt,
			FinishedAt: record.FinishedAt,
			Budget: sessionstore.GoalBudget{
				MaxLoops:      record.Budget.MaxLoops,
				MaxTokens:     record.Budget.MaxTokens,
				TLTokensShare: record.Budget.TLTokensShare,
			},
		}
		for _, item := range record.Progress {
			frame.Progress = append(frame.Progress, sessionstore.GoalProgress{
				At: item.At, Kind: string(item.Kind), Content: item.Content,
			})
		}
		if record.Meta != nil {
			frame.Meta = make(map[string]string, len(record.Meta))
			for key, value := range record.Meta {
				frame.Meta[key] = value
			}
		}
		if record.CreatedAt > 0 {
			frame.EnteredAt = time.Unix(record.CreatedAt, 0)
		}
		out = append(out, frame)
	}
	return out
}

// recordsFromGoalFrames 把 sessionstore 第五栈帧还原为 goal 域记录
// （Reload 输入；Status 非法时保留原字符串，由 Controller.Reload 的
// 位置语义修正 active/paused）。
func recordsFromGoalFrames(frames []sessionstore.GoalFrame) []*GoalRecord {
	if len(frames) == 0 {
		return nil
	}
	out := make([]*GoalRecord, 0, len(frames))
	for _, frame := range frames {
		record := &GoalRecord{
			ID:         frame.GoalID,
			Title:      frame.Title,
			Statement:  frame.Statement,
			Acceptance: append([]string(nil), frame.Acceptance...),
			OutOfScope: append([]string(nil), frame.OutOfScope...),
			Status:     Status(frame.Status),
			Directives: append([]string(nil), frame.Directives...),
			CreatedAt:  frame.CreatedAt,
			UpdatedAt:  frame.UpdatedAt,
			FinishedAt: frame.FinishedAt,
			Budget: Budget{
				MaxLoops:      frame.Budget.MaxLoops,
				MaxTokens:     frame.Budget.MaxTokens,
				TLTokensShare: frame.Budget.TLTokensShare,
			},
		}
		for _, item := range frame.Progress {
			record.Progress = append(record.Progress, Progress{
				At: item.At, Kind: ProgressKind(item.Kind), Content: item.Content,
			})
		}
		if len(frame.Meta) > 0 {
			record.Meta = make(map[string]string, len(frame.Meta))
			for key, value := range frame.Meta {
				record.Meta[key] = value
			}
		}
		out = append(out, record)
	}
	return out
}
