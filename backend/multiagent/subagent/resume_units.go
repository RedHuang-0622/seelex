package session

// resume_units.go 是**子代理中断恢复的判据面**（P21-19「域方法随类型走」：从
// `backend/engine/assemble/runtime_subagent_resume.go` 把"残留记录 → 恢复单元"的折算、
// 现场投影、恢复说明的追加事实与终态判据搬进子代理域；plan §2.7 把
// `runtime_subagent_{recovery,resume,session}.go` 列为 `backend/multiagent/subagent/` 的目标）。
//
// 术语（与恢复模板一致）：subagent 是劳务派遣式 tool calling 能力（不是 AgentTeam 成员）。
// 终止前没做完的子代理按 interrupted tool-chain 语义续跑：
//
//	残留记录（active 且无结论）→ 重建现场 → system 注入恢复说明 → 同键重跑
//
// 七步模板本体在 `backend/multiagent/resume`（领域无关）；本文件给的是**子代理这一层的判据**，
// 宿主组件（重派发 / 记录删除 / 会话槽 / 事件库）由装配层以端口注入。

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/backend/multiagent/resume"
	"github.com/RedHuang-0622/seelex/backend/workunit"
	"github.com/RedHuang-0622/seelex/contract/dto"
	storeworkunit "github.com/RedHuang-0622/seelex/store/workunit"
)

// ResumeKind 是恢复模板里的单元种类标识（子代理那一支）。
const ResumeKind = "subagent"

// LocateResumeUnits 把残留记录折算成恢复单元。
//
// 判定口径（持久化事实，不用内存 active 标记）：
//   - 有结论事件 → 终结（done），只需幂等清理残留；
//   - status=queued/running 且无结论 → active，需要重启续跑；
//   - status=done/failed → 终结，不重启。
//
// 读法与折算都**只有一份**（`workunit.UnitReader` 的 List → 定位 → 折算，
// `workunit.ProgressOf` 折状态）：此前这里是"自己 List + 自己判 InFlight + 自己取字段"的
// 第二份实现，与 teammate 侧、与生命周期的记录读面各写一遍。`conclusions` 是幂等键集合
// （父级事件库里的结论），由调用方读好后传入。
func LocateResumeUnits(parentSessionID string, records []storeworkunit.Record, conclusions map[string]struct{}) []resume.Unit {
	units := make([]resume.Unit, 0, len(records))
	for _, record := range records {
		progress := workunit.ProgressOf(workunit.KindSubagent, record)
		key := ResumeUnitKey(progress)
		if key == "" {
			continue
		}
		status := resume.UnitDone
		switch {
		// "还在跑"的判据只有一份（workunit.Progress.InFlight）：与 teammate 侧、与树投影同一条。
		case progress.InFlight:
			status = resume.UnitActive
		case RecordIsFailed(record):
			status = resume.UnitFailed
		}
		if _, concluded := conclusions[key]; concluded {
			// 结论唯一：已有结论 → 残留记录只是半同步残留，按终结处理。
			status = resume.UnitDone
		}
		units = append(units, resume.Unit{
			Key: key, Kind: ResumeKind, Status: status,
			SessionID: progress.SessionID, ParentSessionID: parentSessionID,
			Goal: progress.Goal, UpdatedAt: progress.UpdatedAt,
		})
	}
	return units
}

// ResumeUnitKey 是恢复单元的幂等键：节点 id 优先，缺了退到它自己的会话号（键为空 = 这条记录
// 没有可归位的身份，调用方跳过它）。**键口径只有这一处**：Locate 的分批与后续按键查记录都读它。
func ResumeUnitKey(progress workunit.Progress) string {
	if progress.NodeID != "" {
		return progress.NodeID
	}
	return progress.SessionID
}

// RecordIsFailed 报告落盘记录是不是**判失败**的终态。
//
// 落盘格的唯一转换点（`store` 在契约之下，存的是词，不是枚举）：认不得的词落
// `dto.SubAgentUnknown`，而 Unknown **不是终态**——"读不懂"绝不会被折算成"已完成"
// （判定面因此在 Locate 与 ensureConclusion 的保守分支里）。
//
// 与进程内 wire 的严格口径（dto.UnmarshalJSON 认不得就报错）是**两种取舍**，不是重复：
// 进程内的词由我们同一份代码写出，读不懂就是 bug；落盘的词要跨版本、跨机器读回来。
func RecordIsFailed(record storeworkunit.Record) bool {
	state, ok := dto.ParseSubAgentNodeStatus(record.Status)
	return ok && state == dto.SubAgentFailed
}

// RecordIsTerminal 报告落盘记录本身是否已是**终结态**（done/failed）。
//
// 它给结论重放那一支用：崩溃只可能发生在"写结论 + 删记录"之间，此时记录里的终态事实
// 即结论，按其重放一次即可。认不出的状态词**不是**终态。
func RecordIsTerminal(record storeworkunit.Record) bool {
	state, ok := dto.ParseSubAgentNodeStatus(record.Status)
	if !ok {
		return false
	}
	return state == dto.SubAgentDone || state == dto.SubAgentFailed
}

// SceneOf 重建被执行单元自己的现场：目标/已知进展/工作树。
//
// 摘要取记录自带的 `Summary`；它为空时回退到**阶段日志里最后一条预览**
// （"之前做到哪"），两条都有界。
func SceneOf(record storeworkunit.Record) (resume.Scene, error) {
	payload, err := json.Marshal(record)
	if err != nil {
		return resume.Scene{}, fmt.Errorf("encode scene: %w", err)
	}
	summary := strings.TrimSpace(record.Summary)
	if summary == "" {
		summary = LastStagePreview(record)
	}
	return resume.Scene{Payload: payload, Summary: summary}, nil
}

// LastStagePreview 读取阶段日志里最后一条预览（恢复说明的"之前做到哪"）。
//
// 解码走契约那**唯一一份**（`workunit.DecodeStages`）：此前这里手写了一个匿名结构自己解
// 同一串载荷——同一件事的第二份解码实现，载荷形状一变只有一处会跟着改。
func LastStagePreview(record storeworkunit.Record) string {
	stages := workunit.DecodeStages(record.StagesJSON)
	if len(stages) == 0 {
		return ""
	}
	last := stages[len(stages)-1]
	if last.Preview == "" {
		return last.Stage
	}
	return last.Stage + ": " + last.Preview
}

// ResumeExtraFacts 追加契约 builder 之外的记录事实（可以为空）：
//
//   - last known progress：阶段打点里最后一条预览（人可读的"之前做到哪"）；
//   - recovered messages：回灌回来的历史长度。
//
// 两条都有界（progress 已被 SceneOf 那条路裁到 240 字节内，条数是一个整数）。
// 正文来自契约的唯一构建器（`workunit.RecoveryNote`）；契约 builder 不收的两个字段在这里
// 追上去，不丢事实。
func ResumeExtraFacts(record storeworkunit.Record, progress string) string {
	builder := &strings.Builder{}
	if progress = strings.TrimSpace(progress); progress != "" {
		builder.WriteString("\n- last known progress: ")
		builder.WriteString(progress)
	}
	if len(record.History) > 0 {
		builder.WriteString(fmt.Sprintf("\n- recovered messages: %d", len(record.History)))
	}
	return builder.String()
}

// ResumeNote 组装一条子代理恢复说明：契约正文 + 追加事实（**唯一一份**组合口径）。
func ResumeNote(record storeworkunit.Record, progress string) string {
	return workunit.RecoveryNote(workunit.KindSubagent, record) + ResumeExtraFacts(record, progress)
}
