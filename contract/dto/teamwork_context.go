package dto

// teamwork_context.go — 团队**成员工作上下文**的只读投影（`team_context` 工具的下发形状）。
//
// 生态位：团队看板（TeamworkBoardView）回答"这支团队是什么形状"；本形状回答"**某个人此刻
// 在干什么**"——在编条目 + 它名下的作业行 +（可选）作业正文。两者是同一份事实的两种分辨率，
// 都只有后端 → 前端的单向投影（前端没有写入口）。
//
// 正文为什么**默认关**：作业正文是消费式读法的对象（jobs_manage op=fetch 会推进游标，
// 取尽即销项）。这里走**非消费**读法（jobs.Manager.Peek：读增量、不推进游标、不销项），
// 且默认不取正文——"看一眼成员在干什么"不该顺手把别人还没读的内容变成"已读"。

// TeamworkContextView 是 team_context 的一次回执（整队一份，成员逐条）。
type TeamworkContextView struct {
	TeamID  string `json:"team_id,omitempty"`
	Version int    `json:"version,omitempty"`
	// Closed 是整队**收口**事实（计划侧权威，U3 裁决）。收口之后成员的作业已被回收，
	// 于是各成员多半只剩"空闲"——这一位让调用方分得清"收口造成的空"与"从没派过活"。
	Closed  bool                        `json:"closed,omitempty"`
	Members []TeamworkMemberContextView `json:"members,omitempty"`
}

// TeamworkMemberContextView 是一个成员的工作上下文：在编条目 + 作业行 +（可选）正文。
type TeamworkMemberContextView struct {
	// ── 在编条目（计划侧，权威） ──────────────────────────────────────────
	Role          string `json:"role"`
	RoleSessionID string `json:"role_session_id,omitempty"`
	Worktree      string `json:"worktree,omitempty"`
	ToolsPolicy   string `json:"tools_policy,omitempty"`
	// Milestone 是这位成员**当前归属的里程碑**（角色在其工作项里首次出现的里程碑；
	// 顺序的唯一事实是 plan.milestones[].depends_on——屏障）。
	Milestone string `json:"milestone,omitempty"`

	// ── 作业行（作业表侧，内存态） ────────────────────────────────────────
	//
	// Handle 是这位成员**最新**的作业句柄；空 = 它名下没有在册作业。
	Handle   string `json:"handle,omitempty"`
	State    string `json:"state,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
	Bytes    int64  `json:"bytes,omitempty"`
	Lines    int    `json:"lines,omitempty"`
	Running  bool   `json:"running,omitempty"`
	// Idle 是"此刻没有在跑的作业"的**显式化**：前端据此写"空闲"，而不是靠 handle 为空
	// 反推（反推分不清"没派过活"与"派过、已终态"）。
	Idle bool `json:"idle,omitempty"`
	// Degraded 是作业行的降级标记（句柄不在册 / 正文不可读）。
	Degraded bool   `json:"degraded,omitempty"`
	Summary  string `json:"summary,omitempty"`
	// Evicted 是"行不在册、但产品自有正文仍在"的显式标记：作业表是内存态、会被框架
	// prune 逐出（框架没有 pin 概念），而正文文件归产品、活到收口。置位时 Body 来自
	// **产品文件**（按角色名回读），不是按句柄的非消费读法——调用方据此分得清两种来源。
	Evicted bool `json:"evicted,omitempty"`

	// ── 正文（可选；非消费读法 + 逐成员有界裁剪） ─────────────────────────
	Body          string `json:"body,omitempty"`
	BodyBytes     int    `json:"body_bytes,omitempty"`
	BodyTruncated bool   `json:"body_truncated,omitempty"`
}
