// Package workunit 是「一个工作单元的现场与会话」的生命周期契约——job / subagent /
// teammate 三层共用一份，上层持下层、只写自己的增量。
//
// 层链（只增不减，与目标正文同一口径）：
//
//	job       —— **基线，不实现 Unit**：纯后台运行 + 结果回传，没有现场、没有持久会话。
//	             它不是"少一份现场的工作单元"，它根本不是工作单元（作业面只被 teammate 的
//	             Reclaim 需要，端口见 Jobs）。
//	subagent  —— 第一份 Unit 实现：worktree 现场 + 一件活自己的异步会话 + 存储与合并纪律。
//	teammate  —— 增：听 leader 调度 + 装配（plugin / 系统提示词 / skill 前缀复用）。它**不另写
//	             一套执行面**——每件活都是一个 subagent 单元（独立会话 + 独立现场），team 只托管
//	             这些单元的生命周期（team_close 才算真正结束与删除，见 AtTeamClose）。
//
// **一个 Unit = 一件事 = 一份现场 + 一条会话**。粒度差异不靠分支表达：teammate 的角色级现场就是
// teammate 单元自己那一份，Work Item 级的现场属于那个 Work Item 的 subagent 单元。于是 `Reclaim`
// 永远只拆"我自己这一份"，实现里不会出现"该拆角色级还是 item 级"的判断。
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

import (
	"context"

	"github.com/RedHuang-0622/Seele/jobs"
)

// Kind 标记一个工作单元属于哪一层。它是描述性的（日志、审计、看板，以及恢复说明的前缀族
// 见 RecoveryNote），**不是**实现里的分支判据：实现若出现 `if kind == KindTeammate` 这类
// 判断，说明契约没抽对。注意 `KindJob` 是**基线标注**：job 层不实现 Unit（见包注释）。
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
// 命名契约（唯一事实，不许各处再拼一次）：现场 nodeID 在 subagent 层是节点 id，在 teammate
// 层是 `<role>-<itemID>`；指派名一律 `seelex/<nodeID>`，换算只有一处（teamwork 的
// workItemNodeID 同口径）。`TeamID` / `WorkItem` 是**归属标注**（审计与看板要读），不是
// "该拆哪一级现场"的判据——一个 Unit 只有它自己这一份现场。
type Scene struct {
	Kind      Kind   `json:"kind"`
	NodeID    string `json:"node_id"`
	Worktree  string `json:"worktree,omitempty"`   // 指派名 seelex/<nodeID>；空 = 这件事没有独立现场（降级共享工作区）
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
//
// **说明非空**是合同的一部分：非落定的每一类都必须带上**给人看的处置办法**（现场在哪、
// 下一步谁做什么），因为那一条正是 leader/用户唯一看得见的收尾信息。分类器
// （ClassifyFinish）负责把"原因 + 处置办法"拼齐，实现不许把它清空或替换成裸错误。
type Outcome struct {
	Kind   OutcomeKind `json:"kind"`
	Notice string      `json:"notice,omitempty"`
}

// Unit 是一个工作单元的生命周期。subagent 是第一份实现（job 层不实现它，见包注释）。
//
// 方法语义（实现必须守，见包注释的不变式）：
//
//	Begin        —— 建现场与会话；幂等。
//	Finish       —— 这一轮怎么结束：分类 + 合并 + 回执；不拆现场。幂等（已经收过尾 ⇒
//	                零值结论，不重复合并、不重复回执）；`mergeErr` 非空即采信（调用方
//	                已经合过一次），只有为空时才由实现去合并一次。
//	Reclaim      —— 拆现场 + 清会话 + 回收作业；幂等；唯一入口。
//	Recover      —— 重启回灌：先认领现场（必须在 Prune 之前），再回灌会话并注入恢复说明；
//	                读回粒度是**会话级**（按会话路径取回该会话全部单元，本单元那一份才是
//	                结论）；记录说在跑而本进程已无执行面的，记进 Resume.Interrupted
//	                （见 session.go）。
//	FinishPolicy —— 什么时候回收：三层之间**唯一**允许出现的差异点，因此它是接口的一部分，
//	                不再是各实现自造的一个可选方法（此前每层各写一遍类型断言去够它，
//	                断言就是"合同没抽对"的证据）。
//
// 一个实现只管**自己这一份**现场与会话（粒度见包注释）。
type Unit interface {
	Kind() Kind
	Begin(ctx context.Context) (Scene, error)
	Finish(ctx context.Context, result Result, mergeErr error) (Outcome, error)
	Reclaim(ctx context.Context) error
	Recover(ctx context.Context) (Resume, error)
	FinishPolicy() FinishPolicy
}

// Jobs 是"作业面"的窄端口：只取"回收一个作用域下的全部作业"这一件事。契约里只有 teammate 的
// Reclaim 需要它——一件活跑完就结束，不需要回收作业；整队收口才要把名下仍在飞的作业收回来。
//
// `jobs.Manager`（Seele/jobs，经 vendor）**结构上**就满足它——与 SessionLedger 同一手法：不写
// 适配器，也不另立第二份作业模型。
type Jobs interface {
	Reclaim(ctx context.Context, scope jobs.Scope) error
}

// 编译期钉住"复用"：Seele 的作业管理器接口一旦与这里漂移，先红。
var _ Jobs = (jobs.Manager)(nil)

// FinishPolicy 决定"什么时候回收"——现场与会话的**生命周期**上唯一允许出现的差异点。
//
// 它不是一条统一事件流（那是过度设计）：结束事实各有各的载体（作业回执 / 子代理节点记录 /
// 团队账本），这条策略只回答一个问题——`Finish` 落定之后，现场与会话**现在**回收，还是留给
// team_close 统一回收。
//
// 层与层之间其余的差别（有没有现场、装配多少、谁来调度）是**内容**上的差别，不是策略上的分支：
// 实现里出现按层判断的 `if`，说明契约没抽对。
type FinishPolicy interface {
	// AfterFinish 在一次 Finish 落定之后被调用恰好一次。
	AfterFinish(ctx context.Context, unit Unit) error
}

// Immediate 是"现场临时"的层的策略（subagent 即此：派出即建、收尾即清）。
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
