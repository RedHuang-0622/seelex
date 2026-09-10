package dto

import "time"

// 子代理中断恢复的只读投影（guI/headless 数据源）：
// subagent 是劳务派遣式 tool calling 能力，不进 AgentTeam 成员表；它的
// active 只活跃在表格，冷恢复走 tool-chain 中断语义（见
// docs/arch/a2a-agent-team-factory.md §5/§5.1）。

// SubagentRecoveryView 是单个子代理单元的恢复态投影。
type SubagentRecoveryView struct {
	// NodeID 是派发幂等键（= 节点会话 ID）。
	NodeID string `json:"node_id"`
	// SessionID 是子代理自己的会话 ID。
	SessionID string `json:"session_id,omitempty"`
	// Goal 是原始派发目标。
	Goal string `json:"goal,omitempty"`
	// Status 是持久化状态：queued | running | done | failed。
	Status string `json:"status"`
	// Active 表示单元未终结（派发已发生、结果未记录），需要重启续跑。
	Active bool `json:"active"`
	// ConclusionFound 表示父级事件库已有该节点的结论（幂等键已收敛）。
	ConclusionFound bool `json:"conclusion_found"`
	// Resumable 表示本轮冷恢复会重启该单元。
	Resumable bool `json:"resumable"`
	// Summary/Error 是残留记录里的结论摘要。
	Summary string `json:"summary,omitempty"`
	Error   string `json:"error,omitempty"`
	// WorktreePath 是残留的 worktree 现场（恢复时复用）。
	WorktreePath string `json:"worktree_path,omitempty"`
	// UpdatedAt 是残留记录最后更新时间。
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// SubagentForkSpec 是一次直接派发的子代理规格（headless/自动化入口；与模型
// 调用 fork_subagents 的 subagents[] 项同构）。
type SubagentForkSpec struct {
	// ID 是派发幂等键（= 节点会话 ID，重复派发复用同一结论槽位）。
	ID string `json:"id"`
	// Goal 是子代理目标（重建现场与同键重跑的依据）。
	Goal string `json:"goal"`
}

// SubagentResumeResult 是单个子代理单元的恢复结果。
type SubagentResumeResult struct {
	// NodeID 是派发幂等键。
	NodeID string `json:"node_id"`
	// Resumed 表示本轮真的重建现场并重新派发续跑。
	Resumed bool `json:"resumed"`
	// Skipped 表示本轮未重启（已终结、已在途或未定位到）。
	Skipped bool `json:"skipped"`
	// Steps 是实际执行的恢复模板步骤。
	Steps []string `json:"steps,omitempty"`
	// NoteRole 是恢复说明使用的 provider role（注入过则恒为 system）。
	NoteRole string `json:"note_role,omitempty"`
	// Summary 是恢复摘要。
	Summary string `json:"summary,omitempty"`
	// Error 是失败原因（非空即未收敛，可重试）。
	Error string `json:"error,omitempty"`
}

// SubagentResumeReport 是一轮子代理冷恢复的总报告。
type SubagentResumeReport struct {
	// SessionID 是被恢复的主会话 ID。
	SessionID string `json:"session_id"`
	// Located 是定位到的残留单元数。
	Located int `json:"located"`
	// Units 是逐单元结果。
	Units []SubagentResumeResult `json:"units,omitempty"`
	// Resumed/Skipped/Failed 是按键归类的单元 ID 列表。
	Resumed []string `json:"resumed,omitempty"`
	Skipped []string `json:"skipped,omitempty"`
	Failed  []string `json:"failed,omitempty"`
	// RecoveryNoteRole 断言恢复说明走的标准 provider role（恒为 system）。
	RecoveryNoteRole string `json:"recovery_note_role"`
	// StartedAt/FinishedAt 是本轮起止时间。
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}
