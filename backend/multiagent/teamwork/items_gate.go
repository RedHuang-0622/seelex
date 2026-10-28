package teamwork

import (
	"strings"

	"github.com/RedHuang-0622/Seele/jobs"
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
)

// 工作项的**卡点**（"这件事此刻卡在哪"）——唯一判据 + 唯一词表（2026-10-15 审计 D7/D8）。
//
// 为什么要有这一格：在这之前，leader 只能靠 `team_items` 的状态/依赖/绑定加 `team_context`、
// 再自己反推"卡在哪"（真实会话里同一批取回工具被轮询几十次）；而收口闸门的拒绝文案把四种
// 情形一起列出来，却不说是哪一种。两者的根因是同一个：**判据散在调用方的推理里**。
//
// 判据放在编排面（它同时知道闸门、计划与作业表），前端只显示。词表刻意写成
// `<前缀>:<中文> 形态`：前缀是机器可读的类别，中文是给人和模型看的一句话。
type ItemGate string

const (
	// ItemGateDone 已验收：无需动作。
	ItemGateDone ItemGate = "done:已验收"
	// ItemGateJobRunning 作业在跑：等回执（要停就 kill）。
	ItemGateJobRunning ItemGate = "job:running"
	// ItemGateJobInterrupted 状态说在跑、句柄却已作废（进程重启 / 已销项 / 被驱逐）：可重派。
	ItemGateJobInterrupted ItemGate = "job:句柄已作废（可重派）"
	// ItemGateMerge 尾插没能把改动合进去（未提交 / 主工作区挡路）：现场要人工处置。
	ItemGateMerge ItemGate = "merge:改动未合并待处置"
	// ItemGateReview 跑完且合并已落地：等 leader 裁决。
	ItemGateReview ItemGate = "review:等你裁决"
	// ItemGateFailed 判定不通过 / 执行失败：现场与记忆都留着，待人工处置或重派。
	ItemGateFailed ItemGate = "failed:待人工处置"
	// ItemGateBlocked 依赖没完成：闸门未开。
	ItemGateBlocked ItemGate = "gate:依赖未 done"
	// ItemGateReady 依赖已满足、尚未派发：可以派。
	ItemGateReady ItemGate = "ready:可派发"
)

// Next 返回卡点对应的**下一步动作**（可直接照做的一句话）。
//
// 它与卡点同一张表：说得出"卡在哪"就必须说得出"怎么做"，否则又是"拒绝 + 请你先做 X"里
// 那个不可达的 X（审计 D2/D8）。
func (g ItemGate) Next() string {
	switch g {
	case ItemGateDone:
		return "无需动作"
	case ItemGateJobRunning:
		return "等它的回执（尾插会先合并、再插回执）；要现在停掉就用 jobs_manage(op=kill, handle=…)"
	case ItemGateJobInterrupted:
		return "直接 team_dispatch 重派（复用同一会话号，上下文记忆还在）"
	case ItemGateMerge:
		return "在它的 worktree 里处置（补提交 / 解冲突 / 变基 / 合并），再 team_accept 销项"
	case ItemGateReview:
		return "看证据（team_context / git diff）后 team_accept 或 team_fail"
	case ItemGateFailed:
		return "人工处置现场后 team_accept 销项；要重做就 team_fail 后 team_dispatch 重派"
	case ItemGateBlocked:
		return "先完成它依赖的工作项（依赖 done 之后这件事才可派）"
	case ItemGateReady:
		return "可以 team_dispatch 派发"
	default:
		return ""
	}
}

// JobReading 是判据需要的**作业表读数**：这件事的句柄此刻还在不在册、是不是还在跑。
//
// 两个调用方（编排面的 Coordinator 与看板投影）拿到的原始形状不同（Seele 的 `jobs.Record`
// vs 探针快照），所以判据收口在这两个布尔上，而不是各自判一遍。
type JobReading struct {
	// Alive = 句柄在本进程的作业表里还查得到。
	Alive bool
	// Running = 那个作业此刻还在跑（Alive=false 时必然为 false）。
	Running bool
}

// ItemGateOf 是"这件工作项此刻卡在哪"的**唯一判据**。
//
// 优先级：终态 > 在飞 > 待裁决（含合并残边）> 失败 > 依赖闸门 > 可派。顺序本身就是语义：
// 一件已是 done 的事不该再报"依赖未 done"，一件还在跑的事也不该报"等你裁决"。
func ItemGateOf(plan storeteamwork.Plan, item storeteamwork.WorkItem, job JobReading) ItemGate {
	switch storeteamwork.WorkItemStatusOrPending(item) {
	case storeteamwork.ItemDone:
		return ItemGateDone
	case storeteamwork.ItemRunning:
		if job.Alive && job.Running {
			return ItemGateJobRunning
		}
		return ItemGateJobInterrupted
	case storeteamwork.ItemReview:
		if _, unmerged := plan.State.Unmerged[item.ID]; unmerged {
			return ItemGateMerge
		}
		return ItemGateReview
	case storeteamwork.ItemFailed:
		return ItemGateFailed
	}
	if unmet := unmetDependencies(plan, item); len(unmet) > 0 {
		return ItemGateBlocked
	}
	return ItemGateReady
}

// UnsettledItem 是一件"收口会拆掉、因此必须先处置"的工作项：卡点 + 该卡点的下一步。
type UnsettledItem struct {
	ID   string   `json:"id"`
	Gate ItemGate `json:"gate"`
	Next string   `json:"next"`
}

// jobReading 把作业表读数折成判据需要的两个布尔（`ItemGateOf` 的输入）。
//
// 它是判据与 Seele 作业表之间**唯一**的适配点：卡点词表不直接依赖 `jobs.Record`，看板投影
// 那边用同一对布尔喂同一个判据。
func (c *Coordinator) jobReading(handle string) JobReading {
	trimmed := strings.TrimSpace(handle)
	if trimmed == "" || c.jobs == nil {
		return JobReading{}
	}
	record, ok := c.jobs.Observe(jobs.Handle(trimmed))
	if !ok {
		return JobReading{}
	}
	return JobReading{Alive: true, Running: record.Running()}
}

// unsettledBySandboxGate 报告这个卡点是不是"收口会拆掉、因此必须先处置"的那几类现场。
//
// 它与卡点是**同一张表**：收口闸门不再自己推一遍"running 且还在册 / failed / review 带未合并标记"，
// 而是问卡点。两边各推一遍的代价是它们必然漂移（审计 D8 的文案与事实不一致就是这一类）。
func unsettledBySandboxGate(gate ItemGate) bool {
	switch gate {
	case ItemGateJobRunning, ItemGateMerge, ItemGateFailed:
		return true
	default:
		return false
	}
}

// unsettledItemGates 列出收口闸门会拦下的工作项（卡点口径）：每件带它自己的卡点与下一步。
//
// 它是**唯一**的"没落定"判据：闸门文案、`team_items` 的卡点格、看板投影读的都是同一条链路
// （ItemGateOf → unsettledBySandboxGate）。此前的实现按三种状态各写一遍条件，于是文案与事实
// 会漂移（审计 D8：列了四种情形却说不清当前是哪种）。
func (c *Coordinator) unsettledItemGates(plan storeteamwork.Plan) []UnsettledItem {
	unsettled := make([]UnsettledItem, 0)
	for _, milestone := range plan.Milestones {
		for _, item := range milestone.Items {
			gate := ItemGateOf(plan, item, c.jobReading(item.Handle))
			if !unsettledBySandboxGate(gate) {
				continue
			}
			unsettled = append(unsettled, UnsettledItem{ID: item.ID, Gate: gate, Next: gate.Next()})
		}
	}
	return unsettled
}

// FormatUnsettledItems 把没收干净的账渲染成**逐件**的一行（卡点 + 该照做的下一步）。
//
// 为什么逐件渲染而不是给一段通用说明：leader 要的是"这一件现在做什么"，通用说明等于让它自己
// 把状态翻译成动作——那正是审计 D7/D8 的缺陷（轮询几十次、拒绝文案说不清当前是哪种）。
func FormatUnsettledItems(items []UnsettledItem) string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, item.ID+"【"+string(item.Gate)+"】→ "+item.Next)
	}
	return strings.Join(lines, "；")
}

// unmetDependencies 返回这件工作项里**还没 done** 的依赖项 id。
func unmetDependencies(plan storeteamwork.Plan, item storeteamwork.WorkItem) []string {
	unmet := make([]string, 0, len(item.DependsOn))
	for _, dependency := range item.DependsOn {
		_, dependencyItem, ok := findItem(&plan, dependency)
		if !ok || storeteamwork.WorkItemStatusOrPending(*dependencyItem) != storeteamwork.ItemDone {
			unmet = append(unmet, dependency)
		}
	}
	return unmet
}
