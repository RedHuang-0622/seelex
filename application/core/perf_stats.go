package core

import (
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	view "github.com/RedHuang-0622/seelex/middle/view"
)

// PerfStats 返回 GUI 性能追踪钩子的后端数据面：只上报数量/体积等无内容
// 指标（不含任何 prompt/工具参数/输出文本），供前端渲染进程对照——
// DOM 节点数 ↔ 快照载荷体积 ↔ JS heap 的涨跌能直接定位"谁在吃内存"。
// GUI 每 5~10s 轮询一次即可（开销 = 一次锁内扫描 + 会话 JSON 体积估算）。
//
// 派生读数的计算在中间层读数面 `middle/view`（plan §2.4：`perf_stats.go` →
// `middle/view/`）；这里只保留锁与取数（进程级上限、归档读数口都在宿主侧）。
func (service *Service) PerfStats() model.PerfStats {
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	return view.PerfStats(service.Core.Snapshot, Limits().HistoryWindow, service.components.tasks.ToolResultRefs())
}
