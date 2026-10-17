package teamwork

// unit_record.go 是 **teammate 单元记录**的写侧口径（P21-34「域方法随类型走」：从
// `backend/engine/assemble/workunit_team.go` 搬进本域；plan §2.7 把 `workunit_*.go` 与
// `runtime_teamwork*.go` 都指向多智能体板块）。
//
// 两类事：① **身份**（这一轮作业的 nodeID：有指派名就去前缀，降级共享工作区时按归属拼）；
// ② **状态词**（记录那一格的取值面与"收尾分类 → 终态"的折算）。

import (
	"strings"

	"github.com/RedHuang-0622/seelex/backend/workunit"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// 会话记录的 status 词表（`NodeSessionRecord.Status` 的取值面）。只有"在跑"与两种终态：
// 记录回答的是"这一轮跑到哪"，不是"leader 验收了没有"（那是计划里 item.Status 的事——
// 两处各记一份就会漂移）。
//
// var 而非 const：`dto.SubAgent*.String()` 不是常量表达式；词仍只有契约枚举一处。
// 两个终态**不进 workunit 契约**（它由记录写方按自己的语义定名），但"不进契约"不等于
// "各写一份字面量"：记录那一格的取值面只有对外契约 `dto.SubAgent*` 一份（记录词 = wire 词，
// 由 `TestSubagentStatusVocabularyAgreesWithTheWire` 逐条互锁）。
var (
	// UnitStatusRunning 复用契约的词表（"在跑"只有一份，见 workunit.StatusRunning / InFlight）。
	UnitStatusRunning = workunit.StatusRunning.String()
	// UnitStatusDone / UnitStatusFailed 是记录写方的两种终态词。
	UnitStatusDone   = dto.SubAgentDone.String()
	UnitStatusFailed = dto.SubAgentFailed.String()
)

// UnitStatusFor 把收尾分类折成记录的终态：**不判死的那两类（未提交 / 主工作区挡路）记 done**
// ——这一轮跑完了、产出还在现场，记录不该把它说成"这一轮失败了"；未合并这件事由计划里的
// item.Status（待验收）+ plan.State.Unmerged 承载（两处各记一份就会漂移）。
func UnitStatusFor(kind workunit.OutcomeKind) string {
	if kind == workunit.OutcomeFailed {
		return UnitStatusFailed
	}
	return UnitStatusDone
}

// UnitNodeID 把一轮 teammate 作业的现场折成契约的 nodeID（`<role>-<itemID>` / `<role>`）：
// 有指派名就去前缀（换算只有 NodeIDFromWorktreeName 一处），降级共享工作区（没有独立现场）
// 时按归属拼——同一份命名由 SceneNodeID 给出。
func UnitNodeID(request WorkerRequest) string {
	if nodeID := NodeIDFromWorktreeName(request.Worktree); nodeID != "" {
		return nodeID
	}
	if itemID := strings.TrimSpace(request.WorkItemID); itemID != "" {
		return SceneNodeID(request.Role, itemID)
	}
	return strings.TrimSpace(request.Role)
}
