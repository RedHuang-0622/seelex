package switching

// async.go — 后台作业终态触发对话的**判据**：触发状态词表、幂等账的按表键前缀、
// 以及 teammate 作业表的两个窄端口读取。
//
// 未搬的部分（登记）：两条**用户行正文**（`asyncCompletionPrompt` / `teamworkCompletionPrompt`）
// 仍留在宿主——它们要用"kind → 展示词"的词表，而那张表正被另一个在途切片搬去
// `middle/view/worktable`（`worktable.PromptKind` 尚未提交）；等它落地后再搬，避免制造第二份词表。
//
// 落点沿革：2026-10-17 随 **M5-R6-async-completion** 接收者转移从
// `application/assembly/async_completion.go` 归位（plan §2.4 的 `async_completion.go` → `middle/switch/`）；
// 驱动链（全量扫描登记表、会话空闲判定、起回合）仍留在宿主——它要读会话域与视图锁。
//
// 用户口径（2026-10-01，随判据一起搬走）：后台作业（bash_bg / read_batch / subagent）落到**终态**时，
// 为它所属会话起一个回合，让主代理自己去取回结果并继续。**done 与 failed 都触发**；killed 不触发
// （那不是"作业有了结果"，是被终止）；running 不是终态。
//
// 铁律边界（docs/arch/teamwork-leader-worker-architecture.md §6.1「绝不唤醒忙会话」）：忙会话不唤醒，
// 它的回合边界本来就会从打点块看到完成行（`work_table_async.go` 的完成行回填）——两者互补。

import "github.com/RedHuang-0622/seelex/contract/dto"

// TeamworkTriggeredPrefix 是幂等账的**按表**键前缀。
//
// 句柄自带命名空间（`bt-` / `bs-` / `bteam-`，见 `dto.JobNamespace`），字面量撞车在语法上已不可能；
// 这个前缀仍然留着，因为幂等账的口径是"哪张表的哪一条"，不是"哪一串字符"——靠字面量区分本来就是
// 当年那张表各铸 `a<seq>` 时的将就（2026-10-15 审计 D1）。
const TeamworkTriggeredPrefix = "teamwork:"

// AsyncCompletionTriggers 是触发口径认的两个终态：**转调契约的词表**（`dto.AsyncState*`，字面量只有
// 那一处），这里只是给触发口径起名，不新增第二套状态机。killed / running 不触发。
func AsyncCompletionTriggers(state dto.AsyncState) bool {
	return state == dto.AsyncStateDone || state == dto.AsyncStateFailed
}

// teamworkJobPort 是 teammate 作业表的**窄可选能力面**（结构满足 `contract.TeamworkJobCompletion`）。
// 参数取 `runtime any` 而不是 `contract.RuntimePort`：本包只用到这一格能力，用窄断言比声明整个
// RuntimePort 更诚实（宿主传进来的就是 `Deps.Runtime`，断言不上即"这条链不存在"）。
type teamworkJobPort interface {
	TeamworkJobCompletions() []dto.TeamworkJobCompletionRecord
	TeamworkJobEvents() <-chan struct{}
}

// TeamworkJobCompletions 读窄可选端口（未装配 teamwork 的宿主 = nil）。
func TeamworkJobCompletions(runtime any) []dto.TeamworkJobCompletionRecord {
	if runtime == nil {
		return nil
	}
	completion, ok := runtime.(teamworkJobPort)
	if !ok {
		return nil
	}
	return completion.TeamworkJobCompletions()
}

// TeamworkJobEvents 取 teammate 作业表的变化信号口（未装配 = nil；nil 通道在 select 里等于"这条链不存在"）。
func TeamworkJobEvents(runtime any) <-chan struct{} {
	if runtime == nil {
		return nil
	}
	completion, ok := runtime.(teamworkJobPort)
	if !ok {
		return nil
	}
	return completion.TeamworkJobEvents()
}
