package dto

// teamwork_jobs.go — **teammate 作业（jobs.Manager 里的 worker 作业）**的终态只读投影。
//
// 生态位：团队看板（TeamworkBoardView）回答"这支团队是什么形状"；本形状回答"**哪一条
// teammate 作业刚跑到头了**"——它是"做完自动返回"那条链的输入（application 侧的空闲会话
// 触发回合用），不发给前端、不进快照。
//
// 为什么单开一份而不是复用 AsyncRunRecord：两者是**两张不同的表**。AsyncRunRecord 来自
// tools 的后台执行登记表（bash_bg / read_batch / subagent），句柄空间与取回工具都是那一套
// （job_manage）；teammate 作业活在 Seele 的 jobs.Manager 里，取回工具是 jobs_manage。
// 混成一份就会出现"自动返回的正文指着一个取不到这条作业的工具"这种错误。
type TeamworkJobCompletionRecord struct {
	// Handle 是 jobs.Manager 的句柄（进程内、ab<seq> 从不复用）。
	Handle string
	// Kind 是作业类别（worker）。
	Kind string
	// State 是作业状态字面量（running|done|failed|killed，与 jobs 同源）。
	State string
	// ExitCode 只在终态有意义（-1 = 未跑完）。
	ExitCode int
	// SessionID 是**派发它的主会话**（leader 会话）：触发回合的落点。
	SessionID string
	// Role / WorkItem 是这一轮工作的归属（收口工具按工作项 id 收）。
	Role     string
	WorkItem string
	// Description 是作业行标题（派发时那句话）；Summary 是终态摘要（框架给的）。
	Description string
	Summary     string
	Bytes       int64
}
