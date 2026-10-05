// Package workunit 是「一个工作单元的现场与会话」的生命周期契约——job / subagent /
// teammate 三层共用一份，上层持下层、只写自己的增量。
//
// 层链（只增不减，与目标正文同一口径）：
//
//	job       —— 纯后台运行 + 结果回传；**无现场、无持久会话**（Scene 零值）。
//	subagent  —— 增：worktree 现场 + 一件活自己的异步会话 + 存储与自己的合并纪律。
//	teammate  —— 增：听 leader 调度 + 装配（plugin / 系统提示词 / skill 前缀复用）；
//	             现场与会话的生命周期**归 team**：team_close 才算真正结束与删除。
//
// 为什么要有这份契约：同一件事（建现场 → 收尾 → 回收 → 恢复）此前在三层各手写了一遍，
// 第三遍抄漏了。F2（派发不建现场）、F3（释放拿角色名查注册表 = 空操作）、F4（恢复漏了
// 团队现场，紧随的 Prune 把"干净但还没合并"的现场连分支一起删）、F5（重派落到残留清理
// 分支，worktree remove --force 把未提交产出砸掉）——四条全是同一份生命周期被复制三遍
// 之后的**漂移**，不是四个独立的错。契约把"同一件事"钉成一份实现，并把三层之间**唯一**
// 允许出现的差异显式化成 FinishPolicy（什么时候回收）。
//
// 刻意**不**装什么（装了就变成过度设计）：
//
//   - Run / 回合外壳 / roundGate：三层的"怎么跑"本来就不同（teammate 有 plugin 装配
//     这一特殊存在，结束方式也不同），强行收敛只会长出一堆空实现；
//   - leader 调度、消息队列、系统提示词/plugin 装配：那是执行面别的东西，不是生命周期；
//   - 统一的"结束事件流"：三层的结束事实各有各的载体（作业回执 / 子代理节点记录 /
//     团队账本），再立一条统一事件流就是第二份进度真相。
//
// 不变式（与哪一层无关，每个实现都要守）：
//
//   - Begin 幂等：同一 NodeID 重复 Begin 返回同一现场；**不得**对已存在的现场动手
//     （不 `worktree remove --force`、不 `branch -D`、不丢未提交产出——现场是人的资产）。
//   - Finish 只回答"这一轮怎么结束的"：分类（ClassifyFinish）+ 合并 + 回执；**不**拆现场。
//   - Reclaim 是**唯一**"拆现场 + 清会话 + 回收作业"的动作，且幂等；脏现场显式报错，
//     绝不静默丢。
//   - Recover 必须在 `Prune` 之前完成认领（顺序是判据的一部分，见
//     worktree.WorktreeManager.Prune）；会话回灌复用 sessionstore.NodeSessionStore 的形状
//     （记录携带 History / StagesJSON / Worktree）。
package workunit

import "context"

// Kind 标记一个工作单元属于哪一层。它是描述性的（给日志、审计、看板用），
// **不是**实现里的分支判据：实现若出现 `if kind == KindTeammate` 这类判断，
// 说明契约没抽对。
type Kind string

const (
	// KindJob 是 job 层：只有后台运行与结果回传，没有现场、没有持久会话。
	KindJob Kind = "job"
	// KindSubagent 是 subagent 层：一件活一套 worktree 现场 + 一个自己的异步会话，
	// 跑完即清（现场临时）。
	KindSubagent Kind = "subagent"
	// KindTeammate 是 teammate 层：在 subagent 之上增装配与 leader 调度；现场与会话
	// 归 team 托管，team_close 才真正结束与删除。
	KindTeammate Kind = "teammate"
)

// Scene 是一个工作单元的现场：worktree 指派名 + 会话号 + 归属。
//
// 命名契约（唯一事实，不许各处再拼一次）：现场 nodeID 在 subagent 层是节点 id，
// 在 teammate 层是 `<role>-<itemID>`（角色级现场是 `<role>`）；指派名一律
// `seelex/<nodeID>`，换算只有一处（teamwork 的 workItemNodeID 同口径）。
type Scene struct {
	Kind      Kind   `json:"kind"`
	NodeID    string `json:"node_id"`
	Worktree  string `json:"worktree,omitempty"`   // 指派名 seelex/<nodeID>；空 = 无现场（job 层）
	SessionID string `json:"session_id,omitempty"` // 这件活自己的会话
	TeamID    string `json:"team_id,omitempty"`    // 归属团队（teammate 层）
	WorkItem  string `json:"work_item,omitempty"`  // 归属工作项（角色级现场留空）
}

// Result 是一轮工作**自身**的结果（不是收尾结果）：跑完给结论，跑挂给错误。
type Result struct {
	Summary string // 有界结论（回执/尾插用的那一行）
	Err     error  // 这一轮本身的失败；nil = 跑完
}

// OutcomeKind 是收尾分类的词汇表（三层唯一一份）。四类的区别是**后果**不同：
// 落定可以验收；未提交与挡路只是"这次没合进去"，现场保留、结论照常交付；
// 判死才是可重派/待人工处置。
type OutcomeKind string

const (
	// OutcomeSettled：跑完且改动已合进去 —— 待验收。
	OutcomeSettled OutcomeKind = "settled"
	// OutcomeUncommitted：现场有未提交改动（收尾协议未执行）—— 不改判死
	// （判死会让一份已完成的产出连现场一起留在没人看的角落）。
	OutcomeUncommitted OutcomeKind = "uncommitted"
	// OutcomeMergeBlocked：主工作区的在途改动挡住了本次合并 —— 同上，处置动作是
	// "先让主工作区干净，再重试合并"。
	OutcomeMergeBlocked OutcomeKind = "merge_blocked"
	// OutcomeFailed：判死 —— 可重派、可人工处置（现场与记忆都留着）。
	OutcomeFailed OutcomeKind = "failed"
)

// Outcome 是一次收尾的结论：分类 + 给人看的说明（有界，进回执与看板）。
type Outcome struct {
	Kind   OutcomeKind `json:"kind"`
	Notice string      `json:"notice,omitempty"`
}

// Unit 是一个工作单元的生命周期。三层各实现一份；上层持有下层，只写自己的增量。
//
// 方法语义（实现必须守，见包注释的不变式）：
//
//	Begin   —— 建现场与会话；幂等。
//	Finish  —— 这一轮怎么结束：分类 + 合并 + 回执；不拆现场。
//	Reclaim —— 拆现场 + 清会话 + 回收作业；幂等；唯一入口。
//	// Recover —— 重启回灌：先认领现场（必须在 Prune 之前），再回灌会话并注入恢复说明；
//	            记录说在跑而本进程已无执行面的，记进 Resume.Interrupted（见 session.go）。
type Unit interface {
	Kind() Kind
	Begin(ctx context.Context) (Scene, error)
	Finish(ctx context.Context, result Result, mergeErr error) (Outcome, error)
	Reclaim(ctx context.Context) error
	Recover(ctx context.Context) (Resume, error)
}

// FinishPolicy 决定"什么时候回收"——三层之间**唯一**允许出现的差异点。
//
// 它不是一条统一事件流（那是过度设计）：三层的结束事实各有各的载体，这条策略只回答
// 一个问题——`Finish` 落定之后，现场与会话**现在**回收，还是留给 team_close 统一回收。
type FinishPolicy interface {
	// AfterFinish 在一次 Finish 落定之后被调用恰好一次。
	AfterFinish(ctx context.Context, unit Unit) error
}

// Immediate 是 job / subagent 的策略：这一轮落定之后立刻回收现场与会话
// （subagent 现场是临时的：派出即建、收尾即清）。
type Immediate struct{}

// AfterFinish 立刻回收。Reclaim 幂等，因此重复调用安全。
func (Immediate) AfterFinish(ctx context.Context, unit Unit) error {
	return unit.Reclaim(ctx)
}

// AtTeamClose 是 teammate 的策略：Finish 之后**不动**现场与会话，回收留给 team_close
// 扫账本统一做（回收唯一入口 = team_close；见 Close 的 closeStepsLocked + releaseAllItems）。
//
// 这里刻意什么都不做，而不是"延迟到自己人走"：teammate 的现场与会话归 team 托管，
// 单件活跑完就拆现场会让 leader 的审查、合并失败的人工处置、以及收口的统一释放
// 同时失去依据（旧口径"验收即释放"的抢跑就是这么来的）。
type AtTeamClose struct{}

// AfterFinish 不做任何事：回收由 team_close 统一触发。
func (AtTeamClose) AfterFinish(context.Context, Unit) error { return nil }
