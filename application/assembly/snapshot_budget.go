package assembly

import limit "github.com/RedHuang-0622/seelex/application/policy"

// snapshotToolOutputLimit 返回可见会话快照单条工具输出的字符预算
// （seelex.yaml limits 段 snapshot_tool_output_chars，默认 8000）。
// 默认值与零值补全的唯一来源是 kernel/limit（Apply 走 WithDefaults，
// Get 无快照时回退 DefaultLimits），这里只读生效值。
// 消费点：handleToolCompleteObserved（实时路径）与 appendHistoryLocked
// （会话恢复路径）。provider 侧历史不受此值影响，仍由
// max_tool_result_chars 单独约束。
func snapshotToolOutputLimit() int {
	return limit.Get().SnapshotToolOutputChars
}

// boundToolResultForSnapshot 生成进入可见会话快照的工具输出（调用方持有
// Core.ViewMu；StoreToolResultLocked 与 pendingToolResults 均要求持锁）：
//   - 未超预算 → 原样返回（visible=content, ref=""）；
//   - 超预算 → 全文归档为 result_ref（复用 read_tool_result 的持久化
//     通道：pending 内存态 + 会话落盘，前端可随时经 ToolResultContent
//     分页读回），快照只放预览 + 引用元数据。
//
// 截断判据只有一个家：ResidencyPort.Preview（实现在 middle/residency）。
// 这里不得再按 len(content) 自行判定一次——两侧漂移会产生「标了截断却没有
// 引用」或「全文进了快照却没有引用可读回」。
//
// 这是"谁在吃内存"根因链的治本一环：60KB 级别的全文不再随每次快照/
// 事件进入渲染进程，DOM 与 JS heap 只持有 ≤ 8KB 的预览。
func (service *Service) boundToolResultForSnapshot(name, content string) (visible string, ref string, truncated bool, totalChars int) {
	return service.components.frontendSnapshot.ToolResultBudget().Bound(name, content)
}
