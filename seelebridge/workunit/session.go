package workunit

import (
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// 会话恢复（重启回灌）：三层共用**同一形状**——这是契约里与"现场认领"并列的另一半，
// 也是三层真正一致的那部分动作：
//
//	1. 形状一致：一个工作单元 = 一条会话记录（`sessionstore.NodeSessionRecord`）。NodeID
//	   是定位键（subagent 是节点 id，teammate 是 `<role>-<itemID>`，角色级是 `<role>`）；
//	   记录里的 History / StagesJSON / ContextJSON / Worktree 就是"这一轮跑到哪、现场在哪"。
//	2. 运行期落盘：跑着就写（进程中断或崩溃能从最近一次记录恢复），落定再写终态。
//	3. 认领先于 Prune：重启后先认领现场（`Recover` 必须在 worktree.Prune **之前**跑完），
//	   否则"干净但还没合并"的现场会被当孤儿连分支一起删（F4）。
//	4. 回灌 + 恢复说明：历史灌回执行面，并注入一份**恢复说明**（ResumeNotice）；记录说
//	   running/queued 而本进程已无它的执行面 ⇒ 中断（记进 Resume.Interrupted，交上层重跑
//	   或人工处置）。
//
// 为什么这条要进契约而不是各层各写一遍：teammate 的会话此前**只在内存**（重启即失忆，
// TeammateSessionLive 返回 Running=false），而 subagent 早就有一条完整的"落盘 → 认领 →
// 回灌 → 注入恢复说明"链路。同一件事被写了两遍，且其中一遍是缺的——这正是契约要收口的。

// SessionLedger 是"一个工作单元的会话记录"的持久化端口。
//
// `*sessionstore.NodeSessionStore` **结构上**就满足它（同名、同签名）——这是本契约里
// "复用同一份实现"的落点：不写适配器，也不另立第二份记录类型（第二份就是第二份真相）。
type SessionLedger interface {
	Save(projectID, mainSessionID string, record sessionstore.NodeSessionRecord) error
	List(projectID, mainSessionID string) ([]sessionstore.NodeSessionRecord, error)
	// Delete 清掉一条记录：清会话是生命周期（Reclaim）的**动作**，不是另一件事——
	// 端口只开一半，回收就只能绕回具体类型，那是第二份接线。
	Delete(projectID, mainSessionID, subSessionID string) error
}

// 编译期钉住"复用"这件事：接口一旦与既有存储漂移，这里先红。
var _ SessionLedger = (*sessionstore.NodeSessionStore)(nil)

// Resume 是一次重启回灌的读数（`Lifecycle.Recover` 的返回）。
type Resume struct {
	Scenes      int      `json:"scenes"`      // 认领回来的现场数（不重建、不清理）
	Sessions    int      `json:"sessions"`    // 回灌回来的会话数
	Interrupted []string `json:"interrupted"` // NodeID：记录说在跑，本进程已无执行面
}

// Empty 报告这次回灌什么都没发生（无存储/空会话时的正常读数，不是错误）。
func (r Resume) Empty() bool {
	return r.Scenes == 0 && r.Sessions == 0 && len(r.Interrupted) == 0
}

// 会话记录的「在跑」词表：只有这两个取值算"这一轮还没结束"。
//
// **三层共用这一份**——回灌判中断（`Resume.Interrupted`）就是拿它判的；而此前 node 侧
// （`record.Status == "queued" || record.Status == "running"`）与 teammate 侧
// （`teamUnitInFlight`）各写了一份字面量：同一个判据两处实现，一处改了另一处不会跟着改。
//
// 值**直接引契约**的记录状态枚举（`dto.SubAgentNodeStatus`）：这两个名字是那一格的
// "在跑子集"，本包不再自己写第二份字面量（先前靠一条用例去比对上另一份——现在由构造
// 保证）。终态（done / failed / interrupted …）不进这一层：它由记录写方按自己的语义
// 定名（teammate 记 done|failed，subagent 记它自己的终态），判中断只依赖这一侧。
const (
	StatusQueued  = dto.SubAgentQueued
	StatusRunning = dto.SubAgentRunning
)

// InFlight 报告一条会话记录的 status 是不是"说自己在跑"：记录说在跑、而本进程已无它的
// 执行面 ⇒ 这件事中断了（进 `Resume.Interrupted`，交上层重跑或人工处置）。
//
// 读进来的 `status` 是**落盘记录的字符串**（sessionstore 在契约之下，存的是词），所以
// 按记录那一格的词表读回一次：认不得的词 / 空串都不是"在跑"（也不折成某个已知状态——
// 把读不懂说成"在跑"会让一条早已结束的记录永远占着"进行中"，反过来就是漏判中断）。
func InFlight(status string) bool {
	state, ok := dto.ParseSubAgentNodeStatus(strings.TrimSpace(status))
	if !ok {
		return false
	}
	return state == dto.SubAgentQueued || state == dto.SubAgentRunning
}

// RecoveryNoteRole 是恢复说明的注入 role：恒为 system——它是 Seelex 的编排事实，不是模型
// 发言，也不是用户输入（与既有的 SubagentRecoveryNoteRole 同一口径）。
const RecoveryNoteRole = "system"

// RecoveryNotePrefix 是恢复说明的稳定前缀族（测试与审计据此识别）：完整前缀 =
// 本前缀 + " " + kind。既有的 subagent 前缀（`seelebridge` 的 `subagentRecoveryNotePrefix`）
// 正好是本族 + " subagent"——两层在审计里因此是**同一种东西**，而不是两份自造说明。
const RecoveryNotePrefix = "[Seelex recovery note: interrupted"

// resumeNoticeLimit 是恢复说明的长度上限：它要进 system 注入，必须有界。
const resumeNoticeLimit = 1200

// RecoveryNote 组装"这件事重启前跑到哪、下一步做什么"的恢复说明。三层**共用这一份构建器**
// （前缀族、事实项集合、有界性一致；kind 只决定前缀尾）：**事实在前（目标/状态/阶段/结论/
// 错误/现场），动作在后**；注入时用 RecoveryNoteRole。
//
// 不编造：记录里没有的事实（例如"还差什么"）不写，只把记录里的东西摆出来。
func RecoveryNote(kind Kind, record sessionstore.NodeSessionRecord) string {
	goal := strings.TrimSpace(record.Goal)
	if goal == "" {
		goal = "（记录里没有目标正文）"
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "%s %s] 这件事在本进程重启之前已经开始，它的执行面已不在内存里。记录里的事实：\n", RecoveryNotePrefix, kind)
	fmt.Fprintf(&builder, "- 节点：%s（会话 %s）\n", record.NodeID, record.SessionID)
	fmt.Fprintf(&builder, "- 目标：%s\n", goal)
	if status := strings.TrimSpace(record.Status); status != "" {
		fmt.Fprintf(&builder, "- 中断前状态：%s\n", status)
	}
	if stages := strings.TrimSpace(string(record.StagesJSON)); stages != "" && stages != "null" {
		fmt.Fprintf(&builder, "- 已到阶段（打点）：%s\n", stages)
	}
	if summary := strings.TrimSpace(record.Summary); summary != "" {
		fmt.Fprintf(&builder, "- 中断前结论：%s\n", summary)
	}
	if recordErr := strings.TrimSpace(record.Error); recordErr != "" {
		fmt.Fprintf(&builder, "- 中断前错误：%s\n", recordErr)
	}
	if scene := strings.TrimSpace(record.Worktree.Path); scene != "" {
		fmt.Fprintf(&builder, "- 现场：%s（分支 %s）\n", scene, strings.TrimSpace(record.Worktree.Branch))
	}
	builder.WriteString("下一步：接着这件事做——先看现场有没有未提交产出（有就先补提交或明确处置），" +
		"再按收尾口径落定；不要把它当新的一件活从零开始。")
	return boundedTo(builder.String(), resumeNoticeLimit)
}
