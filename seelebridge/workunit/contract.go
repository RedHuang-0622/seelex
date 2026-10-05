// Package workunit 是「一个工作单元的现场与会话」的生命周期契约——job / subagent /
// teammate 三层共用一份，上层持下层、只写自己的增量。
//
// 层链（只增不减，与目标正文同一口径）：
//
//	job       —— **基线，不实现 Unit**：纯后台运行 + 结果回传，没有现场、没有持久会话。
//	             它不是"少一份现场的工作单元"，它根本不是工作单元（作业面只被 teammate 的
//	             Reclaim 需要，端口见 Jobs）。
//	subagent  —— 第一份**读数**（Unit）+ 在唯一实现上注册：worktree 现场 + 一件活自己的
//	             异步会话 + 存储与合并纪律。
//	teammate  —— 增：听 leader 调度 + 装配（plugin / 系统提示词 / skill 前缀复用）。它**不另写
//	             一套执行面**——每件活都是一个 subagent 单元（独立会话 + 独立现场），team 只托管
//	             这些单元的生命周期（team_close 才算真正结束与删除，见 AtTeamClose）。
//
// **一个 Unit = 一件事 = 一份现场 + 一条会话**。粒度差异不靠分支表达：teammate 的角色级现场就是
// teammate 单元自己那一份，Work Item 级的现场属于那个 Work Item 的 subagent 单元。于是 `Reclaim`
// 永远只拆"我自己这一份"，实现里不会出现"该拆角色级还是 item 级"的判断。
//
// **接口先行（依赖倒置）**：契约里有两个接口，方向是**反的**——
//
//	Lifecycle —— 父实现的契约面（**一份实现**）：Begin / Finish / Reclaim / Recover /
//	             AlreadySettled / Notice。调用方依赖它，不依赖具体实现。
//	Unit      —— 层的**读数**：Kind / ID / SessionPath / Policy / Owns。只有身份与策略，
//	             没有逻辑，也不持有任何端口。
//
// 装配：实现包（`seelebridge` 的 lifecycleHost）实现 `Lifecycle`，端口字段**不导出**；两个
// 注册点（subagent / teammate）在 `new` 时注入它，**只持有 `workunit.Lifecycle` + 自己的
// `Unit` 读数**，方法体一律转发。将来两层真要不同实现 = 再写一个 `Lifecycle` 实现 + 改装配处
// 一行，不动调用面。本包谁都不 import 实现包（不着 `teamwork` / `worktree` / `session` 的类型
// 进签名的那个方向）：归属用本包自己的小结构 `Ownership` 表达。
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
//   - Begin 幂等：同一 ID 重复 Begin 返回同一现场；**不得**对已存在的现场动手
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

// Ownership 是一个工作单元的**归属读数**：这是**数据**，不是端口，也不带逻辑。
//
// 它存在的理由是依赖方向：teammate 侧的归属原先装在 `teamwork.WorkerRequest` 里，把它引进
// 契约包就形成了 `workunit → teamwork` 的依赖（而 teamwork 已经 import workunit，直接成环）。
// 契约因此自带这个小结构，实现包负责把 `WorkerRequest` 折进来（折算只有一处：
// `seelebridge` 的 teamUnitReadings.Owns）。
//
// 字段分两组：**谁的活、哪一件**（TeamID / ItemID / Role / Milestone）与**这一轮落在哪**
// （RoleSessionID / Goal / Worktree）。零值 = 没有团队归属（subagent 层）。全部字段都是
// 只读事实：实现不得在生命周期方法里回写它们（要回写的是现场与会话，那是 Lifecycle 的事）。
type Ownership struct {
	TeamID        string `json:"team_id,omitempty"`         // 归属团队；空 = 没有团队归属
	ItemID        string `json:"item_id,omitempty"`         // 归属工作项；空 = 角色级单元
	Role          string `json:"role,omitempty"`            // 谁在做（"要不要一份独立现场"的读数之一）
	Milestone     string `json:"milestone,omitempty"`       // 归属里程碑
	RoleSessionID string `json:"role_session_id,omitempty"` // 这一件活自己的会话（一件活一条）
	Goal          string `json:"goal,omitempty"`            // 这一轮的目标（恢复说明要读）
	Worktree      string `json:"worktree,omitempty"`        // 现场指派名的**显式**读数；空 = 由实现按命名约定派生一次
}

// Unit 是一个工作单元的**读数**：只有身份与策略，没有逻辑，也不持有任何端口。
//
// 它是**层的差异**的唯一表达处——层与层之间允许不同的东西只有这些读数：
//
//	Kind         —— 我是谁（描述性的：日志 / 审计 / 恢复说明前缀族，不是分支判据）；
//	ID           —— 我这一份现场的身份（subagent = 节点 id；teammate = `<role>-<itemID>`，
//	                角色级 = `<role>`）；
//	SessionPath  —— 我的会话在哪（转发时传进去的"自己的会话路径"；账本键由实现按归属解析）；
//	Policy       —— 我什么时候回收（契约里唯一的层间策略差异，见 FinishPolicy）；
//	Owns         —— 我归谁（团队 / 工作项 / 角色 / 这一轮的目标）。
//
// 方法体里**不许**出现 store / worktree / jobs / 账本的写入——写是 Lifecycle 的事。
type Unit interface {
	Kind() Kind
	ID() string
	SessionPath() string
	Policy() FinishPolicy
	Owns() Ownership
}

// Lifecycle 是一件活的生命周期的**契约面**（父实现的接口）：调用方依赖它，不依赖具体实现。
//
// 这一份实现**只有一份**（`seelebridge` 的 lifecycleHost），两层在它上面注册：注册点只持有
// `Lifecycle` + 自己的 `Unit` 读数，方法体一律转发。将来两层真要不同实现 = 再写一个
// `Lifecycle` 实现 + 改装配处一行。
//
// 方法语义（实现必须守，见包注释的不变式）：
//
//	Begin         —— 建现场与会话；幂等。
//	Finish        —— 这一轮怎么结束：分类 + 合并 + 回执；不拆现场。幂等（已经收过尾 ⇒
//	                 零值结论，不重复合并、不重复回执）；`mergeErr` 非空即采信（调用方
//	                 已经合过一次），只有为空时才由实现去合并一次。
//	Reclaim       —— 拆现场 + 清会话 + 回收作业；幂等；唯一入口。**两个策略调的都是它**，
//	                 差别只在**调用点**：`Immediate` 在 Finish 落定时就调，`AtTeamClose`
//	                 留到整队收口调（见 FinishPolicy）。
//	Recover       —— 重启回灌：先认领现场（必须在 Prune 之前），再回灌会话并注入恢复说明；
//	                 读回粒度是**会话级**（按会话路径取回该会话全部单元，本单元那一份才是
//	                 结论）；记录说在跑而本进程已无执行面的，记进 Resume.Interrupted
//	                 （见 session.go）。
//	AlreadySettled—— "这件事已经收过尾了吗"：**重入**的唯一判据（两层共用一份）。
//	Notice        —— 把 Outcome 折成给人看的一行。
type Lifecycle interface {
	Begin(ctx context.Context, u Unit) (Scene, error)
	Finish(ctx context.Context, u Unit, result Result, mergeErr error) (Outcome, error)
	Reclaim(ctx context.Context, u Unit) error
	Recover(ctx context.Context, u Unit) (Resume, error)
	AlreadySettled(ctx context.Context, u Unit) (bool, error)
	Notice(outcome Outcome) string
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
// **策略不是实现，只是调用点**：两个策略调的是**同一个**析构函数（`Lifecycle.Reclaim`），
// 差别只在什么时候调（subagent 收尾当场调；teammate 留到整队收口）。因此 `AfterFinish` 拿得到
// `Lifecycle`——它要调的正是那一个实现，不是自己再写一份拆现场。
//
// 层与层之间其余的差别（有没有现场、装配多少、谁来调度）是**内容**上的差别，不是策略上的分支：
// 实现里出现按层判断的 `if`，说明契约没抽对。
type FinishPolicy interface {
	// AfterFinish 在一次 Finish 落定之后被调用恰好一次。
	AfterFinish(ctx context.Context, lifecycle Lifecycle, u Unit) error
}

// Immediate 是"现场临时"的层的策略（subagent 即此：派出即建、收尾即清）。
type Immediate struct{}

// AfterFinish 立刻回收：调的就是**同一个**析构函数（`Lifecycle.Reclaim`）。
// Reclaim 幂等，因此重复调用安全。
func (Immediate) AfterFinish(ctx context.Context, lifecycle Lifecycle, u Unit) error {
	return lifecycle.Reclaim(ctx, u)
}

// AtTeamClose 是 teammate 的策略：Finish 之后**不动**现场与会话，回收留给 team_close
// 扫账本统一做（回收唯一入口 = team_close；见 Close 的 closeStepsLocked + releaseAllItems）。
//
// 这里刻意什么都不做，而不是"延迟到自己人走"：teammate 的现场与会话归 team 托管，
// 单件活跑完就拆现场会让 leader 的审查、合并失败的人工处置、以及收口的统一释放
// 同时失去依据（旧口径"验收即释放"的抢跑就是这么来的）。
//
// 它**不是**"另一份析构"：整队收口调的还是 `Lifecycle.Reclaim` 落到的那一份实现
// （teammate 侧 = `Coordinator.reclaimStepsLocked`，与 `Lifecycle.Reclaim` 共用）。
type AtTeamClose struct{}

// AfterFinish 不做任何事：回收由 team_close 统一触发（调用点不同，析构函数是同一个）。
func (AtTeamClose) AfterFinish(context.Context, Lifecycle, Unit) error { return nil }
