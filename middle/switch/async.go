package switching

// async.go — 后台作业终态触发对话的**判据**：触发状态词表、两条触发正文（用户行文本）、
// 以及 teammate 作业表的两个窄端口读取。
//
// 落点沿革：2026-10-17 随 **M5-R6-async-completion** 接收者转移从
// `application/core/async_completion.go` 归位（plan §2.4 的 `async_completion.go` → `middle/switch/`）；
// 驱动链（全量扫描登记表、会话空闲判定、起回合）仍留在宿主——它要读会话域与视图锁。
//
// 用户口径（2026-10-01，随判据一起搬走）：后台作业（bash_bg / read_batch / subagent）落到**终态**时，
// 为它所属会话起一个回合，让主代理自己去取回结果并继续。**done 与 failed 都触发**；killed 不触发
// （那不是"作业有了结果"，是被终止）；running 不是终态。
//
// 铁律边界（docs/arch/teamwork-leader-worker-architecture.md §6.1「绝不唤醒忙会话」）：忙会话不唤醒，
// 它的回合边界本来就会从打点块看到完成行（`work_table_async.go` 的完成行回填）——两者互补。

import (
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/view/worktable"
)

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

// AsyncCompletionPrompt 组装触发回合的正文。
//
// 它是一条**用户行**（走 submit 的正常输入路径），因此必须自带上下文：用户会看到"为什么这里多了一轮"，
// 模型也需要知道去取哪一条。字段刻意收窄到 handle / kind / state / exit / 有界摘要 / 有界标题——
// 与打点块同一份口径（绝对路径、末行原文、时间戳都不进上下文，见 `dto.AsyncRunRecord` 的字段分层）。
//
// `evidenceChars` 是摘要的字数预算（进程级 `limits.evidence_chars`，由宿主取数传入——中间层不读配置）。
func AsyncCompletionPrompt(record dto.AsyncRunRecord, evidenceChars int) string {
	var body strings.Builder
	body.WriteString("后台作业已完成，请取回结果并继续：\n")
	fmt.Fprintf(&body, "- handle: %s（kind=%s）\n", record.Handle, worktable.PromptKind(record.Kind))
	fmt.Fprintf(&body, "- 状态: %s · exit=%d\n", record.State, record.ExitCode)
	if title := strings.TrimSpace(record.Description); title != "" {
		fmt.Fprintf(&body, "- 作业: %s\n", worktable.TruncateWorkEvidence(title, 200))
	}
	if summary := strings.TrimSpace(record.Summary); summary != "" {
		fmt.Fprintf(&body, "- 摘要: %s\n", worktable.TruncateWorkEvidence(summary, evidenceChars))
	}
	fmt.Fprintf(&body, "取回全文用 bg_manage(op=fetch, handle=%q)；确认不需要了用 bg_manage(op=done, handle=%q) 销项。",
		record.Handle, record.Handle)
	return body.String()
}

// TeamworkCompletionPrompt 组装 teammate 完成回执的正文。
//
// 一处**刻意不同**于后台作业：正文指向 `jobs_manage` 与 `team_context`，不是 `bg_manage`——
// 两张表的取回工具不是同一个，指错了模型就会去一个取不到这条作业的地方捞。字段刻意收窄：只给归属
// （teammate / 工作项）、句柄、状态、有界摘要——不给正文、不给日志路径（正文是 `team_context` /
// `jobs_manage` 的事，塞进来就是按轮数线性烧 token）。
func TeamworkCompletionPrompt(record dto.TeamworkJobCompletionRecord, evidenceChars int) string {
	var body strings.Builder
	body.WriteString("teammate 作业已完成，请收口：\n")
	role := strings.TrimSpace(record.Role)
	item := strings.TrimSpace(record.WorkItem)
	switch {
	case role != "" && item != "":
		fmt.Fprintf(&body, "- teammate: %s（工作项 %s）\n", role, item)
	case role != "":
		fmt.Fprintf(&body, "- teammate: %s\n", role)
	default:
		body.WriteString("- teammate: —\n")
	}
	fmt.Fprintf(&body, "- handle: %s（kind=%s）\n", record.Handle, worktable.PromptKind(record.Kind))
	fmt.Fprintf(&body, "- 状态: %s · exit=%d\n", record.State, record.ExitCode)
	if description := strings.TrimSpace(record.Description); description != "" {
		fmt.Fprintf(&body, "- 作业: %s\n", worktable.TruncateWorkEvidence(description, 200))
	}
	if summary := strings.TrimSpace(record.Summary); summary != "" {
		fmt.Fprintf(&body, "- 摘要: %s\n", worktable.TruncateWorkEvidence(summary, evidenceChars))
	}
	fmt.Fprintf(&body,
		"读结论用 team_context（非消费读，不推进游标）；产物正文用 jobs_manage(op=observe|fetch, handle=%q)；"+
			"复核完用 team_accept / team_fail 收口（accept 才会打开下游依赖闸门）。",
		record.Handle)
	return body.String()
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
