package assembly

import (
	limit "github.com/RedHuang-0622/seelex/application/policy"
	worktable "github.com/RedHuang-0622/seelex/kernel/worktable"
)

// workTableBounds 把进程级上限里**工作表格投影**用到的那三格按值取出，交给中间层的
// 纯投影（`kernel/worktable` 不反向依赖 `kernel/limit`）。
// 这是本仓库唯一一处"limit.Limits → worktable.Limits"的转换点：投影侧只声明它用到
// 哪些上限，宿主只负责取数（判据仍在 `kernel/limit` 一处）。
func workTableBounds() worktable.Limits {
	values := limit.Get()
	return worktable.Limits{
		WorkTableRows:  values.WorkTableRows,
		EvidenceChars:  values.EvidenceChars,
		PlanNodeEvents: values.PlanNodeEvents,
	}
}
