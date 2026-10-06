package task_context

import "github.com/RedHuang-0622/seelex/application/contract/dto"

// turn_status_record.go — **回合状态在存档面上的读回收口**（全仓唯一一个转换点）。
//
// 为什么要单独一个函数，而不是直接用契约的严格读回（`dto.TurnStatus.UnmarshalJSON`）：
// 存档面 `model.TaskContextProjection.Status` 是**落盘**的，它比当前这一版代码活得久。
//
// 合并之前，"一次回合在进行中"这件事有两份词表：
//
//   - 可见面（`Snapshot.Task.Status`）：`progressing`
//   - 执行/存档面（本包原先的 `StatusRunning`）：`running`
//
// 两处之间只靠 `task_context_state.go` 里那两行手写映射接着（旧名 `model.TaskStatus("running")`）。
// 合并后只剩契约那一份词表（`dto.TurnStatus`），词取可见面一直在用的 `progressing`——
// 它同时是 GUI/TUI 已经在渲染的那个词，换它才是真的会破 wire。
//
// 于是老存档里写的 "running" 必须照样读得回来。口径写在这里，一处：
//
//   - `"running"`（合并前那一格的"进行中"）→ `TurnProgressing`（同一件事换了词）；
//   - 认不得的词、空串（老记录没有这个字段）→ `TurnUnknown`：**说认不得**，不折成任何
//     已知状态。折不认得的词是判定面上最贵的一类错——折成终态会让一个没收尾的回合看起来
//     已经收尾，折成 `idle` 又会假装"这个会话没有回合"；
//   - 其余词交给契约的词表（`dto.ParseTurnStatus`），一格只有一处定义。
//
// 与 seelebridge 记录状态那一格的 `nodeStateOfRecord` 同一形状：落盘格的未知词取舍
// 写在**一个具名函数**上，"读旧文件不炸、也不把读不懂折算成终态"。
const legacyTurnStatusRunning = "running"

// TurnStatusOfRecord 把存档里的回合状态词读回枚举（导出：跨包读方与用例读同一处口径）。
func TurnStatusOfRecord(wire string) dto.TurnStatus {
	if wire == legacyTurnStatusRunning {
		return dto.TurnProgressing
	}
	if status, ok := dto.ParseTurnStatus(wire); ok {
		return status
	}
	return dto.TurnUnknown
}
