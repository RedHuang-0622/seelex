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
	storeworkunit "github.com/RedHuang-0622/seelex/store/workunit"
)

// 会话记录的 status 词表（`Record.Status` 的取值面）。只有"在跑"与两种终态：
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

// UnitRecordKey 是一个 teammate 单元的身份：现场 nodeID + 角色会话 + 这一轮的目标，
// 以及记录快照里那一格身份要用的三件事（角色 / 插件装配 / 系统提示）。
//
// SystemPrompt 由**装配那一刻**填（宿主算出来的那一份，含中断恢复说明），不在
// `UnitRecordKeyFor` 里拼：那是装配层的事实，不是派发载荷里的事实。
type UnitRecordKey struct {
	NodeID        string
	RoleSessionID string
	Goal          string
	Role          string
	Plugins       []string
	SystemPrompt  string
}

// UnitRecordKeyFor 从一轮作业的载荷取出记录身份（NodeID 口径见 `UnitNodeID`）。
// SystemPrompt 不在这里填（它由宿主的装配面在开跑前补上）。
func UnitRecordKeyFor(request WorkerRequest) UnitRecordKey {
	return UnitRecordKey{
		NodeID:        UnitNodeID(request),
		RoleSessionID: strings.TrimSpace(request.RoleSessionID),
		Goal:          strings.TrimSpace(request.Goal),
		Role:          strings.TrimSpace(request.Role),
		Plugins:       append([]string(nil), request.Plugins...),
	}
}

// UnitRecord 把一条 teammate 单元记录的形状收在本域：记录身份、摘要裁剪与阶段编码
// 都是"这一份现场跑到哪"的判据，装配层只提供主会话作用域、状态词与现场四栏。
//
// 时间戳不在这里造：哪一刻开始/落定归计划里的 item 事实，记录只带存储写入的
// UpdatedAt。
func UnitRecord(key UnitRecordKey, mainSessionID, status, summary, stage string, worktree storeworkunit.WorktreeRecord) storeworkunit.Record {
	return storeworkunit.Record{
		SchemaVersion: storeworkunit.SchemaVersion,
		NodeID:        strings.TrimSpace(key.NodeID),
		SessionID:     strings.TrimSpace(key.RoleSessionID),
		MainSessionID: strings.TrimSpace(mainSessionID),
		Goal:          strings.TrimSpace(key.Goal),
		Status:        status,
		Summary:       workunit.ClipPreview(summary),
		StagesJSON:    workunit.EncodeStages([]workunit.Stage{{Stage: stage, Preview: summary}}),
		Worktree:      worktree,
	}
}

// TeammateUnitRecordLink 是写链上 teammate 那一环：把**这一层的身份**写进记录
// （Kind = teammate / Role / Plugins / SystemPrompt）。
//
// 它只写自己知道的事实，别的一概不动：子代理那一环在内层，看到身份已经有了就不再覆盖
// （与 `subagent.SubagentUnitRecordLink` 同一种"责任链上的身份环"形状）。
func TeammateUnitRecordLink(key UnitRecordKey) storeworkunit.Link {
	identity := storeworkunit.UnitRecord{
		Kind:         string(workunit.KindTeammate),
		Role:         strings.TrimSpace(key.Role),
		Plugins:      append([]string(nil), key.Plugins...),
		SystemPrompt: key.SystemPrompt,
	}
	return func(record storeworkunit.Record) storeworkunit.Record {
		record.Unit = identity
		return record
	}
}
