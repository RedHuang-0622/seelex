package goal

// audit.go — goal 生命周期审计端口与条目。
//
// 审计与 GoalStack（活栈投影）正交：GoalStack 弹栈即删除、初始与终态都为空；
// GoalAudit 是**按会话隔离的 append-only 账本**，保留 begin/update/finish/
// abort/restore 收口记录（含 reason/result/出处），用于事后审计。审计不跨
// 会话共享：goal 的治理与收口记录留在其所属会话；若用户在其它会话完成了
// 该 goal，装配方可携带 SourceSession 出处写收口条目（仍写回原会话账本，
// 不写入其它会话）。

import (
	"context"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// MaxAuditTextRunes 是审计文本字段（reason/result/detail）的 rune 上限，
// 保证账本条目有界。
const MaxAuditTextRunes = 1200

// AuditAccount 是审计账本端口：由装配方注入（sessionstore GoalAudit 适配），
// Controller 每次状态机变更成功后在锁内追加一条审计。nil 时不审计。
type AuditAccount interface {
	AppendGoalAudit(ctx context.Context, entry AuditEntry) error
}

// AuditEntry 是一次 goal 状态迁移的审计条目（goal 域形态；落库时映射为
// sessionstore.GoalAuditEntry，Seq 由会话账本分配）。
type AuditEntry struct {
	Kind   EventKind
	GoalID string
	Title  string
	Status Status
	At     int64
	// SourceSession 非空表示该迁移发生在/被证实于其它会话（如用户在其它
	// 会话完成了该 goal，收口时由装配层携带出处）。
	SourceSession string
	Reason        string
	Result        string
	Detail        string
}

// normalized 返回文本字段有界的副本。
func (e AuditEntry) normalized() AuditEntry {
	e.Reason = truncateAuditText(e.Reason)
	e.Result = truncateAuditText(e.Result)
	e.Detail = truncateAuditText(e.Detail)
	return e
}

func truncateAuditText(value string) string {
	if len([]rune(value)) <= MaxAuditTextRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:MaxAuditTextRunes-1]) + "…"
}

// AppendGoalAudit 实现 AuditAccount：映射为 sessionstore GoalAuditEntry 后
// 追加到会话账本（写入前确保会话 context 已 Load，避免覆盖既有账本）。
func (s *ContextStateStore) AppendGoalAudit(ctx context.Context, entry AuditEntry) error {
	if s == nil || s.session == nil {
		return ErrStoreUnavailable
	}
	if err := s.session.Load(ctx); err != nil {
		return err
	}
	entry = entry.normalized()
	record := sessionstore.GoalAuditEntry{
		Kind:          string(entry.Kind),
		GoalID:        entry.GoalID,
		Title:         entry.Title,
		Status:        string(entry.Status),
		At:            entry.At,
		SourceSession: entry.SourceSession,
		Reason:        entry.Reason,
		Result:        entry.Result,
		Detail:        entry.Detail,
	}
	if err := s.session.AppendGoalAudit(record); err != nil {
		return err
	}
	return nil
}
